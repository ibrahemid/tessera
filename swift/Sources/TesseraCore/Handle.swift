import Foundation

/// Deterministic handle assignment and validation. A byte-for-byte mirror of the
/// Go reference (`go/internal/account/handle.go`): both cores MUST produce
/// identical handles for identical input, proven by /spec/testvectors.json.
public enum Handles {

    /// Handle charset: lowercase, leading letter, 1-12 chars.
    public static func isValid(_ h: String) -> Bool {
        let scalars = h.unicodeScalars
        guard let first = scalars.first, first >= "a", first <= "z" else { return false }
        guard scalars.count <= 12 else { return false }
        for s in scalars.dropFirst() {
            let isLower = s >= "a" && s <= "z"
            let isDigit = s >= "0" && s <= "9"
            if !isLower && !isDigit { return false }
        }
        return true
    }

    /// Lowercase, keep only [a-z0-9 ], collapse whitespace runs to single spaces,
    /// trim the ends.
    private static func normalizeSource(_ s: String) -> String {
        var out = ""
        for scalar in s.lowercased().unicodeScalars {
            if (scalar >= "a" && scalar <= "z") || (scalar >= "0" && scalar <= "9") || scalar == " " {
                out.unicodeScalars.append(scalar)
            }
        }
        return out.split(separator: " ").joined(separator: " ")
    }

    /// Derive the bare handle base: a normalized issuer (or the local part of
    /// account, or the literal "acct") yields two characters, x-prefixed when it
    /// would otherwise lead with a digit.
    private static func base(issuer: String, account: String) -> String {
        var src = normalizeSource(issuer)
        if src.isEmpty {
            var local = account
            if let at = account.firstIndex(of: "@") { local = String(account[..<at]) }
            src = normalizeSource(local)
        }
        if src.isEmpty { return "acct" }   // literal fallback, used verbatim as the base
        let words = src.split(separator: " ").map(String.init)
        var b: String
        if words.count == 1 {
            let w = words[0]
            b = w.count >= 2 ? String(w.prefix(2)) : w
        } else {
            b = String(words[0].first!) + String(words[1].first!)
        }
        if let f = b.unicodeScalars.first, f >= "0", f <= "9" { b = "x" + b }
        return b
    }

    /// Assign a deterministic handle to every account that lacks one, mutating
    /// `accounts` in place, and report whether any handle was assigned. Accounts
    /// that already have a handle (original or user-edited) are never changed or
    /// renumbered. Assignment order is ascending createdAt, then ascending id, so
    /// the result is independent of storage order. The smallest free integer N>=2
    /// disambiguates a base already taken by any existing or just-assigned handle.
    @discardableResult
    public static func assign(_ accounts: inout [Account]) -> Bool {
        var taken = Set<String>()
        var missing: [Int] = []
        for (i, a) in accounts.enumerated() {
            if a.handle.isEmpty { missing.append(i) } else { taken.insert(a.handle) }
        }
        if missing.isEmpty { return false }
        missing.sort { x, y in
            let ax = accounts[x], ay = accounts[y]
            if ax.createdAt != ay.createdAt { return ax.createdAt < ay.createdAt }
            return ax.id < ay.id
        }
        for i in missing {
            let b = base(issuer: accounts[i].issuer, account: accounts[i].account)
            var h = b
            var n = 2
            while taken.contains(h) { h = b + String(n); n += 1 }
            accounts[i].handle = h
            taken.insert(h)
        }
        return true
    }

    /// Throw if any account carries an invalid handle or two accounts share one.
    /// A vault-level invariant enforced before persisting.
    public static func checkUniqueness(_ accounts: [Account]) throws {
        var seen: [String: String] = [:]
        for a in accounts {
            if a.handle.isEmpty { continue }
            if !isValid(a.handle) {
                throw AccountError.invalid("account \(a.id): invalid handle \(a.handle)")
            }
            if let other = seen[a.handle] {
                throw AccountError.invalid("duplicate handle \(a.handle) on accounts \(other) and \(a.id)")
            }
            seen[a.handle] = a.id
        }
    }
}
