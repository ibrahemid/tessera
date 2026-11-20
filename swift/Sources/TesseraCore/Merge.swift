import Foundation

/// Combines two account lists by id the way the Go `merge` package does: the
/// newer `updatedAt` wins, an existing handle is kept, an HOTP counter never
/// moves back, a live account whose content another live account already holds
/// is refused as a duplicate, and an account that fails validation is refused.
public enum Merge {

    /// What one merge did to its destination.
    public struct Counts: Equatable, Sendable {
        public var added: Int
        public var updated: Int
        public var trashed: Int
        public var unchanged: Int
        public var duplicate: Int
        public var invalid: Int

        public init(added: Int = 0, updated: Int = 0, trashed: Int = 0,
                    unchanged: Int = 0, duplicate: Int = 0, invalid: Int = 0) {
            self.added = added
            self.updated = updated
            self.trashed = trashed
            self.unchanged = unchanged
            self.duplicate = duplicate
            self.invalid = invalid
        }

        /// Whether the destination differs from what it was: something was
        /// added, updated or moved to the trash.
        public var changed: Bool { added > 0 || updated > 0 || trashed > 0 }
    }

    /// The identity two accounts share when they hold the same code: type,
    /// lowercased issuer and account, and secret, each written as its byte
    /// length, a colon and the field, so no two field lists share a key.
    /// Import and dedupe use the same key, compared by bytes as Go compares
    /// strings.
    public struct ContentKey: Hashable, Sendable {
        let bytes: [UInt8]

        public init(_ a: Account) {
            var b: [UInt8] = []
            for field in [a.type.rawValue, GoText.toLower(a.issuer), GoText.toLower(a.account),
                          Base32.encodeNoPad(a.secret)] {
                let f = Array(field.utf8)
                b.append(contentsOf: Array(String(f.count).utf8))
                b.append(0x3A)
                b.append(contentsOf: f)
            }
            bytes = b
        }
    }

    /// Merges `source` into `destination`. Returns the destination's new list,
    /// destination accounts first in their order and added accounts after, with
    /// a repeated handle cleared after its first holder.
    public static func into(_ destination: [Account], _ source: [Account]) -> (accounts: [Account], counts: Counts) {
        var out = destination
        out.reserveCapacity(destination.count + source.count)
        var counts = Counts()
        var byID: [[UInt8]: Int] = [:]
        var live: [ContentKey: Int] = [:]
        for (i, a) in destination.enumerated() {
            byID[Array(a.id.utf8)] = i
            if !a.isTrashed { live[ContentKey(a), default: 0] += 1 }
        }
        for a in source {
            let id = Array(a.id.utf8)
            if let idx = byID[id] {
                let cur = out[idx]
                let (counter, raised) = hotpCounter(cur, a)
                if a.updatedAt <= cur.updatedAt {
                    guard raised else {
                        counts.unchanged += 1
                        continue
                    }
                    var kept = cur
                    kept.counter = counter
                    if (try? kept.validate()) == nil {
                        counts.invalid += 1
                        continue
                    }
                    out[idx] = kept
                    counts.updated += 1
                    continue
                }
                var next = a
                next.counter = counter
                if !cur.handle.isEmpty { next.handle = cur.handle }
                if (try? next.validate()) == nil {
                    counts.invalid += 1
                    continue
                }
                if !cur.isTrashed { live[ContentKey(cur), default: 0] -= 1 }
                if !next.isTrashed { live[ContentKey(next), default: 0] += 1 }
                if !cur.isTrashed && next.isTrashed {
                    counts.trashed += 1
                } else {
                    counts.updated += 1
                }
                out[idx] = next
                continue
            }
            if !a.isTrashed && live[ContentKey(a), default: 0] > 0 {
                counts.duplicate += 1
                continue
            }
            if (try? a.validate()) == nil {
                counts.invalid += 1
                continue
            }
            byID[id] = out.count
            if !a.isTrashed { live[ContentKey(a), default: 0] += 1 }
            out.append(a)
            counts.added += 1
        }
        dedupeHandles(&out)
        return (out, counts)
    }

    /// Merges each side into the other: `source` into `local`, and the
    /// original `local` into `source`.
    public static func twoWay(local: [Account], source: [Account])
        -> (local: [Account], source: [Account], localCounts: Counts, sourceCounts: Counts) {
        let here = into(local, source)
        let there = into(source, local)
        return (here.accounts, there.accounts, here.counts, there.counts)
    }

    /// The counter the merged copy of an id carries and whether it is larger
    /// than the destination's: the larger of the two when both copies are HOTP
    /// with equal secrets, else the source's.
    private static func hotpCounter(_ cur: Account, _ src: Account) -> (Int64, Bool) {
        guard cur.type == .hotp, src.type == .hotp, cur.secret == src.secret else { return (src.counter, false) }
        return (max(cur.counter, src.counter), src.counter > cur.counter)
    }

    /// Clears every non-empty handle already held by an earlier account.
    public static func dedupeHandles(_ accounts: inout [Account]) {
        var seen = Set<[UInt8]>()
        for i in accounts.indices where !accounts[i].handle.isEmpty {
            if !seen.insert(Array(accounts[i].handle.utf8)).inserted {
                accounts[i].handle = ""
            }
        }
    }
}
