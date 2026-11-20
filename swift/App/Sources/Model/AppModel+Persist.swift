import Foundation
import TesseraCore

struct PersistenceState {}

extension AppModel {
    @discardableResult
    func mutate(_ change: (inout [Account]) -> Void) -> Bool {
        run {
            var next = accounts
            change(&next)
            try persist(next)
        }
    }

    func persist(_ next: [Account]) throws {
        guard let env = envelope, let dek = dek else { throw VaultError.wrongPassphrase }
        // Any account added this write (import, manual entry) gets a handle here,
        // matching the CLI's save path so both cores stay in step.
        var next = next
        Handles.assign(&next)
        try Handles.checkUniqueness(next)
        let updated = try env.reseal(accounts: next, dek: dek)
        try store.save(updated)
        self.envelope = updated
        self.accounts = next
        self.lastVaultModified = store.modifiedAt   // our own write; don't reload it back
    }

    func adopt(_ committed: CommittedVault) {
        envelope = committed.envelope
        accounts = committed.accounts
        dek = committed.dek
        lastVaultModified = committed.modifiedAt
    }

    func stamped(_ a: Account) -> Account {
        var x = a
        if x.id.isEmpty { x.id = UUID().uuidString }
        let ts = Int64(Date().timeIntervalSince1970)
        if x.createdAt == 0 { x.createdAt = ts }
        x.updatedAt = ts
        return x
    }

    @discardableResult
    func run(_ body: () throws -> Void) -> Bool {
        do {
            try body()
            return true
        } catch {
            if let text = UserMessage.textUnlessCancelled(for: error, context: .save) { notice = .failure(text) }
            return false
        }
    }

    /// Migration: accounts written before handles existed gain one deterministically
    /// at unlock and the vault re-seals once (wraps untouched). The `assign` Bool
    /// guards the write, so a second unlock with handles already present is a no-op.
    /// A read-only external vault keeps the assigned handles in memory only; the
    /// same algorithm reproduces them on the next writable unlock.
    func migrateHandles() {
        guard !isLocked, envelope != nil, dek != nil else { return }
        var next = accounts
        guard Handles.assign(&next) else { return }
        do { try persist(next) }
        catch { accounts = next }   // couldn't write (e.g. read-only): keep in memory
    }
}
