import Foundation
import TesseraCore

struct VaultLocation: Sendable, Equatable {
    enum Access: Sendable, Equatable { case container, folder, fileOnly }
    let fileURL: URL
    let access: Access
    var hasDirectoryAccess: Bool { access != .fileOnly }
}

enum VaultSidecar {
    static func backupURL(for vault: URL) -> URL { URL(fileURLWithPath: vault.path + ".bak") }
    static func lockURL(for vault: URL) -> URL { URL(fileURLWithPath: vault.path + ".lock") }
    static let tempPrefix = ".tess-"
    static let tempSuffix = ".tmp"
}

struct VaultSnapshot: Sendable {
    let bytes: Data
    let envelope: Envelope
    let modifiedAt: Date?
}

struct OpenedVault: Sendable {
    let snapshot: VaultSnapshot
    let accounts: [Account]
}

enum BackupPolicy: Sendable { case copyCurrent, keep }

struct VaultCommit: Sendable {
    let envelope: Envelope
    let accounts: [Account]
    let dek: Data
    let backupPolicy: BackupPolicy
}

struct CommittedVault: Sendable {
    let envelope: Envelope
    let accounts: [Account]
    let dek: Data
    let bytes: Data
    let modifiedAt: Date?
}

struct DamagedVaultInfo: Sendable, Equatable {
    let vault: URL
    let backup: URL?
    let rollback: URL?
}

protocol VaultTransaction: Sendable {
    var location: VaultLocation { get }
    func read() async throws -> VaultSnapshot
    func commit(heldDEK: Data, _ transform: @escaping @Sendable (OpenedVault) throws -> VaultCommit) async throws -> CommittedVault
    func create(_ commit: VaultCommit) async throws -> CommittedVault
    func restore(verbatim bytes: Data) async throws
    func readBackup() async throws -> VaultSnapshot?
    func deleteBackup() async throws
}

enum VaultLockError: Error, Equatable {
    case timedOut(URL), symlink(URL), notRegularFile(URL), wrongOwner(URL), wrongMode(URL, mode: UInt16), io(URL, errno: Int32)
}

enum VaultWriteError: Error, Equatable {
    case keyChanged, missingFile(URL), alreadyExists(URL), unparseable(URL), noWraps
    case concurrentChange(URL), lock(VaultLockError), io(operation: String, url: URL, errno: Int32)
}
