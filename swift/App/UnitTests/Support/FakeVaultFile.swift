import Foundation
import os
import TesseraCore
@testable import Tessera

/// Thrown at an injected point to stand for the process stopping there.
struct SimulatedCrash: Error, Equatable {}

enum FakeDiskError: Error, Equatable {
    case writeFailed, readFailed, restoreFailed
}

/// One vault file and its `.bak`, kept across simulated launches, with faults
/// that fire inside a write, on the read after a write, and in a restore, and
/// a second writer that changes the file between a commit and its read-back.
final class FakeVaultDisk: Sendable {
    enum CommitFault: Sendable, Equatable, CaseIterable {
        case crashBeforeRename, failBeforeRename, crashAfterRename, failAfterRename
    }

    enum ReadFault: Sendable, Equatable { case crash, fail }

    enum RestoreFault: Sendable, Equatable { case fail, crashAfterWrite }

    private struct State {
        var vault: Data?
        var backup: Data?
        var residue: [Data] = []
        var writes = 0
        var commits = 0
        var transformRuns = 1
        var commitFaults: [Int: CommitFault] = [:]
        var readFaultAfterCommit: ReadFault?
        var armedReadFault: ReadFault?
        var restoreFault: RestoreFault?
        var writerAfterCommit: (@Sendable (Data) -> Data)?
        var armedWriter: (@Sendable (Data) -> Data)?
    }

    private let state = OSAllocatedUnfairLock(initialState: State())

    init(vault: Data? = nil) {
        state.withLock { $0.vault = vault }
    }

    var vault: Data? { state.withLock { $0.vault } }
    var backup: Data? { state.withLock { $0.backup } }
    var residue: [Data] { state.withLock { $0.residue } }
    var commits: Int { state.withLock { $0.commits } }
    var transformRuns: Int { state.withLock { $0.transformRuns } }

    func setVault(_ bytes: Data?) { state.withLock { $0.vault = bytes } }
    func setBackup(_ bytes: Data?) { state.withLock { $0.backup = bytes } }

    /// Runs every commit's transform this many times, as a compare-and-reapply retry would.
    func setTransformRuns(_ runs: Int) { state.withLock { $0.transformRuns = max(1, runs) } }

    /// `number` counts commits and creates from the start of this disk, from 1.
    func injectCommitFault(_ fault: CommitFault, onCommit number: Int) {
        state.withLock { $0.commitFaults[number] = fault }
    }

    func injectReadFaultAfterNextCommit(_ fault: ReadFault) {
        state.withLock { $0.readFaultAfterCommit = fault }
    }

    func injectRestoreFault(_ fault: RestoreFault) {
        state.withLock { $0.restoreFault = fault }
    }

    /// The next commit that lands arms `change`; the read after it writes
    /// `change(file)` first, as another app would, and then any read fault fires.
    func injectWriterAfterNextCommit(_ change: @escaping @Sendable (Data) -> Data) {
        state.withLock { $0.writerAfterCommit = change }
    }

    func clearFaults() {
        state.withLock {
            $0.commitFaults = [:]
            $0.readFaultAfterCommit = nil
            $0.armedReadFault = nil
            $0.restoreFault = nil
            $0.writerAfterCommit = nil
            $0.armedWriter = nil
            $0.transformRuns = 1
        }
    }

    func read() throws -> (bytes: Data, modifiedAt: Date)? {
        let (bytes, writes, fault) = state.withLock { s -> (Data?, Int, ReadFault?) in
            if let writer = s.armedWriter, let current = s.vault {
                s.vault = writer(current)
                s.writes += 1
            }
            s.armedWriter = nil
            let fault = s.armedReadFault
            s.armedReadFault = nil
            return (s.vault, s.writes, fault)
        }
        switch fault {
        case .crash?: throw SimulatedCrash()
        case .fail?: throw FakeDiskError.readFailed
        case nil: break
        }
        return bytes.map { ($0, Self.date(writes)) }
    }

    /// The bytes as they are, for a write's own read; no fault fires.
    func current() -> (bytes: Data, modifiedAt: Date)? {
        state.withLock { s in s.vault.map { ($0, Self.date(s.writes)) } }
    }

    /// Writes `bytes` as a commit: `.bak` first when asked, then a temp file, then the rename.
    func write(_ bytes: Data, copyingBackup: Bool) throws -> Date {
        let (fault, written) = state.withLock { s -> (CommitFault?, Date) in
            s.commits += 1
            let fault = s.commitFaults.removeValue(forKey: s.commits)
            if copyingBackup, let current = s.vault { s.backup = current }
            if fault == .crashBeforeRename || fault == .failBeforeRename {
                s.residue.append(bytes)
                return (fault, Self.date(s.writes))
            }
            s.vault = bytes
            s.writes += 1
            s.armedReadFault = s.readFaultAfterCommit
            s.readFaultAfterCommit = nil
            s.armedWriter = s.writerAfterCommit
            s.writerAfterCommit = nil
            return (fault, Self.date(s.writes))
        }
        switch fault {
        case .crashBeforeRename?, .crashAfterRename?: throw SimulatedCrash()
        case .failBeforeRename?, .failAfterRename?: throw FakeDiskError.writeFailed
        case nil: return written
        }
    }

