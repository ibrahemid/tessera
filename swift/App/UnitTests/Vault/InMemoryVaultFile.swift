import AppKit
import Foundation
import os
import TesseraCore
import XCTest
@testable import Tessera

final class InMemoryVaultFile: VaultTransaction {
    private struct State {
        var envelope: Envelope
        var revision = 0
        var isHeld = false
        var waiters: [CheckedContinuation<Void, Never>] = []
        var failures: [any Error] = []
    }

    let location = VaultLocation(fileURL: FileManager.default.temporaryDirectory
        .appendingPathComponent("in-memory-\(UUID().uuidString).json"), access: .container)
    private let state: OSAllocatedUnfairLock<State>

    init(envelope: Envelope) {
        state = OSAllocatedUnfairLock(initialState: State(envelope: envelope))
    }

    var envelope: Envelope { state.withLock { $0.envelope } }

    var waitingCommits: Int { state.withLock { $0.waiters.count } }

    func accounts(dek: Data) throws -> [Account] { try envelope.open(dek: dek) }

    func hold() { state.withLock { $0.isHeld = true } }

    func release() {
        let waiters = state.withLock { s -> [CheckedContinuation<Void, Never>] in
            s.isHeld = false
            defer { s.waiters = [] }
            return s.waiters
        }
        waiters.forEach { $0.resume() }
    }

    func failNextCommit(with error: any Error) { state.withLock { $0.failures.append(error) } }

    func externalAdd(_ account: Account, dek: Data) throws {
        try state.withLock { s in
            let next = try s.envelope.open(dek: dek) + [account]
            s.envelope = try s.envelope.reseal(accounts: next, dek: dek)
            s.revision += 1
        }
    }

    func replace(with envelope: Envelope) {
        state.withLock { s in
            s.envelope = envelope
            s.revision += 1
        }
    }

    func read() async throws -> VaultSnapshot {
        try state.withLock { try Self.snapshot($0) }
    }

    func commit(heldDEK: Data,
                _ transform: @escaping @Sendable (OpenedVault) throws -> VaultCommit) async throws -> CommittedVault {
        await passGate()
        if let failure = state.withLock({ s in s.failures.isEmpty ? nil : s.failures.removeFirst() }) {
            throw failure
        }
        return try state.withLock { s in
            let accounts: [Account]
            do { accounts = try s.envelope.open(dek: heldDEK) }
            catch VaultError.wrongPassphrase { throw VaultWriteError.keyChanged }
            let commit = try transform(OpenedVault(snapshot: try Self.snapshot(s), accounts: accounts))
            s.envelope = commit.envelope
            s.revision += 1
            return CommittedVault(envelope: commit.envelope, accounts: commit.accounts, dek: commit.dek,
                                  bytes: try commit.envelope.encoded(), modifiedAt: Self.modified(s.revision))
        }
    }

    func create(_ commit: VaultCommit) async throws -> CommittedVault {
        try state.withLock { s in
            s.envelope = commit.envelope
            s.revision += 1
            return CommittedVault(envelope: commit.envelope, accounts: commit.accounts, dek: commit.dek,
                                  bytes: try commit.envelope.encoded(), modifiedAt: Self.modified(s.revision))
        }
    }

    func restore(verbatim bytes: Data) async throws {
        let envelope = try Envelope.decode(bytes)
        replace(with: envelope)
    }

    func restore(verbatim bytes: Data, ifStill committed: Data) async throws -> RestoreOutcome {
        let envelope = try Envelope.decode(bytes)
        return try state.withLock { s in
            guard try s.envelope.encoded() == committed else { return .changedSince }
            s.envelope = envelope
            s.revision += 1
            return .restored
        }
    }

    /// This file always exists, so a removal fails.
    func removeCreated(ifStill committed: Data) async throws -> RestoreOutcome {
        throw VaultWriteError.io(operation: "remove", url: location.fileURL, errno: ENOTSUP)
    }

