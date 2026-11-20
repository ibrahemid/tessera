import Foundation
import TesseraCore

typealias AccountChange = @Sendable (inout [Account]) throws -> Void

enum CommitOutcome: Sendable {
    case committed(CommittedVault)
    case failed(any Error)
}

enum CommitFailurePolicy: Sendable {
    case notify
    case reload
    case keep
}

struct PersistenceState {
    var issued: UInt64 = 0
    var settled: UInt64 = 0
    var tail: Task<CommitOutcome, Never>?

    var hasPendingCommits: Bool { settled < issued }
}

@MainActor
protocol VaultPersisting: AnyObject {
    var accounts: [Account] { get set }
    var envelope: Envelope? { get set }
    var dek: Data? { get set }
    var lastVaultModified: Date? { get set }
    var notice: AppModel.Notice? { get set }
    var persistence: PersistenceState { get set }
    var lockGeneration: UInt64 { get }
    var isLocked: Bool { get }
    func makeVaultTransaction() -> any VaultTransaction
    func lock(reason: LockReason)
    func copyToPasteboard(_ value: String, kind: SecretClipboard.Kind, to pasteboard: any SecretPasteboard) -> Bool
    func announce(_ text: String)
}

enum VaultChange {
    static func apply(_ change: AccountChange, to accounts: inout [Account]) throws {
        try change(&accounts)
        Handles.assign(&accounts)
        try Handles.checkUniqueness(accounts)
    }

    static func commit(_ change: @escaping AccountChange, dek: Data,
                       through transaction: any VaultTransaction) async -> CommitOutcome {
        do {
            let committed = try await transaction.commit(heldDEK: dek) { opened in
                var next = opened.accounts
                try apply(change, to: &next)
                return VaultCommit(envelope: try opened.snapshot.envelope.reseal(accounts: next, dek: dek),
                                   accounts: next, dek: dek, backupPolicy: .copyCurrent)
            }
            return .committed(committed)
        } catch {
            return .failed(error)
        }
    }

    static func stamped(_ account: Account, at stamp: Int64 = Int64(Date().timeIntervalSince1970)) -> Account {
        var a = account
        if a.id.isEmpty { a.id = UUID().uuidString }
        if a.createdAt == 0 { a.createdAt = stamp }
        a.updatedAt = stamp
        return a
    }

    static func dedupeKey(_ a: Account) -> String {
        [String(describing: a.type), a.issuer.lowercased(), a.account.lowercased(),
         Base32.encodeNoPad(a.secret)].joined(separator: "|")
    }

    static func appendingNew(_ candidates: [Account]) -> AccountChange {
        { accounts in
            var seen = Set(accounts.map(dedupeKey))
            for a in candidates where seen.insert(dedupeKey(a)).inserted { accounts.append(a) }
        }
    }

    static func droppingDuplicates(_ accounts: [Account]) -> [Account] {
        var seen = Set<String>()
        return accounts.filter { seen.insert(dedupeKey($0)).inserted }
    }

    static func carryingEdits(from original: Account, to edited: Account, onto fresh: Account) -> Account {
        var a = fresh
        func carry<Value: Equatable>(_ key: WritableKeyPath<Account, Value>) {
            if edited[keyPath: key] != original[keyPath: key] { a[keyPath: key] = edited[keyPath: key] }
        }
        carry(\.type); carry(\.issuer); carry(\.account); carry(\.secret); carry(\.algorithm)
        carry(\.digits); carry(\.period); carry(\.counter); carry(\.folder); carry(\.handle)
        carry(\.tags); carry(\.pinned); carry(\.note); carry(\.deletedAt); carry(\.order)
        a.updatedAt = edited.updatedAt
        return a
    }

    static func replacing(_ id: String, from original: Account, with edited: Account) -> AccountChange {
        { accounts in
            guard let i = accounts.firstIndex(where: { $0.id == id }) else { return }
            accounts[i] = carryingEdits(from: original, to: edited, onto: accounts[i])
        }
    }
}

