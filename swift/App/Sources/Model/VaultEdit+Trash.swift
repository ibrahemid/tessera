import Foundation
import TesseraCore

extension VaultEdit {
    /// Moves the live accounts among `ids` to the Trash: `deletedAt` and
    /// `updatedAt` both become `stamp`. Trashed and unknown ids are skipped,
    /// and a non-positive stamp changes nothing. Returns how many moved.
    @discardableResult
    static func trash(_ ids: Set<String>, at stamp: Int64, in accounts: inout [Account]) -> Int {
        guard stamp > 0 else { return 0 }
        var moved = 0
        for i in accounts.indices where ids.contains(accounts[i].id) && !accounts[i].isTrashed {
            accounts[i].deletedAt = stamp
            accounts[i].updatedAt = stamp
            moved += 1
        }
        return moved
    }

    /// Brings the trashed accounts among `ids` back: `deletedAt` is cleared and
    /// `updatedAt` becomes `stamp`. Live and unknown ids are skipped. Returns
    /// how many came back.
    @discardableResult
    static func restore(_ ids: Set<String>, at stamp: Int64, in accounts: inout [Account]) -> Int {
        restore(at: stamp, in: &accounts) { ids.contains($0.id) }
    }

    /// Restores only the accounts still in the Trash with the `deletedAt` that
    /// `stamps` recorded for their id, so a change made since then is kept.
    @discardableResult
    static func restoreIfStamped(_ stamps: [String: Int64], at stamp: Int64, in accounts: inout [Account]) -> Int {
        restore(at: stamp, in: &accounts) { stamps[$0.id] == $0.deletedAt }
    }

    /// Deletes the `snapshot` accounts for good, but only when every one of them
    /// is still in `accounts` exactly as snapshotted; otherwise nothing is
    /// removed. `changed` counts the snapshot accounts that are gone or differ.
    static func purge(unchangedSince snapshot: [Account], in accounts: inout [Account]) -> (removed: Int, changed: Int) {
        var wanted: [String: Account] = [:]
        for a in snapshot { wanted[a.id] = a }
        var current: [String: Account] = [:]
        for a in accounts where wanted[a.id] != nil { current[a.id] = a }
        let changed = wanted.values.filter { want in
            current[want.id].map { !isIdentical($0, want) } ?? true
        }.count
        guard changed == 0 else { return (0, changed) }
        let before = accounts.count
        accounts.removeAll { wanted[$0.id] != nil }
        return (before - accounts.count, 0)
    }

    private static func restore(at stamp: Int64, in accounts: inout [Account],
                                where shouldRestore: (Account) -> Bool) -> Int {
        var restored = 0
        for i in accounts.indices where accounts[i].isTrashed && shouldRestore(accounts[i]) {
            accounts[i].deletedAt = 0
            accounts[i].updatedAt = stamp
            restored += 1
        }
        return restored
    }

    /// Byte equality of the serialized accounts, the comparison Go makes with
    /// `reflect.DeepEqual`.
    private static func isIdentical(_ a: Account, _ b: Account) -> Bool {
        CanonicalJSON.encode([a]) == CanonicalJSON.encode([b])
    }
}
