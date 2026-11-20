import Foundation
import TesseraCore

/// What "Remove duplicates" will remove, computed before anything is deleted so
/// the confirmation can name it. Same rule as `AppModel.removeDuplicates`: the
/// first account with a given key stays, every later one with that key goes.
enum DuplicateScan {
    static func redundant<Key: Hashable>(in accounts: [Account], key: (Account) -> Key) -> [Account] {
        var seen = Set<Key>()
        var extra: [Account] = []
        for a in accounts {
            if seen.insert(key(a)).inserted { continue }
            extra.append(a)
        }
        return extra
    }

    /// Confirmation wording for removing `removed`. Names up to `listLimit`
    /// accounts and counts the rest.
    static func confirmation(for removed: [Account], listLimit: Int = 4) -> (title: String, message: String, action: String) {
        let n = removed.count
        let noun = n == 1 ? "duplicate account" : "duplicate accounts"
        var names = removed.prefix(listLimit).map(label)
        if n > listLimit { names.append("\(n - listLimit) more") }
        let list = ListFormatter.localizedString(byJoining: names)
        return (title: "Remove \(n) \(noun)?",
                message: "Removes the later copy of \(list). The first copy of each stays. A folder, pin or alias set only on a removed copy is lost.",
                action: n == 1 ? "Remove duplicate" : "Remove \(n) duplicates")
    }

    private static func label(_ a: Account) -> String {
        switch (a.issuer.isEmpty, a.account.isEmpty) {
        case (false, false): return "\(a.issuer) (\(a.account))"
        case (false, true): return a.issuer
        case (true, false): return a.account
        case (true, true): return "an unnamed account"
        }
    }
}

extension AppModel {
    /// Accounts `removeDuplicates()` would remove right now.
    var duplicatesToRemove: [Account] {
        DuplicateScan.redundant(in: accounts, key: dedupeKey)
    }
}
