import Darwin
import Foundation
import TesseraCore

struct DamagedVaultRestore: Sendable {
    let location: VaultLocation
    let queue: VaultIOQueue

    init(location: VaultLocation, queue: VaultIOQueue = .shared) {
        self.location = location
        self.queue = queue
    }

    static func damagedURL(for vault: URL) -> URL {
        URL(fileURLWithPath: vault.path + ".damaged")
    }

    func run(from source: URL, confirm: @MainActor @Sendable () -> Bool) async throws -> Bool {
        let location = location
        guard location.hasDirectoryAccess else {
            throw VaultWriteError.io(operation: "restore", url: location.fileURL, errno: EACCES)
        }
        _ = try await queue.run { try Self.verifiedBytes(at: source) }
        guard await confirm() else { return false }
        try await queue.run {
            let target = DurableFile.resolvedTarget(location.fileURL)
            let lock: VaultFileLock
            do throws(VaultLockError) {
                lock = try VaultFileLock.acquire(for: target)
            } catch {
                throw VaultWriteError.lock(error)
            }
            defer { lock.release() }
            let bytes = try Self.verifiedBytes(at: source)
            if DurableFile.isSymlink(target) { throw VaultWriteError.missingFile(target) }
            if FileManager.default.fileExists(atPath: target.path) {
                try DurableFile.preserve(target, as: Self.damagedURL(for: target))
            }
            try DurableFile.replace(bytes, at: target, hasDirectoryAccess: true,
                                    fileProtection: location.access == .container)
        }
        return true
    }

    private static func verifiedBytes(at source: URL) throws -> Data {
        let bytes = try DurableFile.read(source)
        guard (try? Envelope.decode(bytes)) != nil else { throw VaultWriteError.unparseable(source) }
        return bytes
    }
}
