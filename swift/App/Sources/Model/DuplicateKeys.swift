import Foundation
import TesseraCore

enum DuplicateKeys {
    /// The content two accounts share when they give the same codes.
    static func key(_ a: Account) -> Merge.ContentKey { Merge.ContentKey(a) }

    /// The keys of the live accounts; a trashed account never makes a duplicate.
    static func live(in accounts: [Account]) -> Set<Merge.ContentKey> {
        Set(Account.live(accounts).map(key))
    }
}
