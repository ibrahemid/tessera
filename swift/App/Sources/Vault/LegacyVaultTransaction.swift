import Foundation
import TesseraCore

struct LegacyVaultTransaction: VaultTransaction {
    let location: VaultLocation

    func read() async throws -> VaultSnapshot {
        try withAccess { try loadSnapshot() }
    }

    func commit(heldDEK: Data, _ transform: @escaping @Sendable (OpenedVault) throws -> VaultCommit) async throws -> CommittedVault {
        try withAccess {
            let snapshot = try loadSnapshot()
            let accounts: [Account]
            do { accounts = try snapshot.envelope.open(dek: heldDEK) }
            catch VaultError.wrongPassphrase { throw VaultWriteError.keyChanged }
            catch { throw VaultWriteError.unparseable(location.fileURL) }
            let next = try transform(OpenedVault(snapshot: snapshot, accounts: accounts))
            return try write(next)
        }
    }

    func create(_ commit: VaultCommit) async throws -> CommittedVault {
        try withAccess {
            guard !FileManager.default.fileExists(atPath: location.fileURL.path) else {
                throw VaultWriteError.alreadyExists(location.fileURL)
            }
            return try write(commit)
        }
    }

    func restore(verbatim bytes: Data) async throws {
        try withAccess { try writeBytes(bytes) }
    }

    func readBackup() async throws -> VaultSnapshot? { nil }

    func deleteBackup() async throws {}

    private func loadSnapshot() throws -> VaultSnapshot {
        let url = location.fileURL
        let bytes: Data
        do { bytes = try Data(contentsOf: url) }
        catch let e as CocoaError where e.code == .fileReadNoSuchFile { throw VaultWriteError.missingFile(url) }
        let envelope: Envelope
        do { envelope = try Envelope.decode(bytes) }
        catch { throw VaultWriteError.unparseable(url) }
        return VaultSnapshot(bytes: bytes, envelope: envelope, modifiedAt: modificationDate())
    }

    private func write(_ commit: VaultCommit) throws -> CommittedVault {
        let bytes = try commit.envelope.encoded()
        try writeBytes(bytes)
        return CommittedVault(envelope: commit.envelope, accounts: commit.accounts, dek: commit.dek,
                              bytes: bytes, modifiedAt: modificationDate())
    }

    private func writeBytes(_ bytes: Data) throws {
        let url = location.fileURL
        let fm = FileManager.default
        if location.access == .fileOnly {
            try bytes.write(to: url, options: [.atomic])
        } else {
            try fm.createDirectory(at: url.deletingLastPathComponent(),
                                   withIntermediateDirectories: true,
                                   attributes: [.posixPermissions: 0o700])
            try bytes.write(to: url, options: [.atomic, .completeFileProtection])
            try? fm.setAttributes([.posixPermissions: 0o600], ofItemAtPath: url.path)
        }
    }

    private func modificationDate() -> Date? {
        (try? FileManager.default.attributesOfItem(atPath: location.fileURL.path)[.modificationDate]) as? Date
    }

    private func withAccess<T>(_ body: () throws -> T) rethrows -> T {
        guard location.access == .fileOnly else { return try body() }
        let url = location.fileURL
        let scoped = url.startAccessingSecurityScopedResource()
        defer { if scoped { url.stopAccessingSecurityScopedResource() } }
        return try body()
    }
}
