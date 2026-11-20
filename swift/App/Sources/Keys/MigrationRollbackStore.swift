import Darwin
import Foundation

enum MigrationRollbackError: Error, Equatable, Sendable {
    case io(operation: String, errno: Int32)
}

/// The bytes of a vault file as they were before a key lifecycle commit, kept
/// until the vault has opened through its new key. One copy at a time; the
/// vault path it belongs to lives in UserDefaults.
struct MigrationRollbackStore: @unchecked Sendable {
    static let vaultPathKey = "tessera.migrationRollbackVaultPath"
    static let fileName = "migration-rollback.json"

    let directory: URL
    private let defaults: UserDefaults

    init(directory: URL, defaults: UserDefaults) {
        self.directory = directory
        self.defaults = defaults
    }

    var fileURL: URL { directory.appendingPathComponent(Self.fileName) }

    func save(_ bytes: Data, for vaultPath: String) throws(MigrationRollbackError) {
        defaults.removeObject(forKey: Self.vaultPathKey)
        try DurableWrite.replace(fileURL, with: bytes, directory: directory)
        defaults.set(SEKeyRegistry.normalizedPath(vaultPath), forKey: Self.vaultPathKey)
    }

    /// The saved copy and the real path of the vault it belongs to.
    func pending() -> (vaultPath: String, url: URL)? {
        guard let path = defaults.string(forKey: Self.vaultPathKey),
              FileManager.default.fileExists(atPath: fileURL.path) else { return nil }
        return (path, fileURL)
    }

    func discard() throws(MigrationRollbackError) {
        if unlink(fileURL.path) != 0 && errno != ENOENT { throw .io(operation: "unlink", errno: errno) }
        defaults.removeObject(forKey: Self.vaultPathKey)
    }
}

/// Temp file at 0600, full sync, rename over the target, then sync the directory.
private enum DurableWrite {
    static func replace(_ target: URL, with bytes: Data, directory: URL) throws(MigrationRollbackError) {
        try makeDirectory(directory)
        let temp = directory.appendingPathComponent(VaultSidecar.tempPrefix + UUID().uuidString + ".tmp")
        let fd = open(temp.path, O_WRONLY | O_CREAT | O_EXCL | O_NOFOLLOW | O_CLOEXEC, 0o600)
        guard fd >= 0 else { throw .io(operation: "open", errno: errno) }
        var renamed = false
        defer { if !renamed { unlink(temp.path) } }
        do throws(MigrationRollbackError) {
            guard fchmod(fd, 0o600) == 0 else { throw .io(operation: "fchmod", errno: errno) }
            try writeAll(fd, bytes)
            try fullSync(fd)
        } catch {
            close(fd)
            throw error
        }
        guard close(fd) == 0 else { throw .io(operation: "close", errno: errno) }
        guard rename(temp.path, target.path) == 0 else { throw .io(operation: "rename", errno: errno) }
        renamed = true
        syncDirectory(directory)
    }

    private static func makeDirectory(_ directory: URL) throws(MigrationRollbackError) {
        do {
            try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true,
                                                    attributes: [.posixPermissions: 0o700])
        } catch {
            throw .io(operation: "mkdir", errno: Int32((error as NSError).code))
        }
    }

    private static func writeAll(_ fd: Int32, _ bytes: Data) throws(MigrationRollbackError) {
        let failure: Int32? = bytes.withUnsafeBytes { raw -> Int32? in
            guard var base = raw.baseAddress else { return nil }
            var left = raw.count
            while left > 0 {
                let n = write(fd, base, left)
                if n < 0 {
                    if errno == EINTR { continue }
                    return errno
                }
                left -= n
                base += n
            }
            return nil
        }
        if let failure { throw .io(operation: "write", errno: failure) }
    }

    private static func fullSync(_ fd: Int32) throws(MigrationRollbackError) {
        if fcntl(fd, F_FULLFSYNC) == 0 { return }
        guard fsync(fd) == 0 else { throw .io(operation: "fsync", errno: errno) }
    }

    private static func syncDirectory(_ directory: URL) {
        let fd = open(directory.path, O_RDONLY | O_CLOEXEC)
        guard fd >= 0 else { return }
        _ = fsync(fd)
        close(fd)
    }
}
