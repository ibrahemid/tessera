import Foundation
import TesseraCore

enum TrashOrder {
    /// The trashed accounts, most recently trashed first and in the core
    /// display order within the same second, as `tess trash` lists them.
    static func newestFirst(_ accounts: [Account]) -> [Account] {
        Account.sortedForDisplay(Account.trashed(accounts)).enumerated().sorted { lhs, rhs in
            lhs.element.deletedAt != rhs.element.deletedAt
                ? lhs.element.deletedAt > rhs.element.deletedAt
                : lhs.offset < rhs.offset
        }.map(\.element)
    }
}
