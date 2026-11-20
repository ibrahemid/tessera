import Darwin
import Foundation

enum DurableFile {
    static let maxVaultSize = 64 << 20
    static let staleTempAge: TimeInterval = 24 * 60 * 60
    private static let completeProtectionClass: Int32 = 1

    typealias Rename = @Sendable (_ from: String, _ to: String) -> Int32

    static let systemRename: Rename = { from, to in Darwin.rename(from, to) == 0 ? 0 : errno }

    static func replace(_ data: Data, at url: URL, hasDirectoryAccess: Bool,
                        fileProtection: Bool = false) throws(VaultWriteError) {
        try replace(data, at: url, hasDirectoryAccess: hasDirectoryAccess,
                    fileProtection: fileProtection, rename: systemRename)
    }

    static func replace(_ data: Data, at url: URL, hasDirectoryAccess: Bool,
                        fileProtection: Bool, rename: Rename) throws(VaultWriteError) {
        guard hasDirectoryAccess else {
            try replaceWithoutDirectoryAccess(data, at: url, fileProtection: fileProtection, rename: rename)
            return
        }
        let dir = url.deletingLastPathComponent()
        let temp = try writeTemp(data, in: dir, target: url, fileProtection: fileProtection)
        let code = rename(temp.path, url.path)
        guard code == 0 else {
            unlink(temp.path)
            throw .io(operation: "rename", url: url, errno: code)
        }
        try syncDirectory(dir, target: url, ignoreAccessDenied: false)
    }

    static func create(_ data: Data, at url: URL, hasDirectoryAccess: Bool,
                       fileProtection: Bool = false) throws(VaultWriteError) {
        let dir = url.deletingLastPathComponent()
        var staging: URL?
        if !hasDirectoryAccess { staging = try stagingDirectory(for: url) }
        defer { if let staging { try? FileManager.default.removeItem(at: staging) } }
        let temp = try writeTemp(data, in: staging ?? dir, target: url, fileProtection: fileProtection)
        defer { unlink(temp.path) }
        if link(temp.path, url.path) != 0 {
            let code = errno
            if code == EEXIST { throw .alreadyExists(url) }
            guard code == EPERM || code == ENOSYS || isUnsupported(code) else {
                throw .io(operation: "create", url: url, errno: code)
            }
            var info = stat()
            if lstat(url.path, &info) == 0 { throw .alreadyExists(url) }
            let missing = errno
            guard missing == ENOENT else { throw .io(operation: "create", url: url, errno: missing) }
            if Darwin.rename(temp.path, url.path) != 0 { throw .io(operation: "rename", url: url, errno: errno) }
        }
        try syncDirectory(dir, target: url, ignoreAccessDenied: !hasDirectoryAccess)
    }

    static func preserve(_ url: URL, as destination: URL) throws(VaultWriteError) {
        let dir = destination.deletingLastPathComponent()
        var linked: URL?
        for _ in 0..<10_000 {
            let candidate = dir.appendingPathComponent(tempName())
            if link(url.path, candidate.path) == 0 { linked = candidate; break }
            let code = errno
            if code == EEXIST { continue }
            if code == ENOENT { throw .missingFile(url) }
            guard code == EPERM || code == ENOSYS || code == EXDEV || code == EMLINK || isUnsupported(code) else {
                throw .io(operation: "preserve", url: destination, errno: code)
            }
            break
        }
        if let linked {
            if chmod(linked.path, 0o600) != 0 {
                let code = errno
                unlink(linked.path)
                throw .io(operation: "chmod", url: destination, errno: code)
            }
            if Darwin.rename(linked.path, destination.path) != 0 {
                let code = errno
                unlink(linked.path)
                throw .io(operation: "rename", url: destination, errno: code)
            }
        } else {
            if Darwin.rename(url.path, destination.path) != 0 {
                throw .io(operation: "rename", url: destination, errno: errno)
            }
            if chmod(destination.path, 0o600) != 0 { throw .io(operation: "chmod", url: destination, errno: errno) }
        }
        try syncDirectory(dir, target: destination, ignoreAccessDenied: false)
    }

    static func writePrivate(_ data: Data, to url: URL) throws(VaultWriteError) {
        let fd = open(url.path, O_WRONLY | O_CREAT | O_TRUNC | O_NOFOLLOW | O_CLOEXEC, 0o600)
        guard fd >= 0 else { throw .io(operation: "open", url: url, errno: errno) }
        do throws(VaultWriteError) {
            if fchmod(fd, 0o600) != 0 { throw .io(operation: "chmod", url: url, errno: errno) }
            try writeAll(fd, data, target: url)
            let code = fullSync(fd)
            if code != 0 { throw .io(operation: "flush", url: url, errno: code) }
        } catch {
            close(fd)
            throw error
        }
        if close(fd) != 0 { throw .io(operation: "close", url: url, errno: errno) }
    }

