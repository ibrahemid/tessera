import Foundation

extension Importers {

    struct AndOTPEntry {
        var secret = ""
        var issuer = ""
        var label = ""
        var digits = 0
        var type = ""
        var algorithm = ""
        var period = 0
        var counter: Int64 = 0
        var tags: Int?
    }

    static func parseAndOTP(_ doc: GoJSON) throws -> [Account] {
        var entries = GoSlice<AndOTPEntry>()
        try wrapping("andotp: ") {
            let d = GoJSONDecoder(doc, root: "")
            try d.slice(doc.root, &entries, type: "[]importers.andOTPEntry", zero: AndOTPEntry()) { e, x in
                try d.object(e, type: "importers.andOTPEntry",
                             fields: ["secret", "issuer", "label", "digits", "type", "algorithm", "period", "counter", "tags"]) { f, v in
                    switch f {
                    case 0: try d.string(v, &x.secret)
                    case 1: try d.string(v, &x.issuer)
                    case 2: try d.string(v, &x.label)
                    case 3: try d.int(v, &x.digits)
                    case 4: try d.string(v, &x.type)
                    case 5: try d.string(v, &x.algorithm)
                    case 6: try d.int(v, &x.period)
                    case 7: try d.int64(v, &x.counter)
                    default: x.tags = v
                    }
                }
            }
        }
        var out: [Account] = []
        out.reserveCapacity(entries.count)
        for e in entries.items {
            var a: Account
            do {
                a = try buildAccount(
                    type: e.type, issuer: e.issuer, acct: e.label, secretB32: e.secret,
                    algo: e.algorithm, digits: e.digits, period: e.period, counter: e.counter)
            } catch {
                throw entryError("andotp entry \(quoteBounded(e.issuer)): ", error)
            }
            a.tags = cleanTags(rawStrings(doc, e.tags))
            out.append(a)
        }
        return out
    }
}