    func readBackup() async throws -> VaultSnapshot? { nil }

    func deleteBackup() async throws {}

    private func passGate() async {
        await withCheckedContinuation { (continuation: CheckedContinuation<Void, Never>) in
            let passes = state.withLock { s -> Bool in
                guard s.isHeld else { return true }
                s.waiters.append(continuation)
                return false
            }
            if passes { continuation.resume() }
        }
    }

    private static func snapshot(_ s: State) throws -> VaultSnapshot {
        VaultSnapshot(bytes: try s.envelope.encoded(), envelope: s.envelope, modifiedAt: modified(s.revision))
    }

    private static func modified(_ revision: Int) -> Date { Date(timeIntervalSince1970: TimeInterval(revision)) }
}

@MainActor
final class PersistHost: VaultPersisting {
    var accounts: [Account] = [] { didSet { history.append(accounts) } }
    var envelope: Envelope?
    var dek: Data?
    var lastVaultModified: Date?
    var notice: AppModel.Notice?
    var persistence = PersistenceState()
    private(set) var lockGeneration: UInt64 = 0
    private(set) var isLocked = false
    private(set) var history: [[Account]] = []
    private(set) var lockReasons: [LockReason] = []
    private(set) var copied: [String] = []
    var copySucceeds = true
    let file: InMemoryVaultFile

    init(file: InMemoryVaultFile, dek: Data) throws {
        self.file = file
        self.dek = dek
        envelope = file.envelope
        accounts = try file.accounts(dek: dek)
    }

    func makeVaultTransaction() -> any VaultTransaction { file }

    func lock(reason: LockReason) {
        lockGeneration &+= 1
        lockReasons.append(reason)
        isLocked = true
        accounts = []
        envelope = nil
        dek = nil
    }

    func unlock(dek: Data) {
        isLocked = false
        self.dek = dek
        envelope = file.envelope
        accounts = (try? file.accounts(dek: dek)) ?? []
    }

    func rotateKey() async -> Data? {
        guard let held = dek else { return nil }
        let committed = try? await file.commit(heldDEK: held) { opened in
            let (envelope, dek) = try VaultFiles.makeEnvelope(accounts: opened.accounts)
            return VaultCommit(envelope: envelope, accounts: opened.accounts, dek: dek, backupPolicy: .copyCurrent)
        }
        guard let committed else { return nil }
        adopt(committed)
        return committed.dek
    }

    func copyToPasteboard(_ value: String, kind: SecretClipboard.Kind, to pasteboard: any SecretPasteboard) -> Bool {
        guard copySucceeds else { return false }
        copied.append(value)
        return true
    }

    func announce(_ text: String) { notice = .info(text) }
}

@MainActor
final class DiscardingPasteboard: SecretPasteboard {
    private(set) var changeCount = 0

    func prepareForNewContents(with options: NSPasteboard.ContentsOptions) -> Int {
        changeCount += 1
        return changeCount
    }

    func writeObjects(_ objects: [any NSPasteboardWriting]) -> Bool { false }

    @discardableResult
    func clearContents() -> Int {
        changeCount += 1
        return changeCount
    }
}

@MainActor
final class TestGate {
    private var waiters: [CheckedContinuation<Void, Never>] = []
    private var isOpen = false

    var isWaiting: Bool { !waiters.isEmpty }

    func wait() async {
        guard !isOpen else { return }
        await withCheckedContinuation { waiters.append($0) }
    }

    func open() {
        isOpen = true
        let resumed = waiters
        waiters = []
        resumed.forEach { $0.resume() }
    }
}

@MainActor
func waitUntil(_ condition: () -> Bool, file: StaticString = #filePath, line: UInt = #line) async {
    for _ in 0..<2_000 where !condition() { try? await Task.sleep(for: .milliseconds(1)) }
    XCTAssertTrue(condition(), file: file, line: line)
}
