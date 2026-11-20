import CryptoKit
import Darwin
import Foundation
import os

enum MigrationRollbackError: Error, Equatable, Sendable {
    case io(operation: String, errno: Int32)
}

/// The bytes of a vault file as they were before a key lifecycle commit, kept
/// until that vault has opened through its new key. One copy per vault, in a
/// file named by a hash of the vault's real path; the map from that path to
/// the file name lives in UserDefaults.
struct MigrationRollbackStore: @unchecked Sendable {
    static let copiesKey = "tessera.migrationRollbackCopies.v1"
    static let legacyVaultPathKey = "tessera.migrationRollbackVaultPath"
    static let legacyFileName = "migration-rollback.json"
    private static let lock = OSAllocatedUnfairLock()

    let directory: URL
    private let defaults: UserDefaults

    init(directory: URL, defaults: UserDefaults) {
        self.directory = directory
        self.defaults = defaults
    }

    /// Where the copy for `vaultPath` is written.
    func fileURL(for vaultPath: String) -> URL {
        directory.appendingPathComponent(Self.fileName(for: SEKeyRegistry.normalizedPath(vaultPath)))
    }

    func save(_ bytes: Data, for vaultPath: String) throws(MigrationRollbackError) {
        Self.lock.lock()
        defer { Self.lock.unlock() }
        migrateLegacyCopy()
        let path = SEKeyRegistry.normalizedPath(vaultPath)
        do throws(POSIXError) {
            try DurableFile.makePrivateDirectory(directory)
        } catch {
            throw .io(operation: "mkdir", errno: error.code.rawValue)
        }
        do throws(VaultWriteError) {
            try DurableFile.replace(bytes, at: fileURL(for: path), hasDirectoryAccess: true)
        } catch {
            throw Self.rollbackError(error)
        }
        updateCopies { $0[path] = Self.fileName(for: path) }
    }

    /// The copy saved for `vaultPath`, while both the copy and its entry exist.
    func pending(for vaultPath: String) -> URL? {
        Self.lock.lock()
        defer { Self.lock.unlock() }
        migrateLegacyCopy()
        let path = SEKeyRegistry.normalizedPath(vaultPath)
        guard loadCopies()[path] != nil else { return nil }
        let url = fileURL(for: path)
        return FileManager.default.fileExists(atPath: url.path) ? url : nil
    }

    /// Removes the copy for `vaultPath` and its entry; the copies of other vaults stay.
    func discard(for vaultPath: String) throws(MigrationRollbackError) {
        Self.lock.lock()
        defer { Self.lock.unlock() }
        migrateLegacyCopy()
        let path = SEKeyRegistry.normalizedPath(vaultPath)
        do throws(VaultWriteError) {
            try DurableFile.remove(fileURL(for: path))
        } catch {
            throw Self.rollbackError(error)
        }
        updateCopies { $0[path] = nil }
    }

    static func fileName(for normalizedPath: String) -> String {
        let digest = SHA256.hash(data: Data(normalizedPath.utf8))
        return "rollback-" + digest.map { String(format: "%02x", $0) }.joined() + ".json"
    }

    /// Moves the single copy an earlier version kept to the name of its vault.
    /// A file left without its path is never pending, so it goes.
    private func migrateLegacyCopy() {
        let legacy = directory.appendingPathComponent(Self.legacyFileName)
        guard let legacyPath = defaults.string(forKey: Self.legacyVaultPathKey) else {
            unlink(legacy.path)
            return
        }
        let path = SEKeyRegistry.normalizedPath(legacyPath)
        let target = fileURL(for: path)
        var info = stat()
        if lstat(legacy.path, &info) == 0 {
            guard Darwin.rename(legacy.path, target.path) == 0 else { return }
            Self.syncDirectory(directory)
        }
        if FileManager.default.fileExists(atPath: target.path) {
            updateCopies { $0[path] = Self.fileName(for: path) }
        }
        defaults.removeObject(forKey: Self.legacyVaultPathKey)
    }

    private func updateCopies(_ change: (inout [String: String]) -> Void) {
        var copies = loadCopies()
        change(&copies)
        if copies.isEmpty {
            defaults.removeObject(forKey: Self.copiesKey)
        } else {
            defaults.set(copies, forKey: Self.copiesKey)
        }
    }

    /// Only entries whose file name is the one their path derives.
    private func loadCopies() -> [String: String] {
        guard let stored = defaults.dictionary(forKey: Self.copiesKey) else { return [:] }
        var out: [String: String] = [:]
        for (path, value) in stored {
            guard let name = value as? String, name == Self.fileName(for: path) else { continue }
            out[path] = name
        }
        return out
    }

    private static func rollbackError(_ error: VaultWriteError) -> MigrationRollbackError {
        if case let .io(operation, _, code) = error { return .io(operation: operation, errno: code) }
        return .io(operation: "write", errno: EIO)
    }

    private static func syncDirectory(_ directory: URL) {
        let fd = open(directory.path, O_RDONLY | O_CLOEXEC)
        guard fd >= 0 else { return }
        _ = fsync(fd)
        close(fd)
    }
}
