import Foundation

extension Importers {

    struct RaivoEntry {
        var issuer = ""
        var account = ""
        var secret = ""
        var algorithm = ""
        var digits = ""
        var kind = ""
        var timer = ""
        var counter = ""
        var pinned: Int?
    }

    static func parseRaivo(_ doc: GoJSON) throws -> [Account] {
        var entries = GoSlice<RaivoEntry>()
        try wrapping("") {
            let d = GoJSONDecoder(doc, root: "")
            try d.slice(doc.root, &entries, type: "[]importers.raivoEntry", zero: RaivoEntry()) { e, x in
                try d.object(e, type: "importers.raivoEntry",
                             fields: ["issuer", "account", "secret", "algorithm", "digits", "kind", "timer", "counter", "pinned"]) { f, v in
                    switch f {
                    case 0: try d.string(v, &x.issuer)
                    case 1: try d.string(v, &x.account)
                    case 2: try d.string(v, &x.secret)
                    case 3: try d.string(v, &x.algorithm)
                    case 4: try d.string(v, &x.digits)
                    case 5: try d.string(v, &x.kind)
                    case 6: try d.string(v, &x.timer)
                    case 7: try d.string(v, &x.counter)
                    default: x.pinned = v
                    }
                }
            }
        }
        var out: [Account] = []
        out.reserveCapacity(entries.count)
        for e in entries.items {
            // Raivo stores numbers as strings.
            let label = "raivo entry \(quoteBounded(e.issuer))"
            if e.secret.isEmpty { throw ImporterError.malformed(.missingSecret, "\(label): missing secret") }
            let digits = try wrapping("\(label): digits: ") { try atoiDefault(e.digits, 6) }
            let period = try wrapping("\(label): timer: ") { try atoiDefault(e.timer, 30) }
            let counter = try wrapping("\(label): counter: ") { try atoiDefault(e.counter, 0) }
            var a = try wrapping("\(label): ") {
                try buildAccount(type: e.kind, issuer: e.issuer, acct: e.account, secretB32: e.secret,
                                 algo: e.algorithm, digits: digits, period: period, counter: Int64(counter))
            }
            a.pinned = rawString(doc, e.pinned) == "true" || rawIsTrue(doc, e.pinned)
            out.append(a)
        }
        return out
    }
}
