import Darwin
import Foundation

final class VaultFileLock {
    let url: URL
    private var fd: Int32

    private init(fd: Int32, url: URL) {
        self.fd = fd
        self.url = url
    }

    deinit { release() }

    func release() {
        guard fd >= 0 else { return }
        flock(fd, LOCK_UN)
        close(fd)
        fd = -1
    }

    static func acquire(for vault: URL, timeout: Duration = .seconds(20),
                        poll: Duration = .milliseconds(20)) throws(VaultLockError) -> VaultFileLock {
        let lockURL = VaultSidecar.lockURL(for: vault)
        let clock = ContinuousClock()
        let deadline = clock.now.advanced(by: timeout)
        while true {
            if let lock = try tryAcquire(lockURL) { return lock }
            if clock.now >= deadline { throw .timedOut(lockURL) }
            Thread.sleep(forTimeInterval: seconds(poll))
        }
    }

    private static func tryAcquire(_ lockURL: URL) throws(VaultLockError) -> VaultFileLock? {
        let dir = lockURL.deletingLastPathComponent()
        do {
            try DurableFile.makePrivateDirectory(dir)
        } catch {
            throw .io(dir, errno: error.code.rawValue)
        }
        let fd = try openLockFile(lockURL)
        guard flock(fd, LOCK_EX | LOCK_NB) == 0 else {
            let code = errno
            close(fd)
            if code == EWOULDBLOCK || code == EINTR { return nil }
            throw .io(lockURL, errno: code)
        }
        return VaultFileLock(fd: fd, url: lockURL)
    }

    private static func openLockFile(_ lockURL: URL) throws(VaultLockError) -> Int32 {
        let path = lockURL.path
        let created = open(path, O_RDWR | O_CREAT | O_EXCL | O_NOFOLLOW | O_CLOEXEC, 0o600)
        if created >= 0 {
            guard fchmod(created, 0o600) == 0 else {
                let code = errno
                close(created)
                throw .io(lockURL, errno: code)
            }
            return created
        }
        let createCode = errno
        guard createCode == EEXIST else { throw .io(lockURL, errno: createCode) }
        let existing = open(path, O_RDWR | O_NOFOLLOW | O_CLOEXEC)
        guard existing >= 0 else { throw classifyOpenFailure(lockURL, code: errno) }
        var info = stat()
        guard fstat(existing, &info) == 0 else {
            let code = errno
            close(existing)
            throw .io(lockURL, errno: code)
        }
        if let problem = check(info, lockURL) {
            close(existing)
            throw problem
        }
        return existing
    }

    private static func check(_ info: stat, _ lockURL: URL) -> VaultLockError? {
        if info.st_mode & S_IFMT != S_IFREG { return .notRegularFile(lockURL) }
        if info.st_uid != geteuid() { return .wrongOwner(lockURL) }
        let mode = info.st_mode & 0o777
        if mode != 0o600 { return .wrongMode(lockURL, mode: UInt16(mode)) }
        return nil
    }

    private static func classifyOpenFailure(_ lockURL: URL, code: Int32) -> VaultLockError {
        var info = stat()
        guard lstat(lockURL.path, &info) == 0 else { return .io(lockURL, errno: code) }
        switch info.st_mode & S_IFMT {
        case S_IFLNK: return .symlink(lockURL)
        case S_IFREG: return .io(lockURL, errno: code)
        default: return .notRegularFile(lockURL)
        }
    }

    private static func seconds(_ duration: Duration) -> TimeInterval {
        let parts = duration.components
        return TimeInterval(parts.seconds) + TimeInterval(parts.attoseconds) / 1e18
    }
}
