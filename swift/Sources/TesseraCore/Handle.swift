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

    /// Lowercase with Go's simple mapping, keep only [a-z0-9 ], collapse space
    /// runs to single spaces and trim the ends. The result is ASCII bytes.
    private static func normalizeSource<S: Sequence>(_ scalars: S) -> [[UInt8]] where S.Element == Unicode.Scalar {
        var words: [[UInt8]] = []
        var word: [UInt8] = []
        for scalar in scalars {
            let v = CaseMap.lower(scalar.value)
            if (v >= 0x61 && v <= 0x7A) || (v >= 0x30 && v <= 0x39) {
                word.append(UInt8(v))
            } else if v == 0x20, !word.isEmpty {
                words.append(word)
                word = []
            }
        }
        if !word.isEmpty { words.append(word) }
        return words
    }

    /// Derive the bare handle base: a normalized issuer (or the local part of
    /// account, before its first `@` byte, or the literal "acct") yields two
    /// characters, x-prefixed when it would otherwise lead with a digit.
    private static func base(issuer: String, account: String) -> String {
        var words = normalizeSource(issuer.unicodeScalars)
        if words.isEmpty {
            let bytes = Array(account.utf8)
            let local = bytes.firstIndex(of: 0x40).map { Array(bytes[..<$0]) } ?? bytes
            words = normalizeSource(GoText.string(local).unicodeScalars)
        }
        if words.isEmpty { return "acct" }
        var b: [UInt8] = words.count == 1 ? Array(words[0].prefix(2)) : [words[0][0], words[1][0]]
        if b[0] >= 0x30 && b[0] <= 0x39 { b.insert(0x78, at: 0) }
        return String(decoding: b, as: UTF8.self)
    }

    /// Assign a deterministic handle to every account that lacks one, mutating
    /// `accounts` in place, and report whether any handle was assigned. Accounts
    /// that already have a handle (original or user-edited) are never changed or
    /// renumbered. Assignment order is ascending createdAt, then ascending id
    /// bytes, so the result is independent of storage order. A base already
    /// taken by an existing or just-assigned handle gets the smallest free
    /// integer N>=2 above the last one given to that base.
    @discardableResult
    public static func assign(_ accounts: inout [Account]) -> Bool {
        var taken = Set<[UInt8]>()
        var missing: [Int] = []
        for (i, a) in accounts.enumerated() {
            if a.handle.isEmpty { missing.append(i) } else { taken.insert(Array(a.handle.utf8)) }
        }
        if missing.isEmpty { return false }
        let ids = accounts.map { Array($0.id.utf8) }
        missing = missing.enumerated().sorted { x, y in
            let ax = accounts[x.element], ay = accounts[y.element]
            if ax.createdAt != ay.createdAt { return ax.createdAt < ay.createdAt }
            let ix = ids[x.element], iy = ids[y.element]
            if ix != iy { return StrictJSON.bytesLess(ix, iy) }
            return x.offset < y.offset
        }.map(\.element)
        var next: [String: Int] = [:]
        for i in missing {
            let b = base(issuer: accounts[i].issuer, account: accounts[i].account)
            var h = b
            if taken.contains(Array(h.utf8)) {
                var n = max(next[b] ?? 0, 2)
                h = b + String(n)
                while taken.contains(Array(h.utf8)) {
                    n += 1
                    h = b + String(n)
                }
                next[b] = n + 1
            }
            accounts[i].handle = h
            taken.insert(Array(h.utf8))
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
                throw AccountError.invalid("account \(GoQuote.quote(a.id)): invalid handle \(GoQuote.quote(a.handle))")
            }
            if let other = seen[a.handle] {
                throw AccountError.invalid(
                    "duplicate handle \(GoQuote.quote(a.handle)) on accounts \(GoQuote.quote(other)) and \(GoQuote.quote(a.id))")
            }
            seen[a.handle] = a.id
        }
    }
}