    static func read(_ url: URL, limit: Int = maxVaultSize) throws(VaultWriteError) -> Data {
        let fd = open(url.path, O_RDONLY | O_NONBLOCK | O_CLOEXEC)
        guard fd >= 0 else {
            let code = errno
            throw code == ENOENT ? .missingFile(url) : .io(operation: "read", url: url, errno: code)
        }
        defer { close(fd) }
        var info = stat()
        guard fstat(fd, &info) == 0 else { throw .io(operation: "read", url: url, errno: errno) }
        guard info.st_mode & S_IFMT == S_IFREG else { throw .io(operation: "read", url: url, errno: EFTYPE) }
        guard info.st_size <= Int64(limit) else { throw .unparseable(url) }
        var data = Data()
        var buffer = [UInt8](repeating: 0, count: 64 * 1024)
        while true {
            let count = buffer.withUnsafeMutableBytes { Darwin.read(fd, $0.baseAddress, $0.count) }
            if count < 0 {
                let code = errno
                if code == EINTR { continue }
                throw .io(operation: "read", url: url, errno: code)
            }
            if count == 0 { return data }
            data.append(contentsOf: buffer[0..<count])
            if data.count > limit { throw .unparseable(url) }
        }
    }

    static func remove(_ url: URL) throws(VaultWriteError) {
        if unlink(url.path) != 0 {
            let code = errno
            if code == ENOENT { return }
            throw .io(operation: "remove", url: url, errno: code)
        }
        try syncDirectory(url.deletingLastPathComponent(), target: url, ignoreAccessDenied: false)
    }

    static func makePrivateDirectory(_ dir: URL) throws(POSIXError) {
        var info = stat()
        if stat(dir.path, &info) == 0 {
            guard info.st_mode & S_IFMT == S_IFDIR else { throw posixError(ENOTDIR) }
            return
        }
        let parent = dir.deletingLastPathComponent()
        if parent.path != dir.path { try makePrivateDirectory(parent) }
        if mkdir(dir.path, 0o700) == 0 { return }
        let code = errno
        if code == EEXIST, stat(dir.path, &info) == 0, info.st_mode & S_IFMT == S_IFDIR { return }
        throw posixError(code)
    }

    static func sweepStaleTemps(in dir: URL, now: Date = Date()) {
        guard let names = try? FileManager.default.contentsOfDirectory(atPath: dir.path) else { return }
        let cutoff = now.addingTimeInterval(-staleTempAge).timeIntervalSince1970
        for name in names where isTempName(name) {
            let path = dir.appendingPathComponent(name).path
            var info = stat()
            guard lstat(path, &info) == 0, info.st_mode & S_IFMT == S_IFREG,
                  info.st_mode & 0o777 == 0o600, info.st_uid == getuid() else { continue }
            let modified = TimeInterval(info.st_mtimespec.tv_sec) + TimeInterval(info.st_mtimespec.tv_nsec) / 1e9
            if modified < cutoff { unlink(path) }
        }
    }

    static func isTempName(_ name: String) -> Bool {
        let prefix = VaultSidecar.tempPrefix, suffix = VaultSidecar.tempSuffix
        guard name.count > prefix.count + suffix.count, name.hasPrefix(prefix), name.hasSuffix(suffix) else { return false }
        return name.dropFirst(prefix.count).dropLast(suffix.count).allSatisfy { $0.isASCII && $0.isNumber }
    }

    static func resolvedTarget(_ url: URL) -> URL {
        guard isSymlink(url), let real = realpath(url.path, nil) else { return url }
        defer { free(real) }
        return URL(fileURLWithPath: String(cString: real))
    }

    static func isSymlink(_ url: URL) -> Bool {
        var info = stat()
        return lstat(url.path, &info) == 0 && info.st_mode & S_IFMT == S_IFLNK
    }

    static func modificationDate(of url: URL) -> Date? {
        (try? FileManager.default.attributesOfItem(atPath: url.path)[.modificationDate]) as? Date
    }

    static func isUnsupported(_ code: Int32) -> Bool {
        code == EINVAL || code == ENOTSUP || code == EOPNOTSUPP
    }

    private static func posixError(_ code: Int32) -> POSIXError {
        POSIXError(POSIXErrorCode(rawValue: code) ?? .EIO)
    }

    private static func replaceWithoutDirectoryAccess(_ data: Data, at url: URL, fileProtection: Bool,
                                                      rename: Rename) throws(VaultWriteError) {
        let staging = try stagingDirectory(for: url)
        defer { try? FileManager.default.removeItem(at: staging) }
        let temp = try writeTemp(data, in: staging, target: url, fileProtection: fileProtection)
        let code = rename(temp.path, url.path)
        if code == 0 {
            try syncDirectory(url.deletingLastPathComponent(), target: url, ignoreAccessDenied: true)
            return
        }
        guard code == EPERM || code == EACCES || code == EXDEV else {
            throw .io(operation: "rename", url: url, errno: code)
        }
        do {
            _ = try FileManager.default.replaceItemAt(url, withItemAt: temp, backupItemName: nil,
                                                      options: [.usingNewMetadataOnly])
        } catch {
            throw .io(operation: "replace", url: url, errno: posixCode(of: error))
        }
        if chmod(url.path, 0o600) != 0 { throw .io(operation: "chmod", url: url, errno: errno) }
    }

