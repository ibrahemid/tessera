import Foundation

/// The steps of a key lifecycle flow, in the order they can be reached.
enum MigrationStep: String, Sendable, CaseIterable {
    case rollbackSaved, registryWritten, keyItemWritten, keyReadBack, wrapVerified, committed
    case readBackVerified, backupHandled, oldKeyDeleted, rollbackDiscarded
}

/// Called after every step of a lifecycle flow. An error it reports as an
/// interruption stops the flow with nothing undone, as a crash would.
protocol MigrationCheckpoint: Sendable {
    func reached(_ step: MigrationStep) throws
    func isInterruption(_ error: any Error) -> Bool
}

extension MigrationCheckpoint {
    func isInterruption(_ error: any Error) -> Bool { false }
}

struct NoCheckpoint: MigrationCheckpoint {
    func reached(_ step: MigrationStep) {}
}

/// A wrap a rotation could not re-create.
enum RemovedWrap: Sendable, Equatable {
    case passphrase, secureEnclave
    case unknown(String)
}

struct MigrationOutcome: Sendable {
    enum State: Sendable, Equatable { case current, rotationPending }

    let committed: CommittedVault
    let state: State
    let removedWraps: [RemovedWrap]
    /// The salt of the AppKey wrap the flow wrote, when it re-created one.
    let appKeyWrapSalt: String?
}

enum MigrationError: Error, Equatable, Sendable {
    /// The legacy wrap the flow replaces is no longer in the file.
    case legacyWrapChanged
    /// The file's wraps changed between the open and the commit.
    case wrapsChanged
    /// The step at which the written file failed to open; the earlier bytes were restored.
    case verificationFailed(MigrationStep)
    /// The written file failed to open and putting the earlier bytes back failed too.
    case restoreFailed
}

/// The passphrase of an AppKey wrap and the salt that names that wrap.
struct AppKeyWrap: Sendable, Equatable {
    let passphrase: String
    let salt: String
}

struct ReconcileReport: Sendable, Equatable {
    var discardedRollback = false
    var deletedBackup = false
    var sweptKeys: [SEKeyID] = []
}