extension VaultPersisting {
    @discardableResult
    func mutate(_ change: @escaping AccountChange) -> Bool {
        do {
            try stage(change)
            return true
        } catch {
            reportFailure(error)
            return false
        }
    }

    @discardableResult
    func stage(_ change: @escaping AccountChange, showsBeforeCommit: Bool = true,
               onFailure policy: CommitFailurePolicy = .notify) throws -> Task<CommitOutcome, Never> {
        guard !isLocked, envelope != nil, let dek else { throw VaultError.wrongPassphrase }
        var next = accounts
        try VaultChange.apply(change, to: &next)
        if showsBeforeCommit { accounts = next }
        persistence.issued += 1
        let ticket = persistence.issued
        let generation = lockGeneration
        let transaction = makeVaultTransaction()
        let previous = persistence.tail
        let task = Task { @MainActor in
            _ = await previous?.value
            let outcome = await VaultChange.commit(change, dek: dek, through: transaction)
            await self.settle(outcome, ticket: ticket, generation: generation, policy: policy,
                              transaction: transaction, dek: dek)
            return outcome
        }
        persistence.tail = task
        return task
    }

    func settlePendingCommits() async {
        while let tail = persistence.tail {
            _ = await tail.value
            if persistence.tail == tail { return }
        }
    }

    func adopt(_ committed: CommittedVault) {
        envelope = committed.envelope
        accounts = committed.accounts
        dek = committed.dek
        lastVaultModified = committed.modifiedAt
    }

    func reportFailure(_ error: any Error) {
        if let text = UserMessage.textUnlessCancelled(for: error, context: .save) { notice = .failure(text) }
    }

    /// Migration: accounts written before handles existed gain one deterministically
    /// at unlock and the vault re-seals once (wraps untouched). The `assign` Bool
    /// guards the write, so a second unlock with handles already present is a no-op.
    /// A read-only external vault keeps the assigned handles in memory only; the
    /// same algorithm reproduces them on the next writable unlock.
    func migrateHandles() {
        guard !isLocked, envelope != nil, dek != nil else { return }
        var assigned = accounts
        guard Handles.assign(&assigned) else { return }
        do { try stage({ _ in }, onFailure: .keep) }
        catch { accounts = assigned }
    }

    private func settle(_ outcome: CommitOutcome, ticket: UInt64, generation: UInt64,
                        policy: CommitFailurePolicy, transaction: any VaultTransaction, dek: Data) async {
        defer { persistence.settled = ticket }
        guard generation == lockGeneration else { return }
        let isLatest = ticket == persistence.issued
        switch outcome {
        case .committed(let committed):
            if isLatest {
                adopt(committed)
            } else {
                envelope = committed.envelope
                lastVaultModified = committed.modifiedAt
            }
        case .failed(let error) where (error as? VaultWriteError) == .keyChanged:
            lock(reason: .keyChanged)
        case .failed(let error):
            guard policy != .keep else { return }
            if policy == .notify { reportFailure(error) }
            if isLatest { await reloadFromDisk(through: transaction, dek: dek, ticket: ticket, generation: generation) }
        }
    }

    private func reloadFromDisk(through transaction: any VaultTransaction, dek: Data,
                                ticket: UInt64, generation: UInt64) async {
        let snapshot = try? await transaction.read()
        guard generation == lockGeneration, ticket == persistence.issued else { return }
        if let snapshot, let fresh = try? snapshot.envelope.open(dek: dek) {
            envelope = snapshot.envelope
            accounts = fresh
            lastVaultModified = snapshot.modifiedAt
        } else if let held = try? envelope?.open(dek: dek) {
            accounts = held
        }
    }
}

extension AppModel: VaultPersisting {
    func makeVaultTransaction() -> any VaultTransaction { dependencies.makeTransaction(store.location) }

    func stamped(_ a: Account) -> Account { VaultChange.stamped(a) }
}