    func restore(_ bytes: Data) throws {
        _ = try restore(bytes, ifStill: nil)
    }

    /// Writes `bytes` only while the file holds `committed`, when it is set.
    func restore(_ bytes: Data, ifStill committed: Data?) throws -> RestoreOutcome {
        try changeIfStill(committed) { $0.vault = bytes }
    }

    /// Removes the file only while it holds `committed`.
    func remove(ifStill committed: Data) throws -> RestoreOutcome {
        try changeIfStill(committed) { $0.vault = nil }
    }

    private func changeIfStill(_ committed: Data?,
                               _ change: @Sendable (inout State) -> Void) throws -> RestoreOutcome {
        let (fault, outcome) = state.withLock { s -> (RestoreFault?, RestoreOutcome) in
            if let committed, s.vault != committed { return (nil, .changedSince) }
            let fault = s.restoreFault
            s.restoreFault = nil
            if fault != .fail {
                change(&s)
                s.writes += 1
            }
            return (fault, .restored)
        }
        switch fault {
        case .fail?: throw FakeDiskError.restoreFailed
        case .crashAfterWrite?: throw SimulatedCrash()
        case nil: return outcome
        }
    }

    private static func date(_ writes: Int) -> Date { Date(timeIntervalSince1970: TimeInterval(writes)) }
}

/// A `VaultTransaction` over a `FakeVaultDisk`, with the writer's checks:
/// a missing or unparseable file, a held DEK that no longer opens it, a
/// create over an existing file, and a commit without wraps all throw.
struct FakeVaultFile: VaultTransaction {
    let location: VaultLocation
    let disk: FakeVaultDisk

    func read() async throws -> VaultSnapshot {
        guard let (bytes, modifiedAt) = try disk.read() else { throw VaultWriteError.missingFile(location.fileURL) }
        return try snapshot(bytes, modifiedAt: modifiedAt, url: location.fileURL)
    }

    func commit(heldDEK: Data, _ transform: @escaping @Sendable (OpenedVault) throws -> VaultCommit) async throws -> CommittedVault {
        guard let (bytes, modifiedAt) = disk.current() else { throw VaultWriteError.missingFile(location.fileURL) }
        let fresh = try snapshot(bytes, modifiedAt: modifiedAt, url: location.fileURL)
        let accounts: [Account]
        do {
            accounts = try fresh.envelope.open(dek: heldDEK)
        } catch VaultError.wrongPassphrase {
            throw VaultWriteError.keyChanged
        } catch {
            throw VaultWriteError.unparseable(location.fileURL)
        }
        var next: VaultCommit?
        for _ in 0..<disk.transformRuns {
            next = try transform(OpenedVault(snapshot: fresh, accounts: accounts))
        }
        guard let next else { throw VaultWriteError.noWraps }
        return try write(next)
    }

    func create(_ commit: VaultCommit) async throws -> CommittedVault {
        guard disk.current() == nil else { throw VaultWriteError.alreadyExists(location.fileURL) }
        return try write(commit)
    }

    func restore(verbatim bytes: Data) async throws {
        try disk.restore(bytes)
    }

    func restore(verbatim bytes: Data, ifStill committed: Data) async throws -> RestoreOutcome {
        try disk.restore(bytes, ifStill: committed)
    }

    func removeCreated(ifStill committed: Data) async throws -> RestoreOutcome {
        try disk.remove(ifStill: committed)
    }

    func readBackup() async throws -> VaultSnapshot? {
        guard location.hasDirectoryAccess, let bytes = disk.backup else { return nil }
        return try snapshot(bytes, modifiedAt: nil, url: VaultSidecar.backupURL(for: location.fileURL))
    }

    func deleteBackup() async throws {
        guard location.hasDirectoryAccess else { return }
        disk.setBackup(nil)
    }

    private func write(_ commit: VaultCommit) throws -> CommittedVault {
        guard !commit.envelope.wraps.isEmpty else { throw VaultWriteError.noWraps }
        let bytes = try commit.envelope.encoded()
        let copyingBackup = commit.backupPolicy == .copyCurrent && location.hasDirectoryAccess
        let modifiedAt = try disk.write(bytes, copyingBackup: copyingBackup)
        return CommittedVault(envelope: commit.envelope, accounts: commit.accounts, dek: commit.dek, bytes: bytes,
                              modifiedAt: modifiedAt)
    }

    private func snapshot(_ bytes: Data, modifiedAt: Date?, url: URL) throws -> VaultSnapshot {
        guard let envelope = try? Envelope.decode(bytes) else { throw VaultWriteError.unparseable(url) }
        return VaultSnapshot(bytes: bytes, envelope: envelope, modifiedAt: modifiedAt)
    }
}