    private static func stagingDirectory(for url: URL) throws(VaultWriteError) -> URL {
        if let dir = try? FileManager.default.url(for: .itemReplacementDirectory, in: .userDomainMask,
                                                  appropriateFor: url, create: true) {
            return dir
        }
        let dir = FileManager.default.temporaryDirectory.appendingPathComponent("tessera-\(UUID().uuidString)")
        if mkdir(dir.path, 0o700) != 0 { throw .io(operation: "create temp", url: url, errno: errno) }
        return dir
    }

    private static func writeTemp(_ data: Data, in dir: URL, target: URL,
                                  fileProtection: Bool) throws(VaultWriteError) -> URL {
        let (fd, temp) = try createTemp(in: dir, target: target)
        do throws(VaultWriteError) {
            if fileProtection, fcntl(fd, F_SETPROTECTIONCLASS, completeProtectionClass) == -1 {
                let code = errno
                // Volumes without data protection answer EPERM, ENOTSUP or EINVAL; the file stays 0600 there.
                guard code == EPERM || isUnsupported(code) else {
                    throw .io(operation: "protect", url: target, errno: code)
                }
            }
            try writeAll(fd, data, target: target)
            let code = fullSync(fd)
            if code != 0 { throw .io(operation: "flush", url: target, errno: code) }
        } catch {
            close(fd)
            unlink(temp.path)
            throw error
        }
        if close(fd) != 0 {
            let code = errno
            unlink(temp.path)
            throw .io(operation: "close", url: target, errno: code)
        }
        return temp
    }

    private static func createTemp(in dir: URL, target: URL) throws(VaultWriteError) -> (Int32, URL) {
        for _ in 0..<10_000 {
            let temp = dir.appendingPathComponent(tempName())
            let fd = open(temp.path, O_WRONLY | O_CREAT | O_EXCL | O_NOFOLLOW | O_CLOEXEC, 0o600)
            if fd >= 0 {
                guard fchmod(fd, 0o600) == 0 else {
                    let code = errno
                    close(fd)
                    unlink(temp.path)
                    throw .io(operation: "create temp", url: target, errno: code)
                }
                return (fd, temp)
            }
            let code = errno
            if code != EEXIST { throw .io(operation: "create temp", url: target, errno: code) }
        }
        throw .io(operation: "create temp", url: target, errno: EEXIST)
    }

    private static func tempName() -> String {
        VaultSidecar.tempPrefix + String(UInt32.random(in: .min ... .max)) + VaultSidecar.tempSuffix
    }

    private static func writeAll(_ fd: Int32, _ data: Data, target: URL) throws(VaultWriteError) {
        let code: Int32 = data.withUnsafeBytes { raw in
            guard var cursor = raw.baseAddress else { return 0 }
            var remaining = raw.count
            while remaining > 0 {
                let written = write(fd, cursor, remaining)
                if written < 0 {
                    if errno == EINTR { continue }
                    return errno
                }
                if written == 0 { return EIO }
                cursor = cursor.advanced(by: written)
                remaining -= written
            }
            return 0
        }
        if code != 0 { throw .io(operation: "write", url: target, errno: code) }
    }

    private static func fullSync(_ fd: Int32) -> Int32 {
        if fcntl(fd, F_FULLFSYNC) != -1 { return 0 }
        let code = errno
        guard isUnsupported(code) else { return code }
        return fsync(fd) == 0 ? 0 : errno
    }

    private static func syncDirectory(_ dir: URL, target: URL, ignoreAccessDenied: Bool) throws(VaultWriteError) {
        let fd = open(dir.path, O_RDONLY | O_DIRECTORY | O_CLOEXEC)
        if fd < 0 {
            let code = errno
            if ignoreAccessDenied, code == EPERM || code == EACCES { return }
            throw .io(operation: "flush directory", url: target, errno: code)
        }
        defer { close(fd) }
        let code = fullSync(fd)
        if code == 0 || isUnsupported(code) { return }
        if ignoreAccessDenied, code == EPERM || code == EACCES { return }
        throw .io(operation: "flush directory", url: target, errno: code)
    }

    private static func posixCode(of error: Error) -> Int32 {
        let ns = error as NSError
        if ns.domain == NSPOSIXErrorDomain { return Int32(ns.code) }
        if let underlying = ns.userInfo[NSUnderlyingErrorKey] as? NSError, underlying.domain == NSPOSIXErrorDomain {
            return Int32(underlying.code)
        }
        return ns.code == NSFileWriteNoPermissionError ? EACCES : EIO
    }
}
