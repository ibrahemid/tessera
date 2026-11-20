import Foundation

extension Importers {

    struct FreeOTPPlusToken {
        var issuerInt = ""
        var issuerExt = ""
        var issuerAlt = ""
        var label = ""
        var labelAlt = ""
        var type = ""
        var algo = ""
        var secret = GoSlice<Int>()
        var digits = 0
        var counter: Int64 = 0
        var period = 0
    }

    static func parseFreeOTPPlus(_ doc: GoJSON) throws -> [Account] {
        var tokens = GoSlice<FreeOTPPlusToken>()
        var tokenOrder = GoSlice<String>()
        try wrapping("freeotp+: ") {
            let d = GoJSONDecoder(doc, root: "freeOTPPlusFile")
            try d.object(doc.root, type: "importers.freeOTPPlusFile", fields: ["tokens", "tokenOrder"]) { f, v in
                if f == 1 {
                    try d.slice(v, &tokenOrder, type: "[]string", zero: "") { e, s in try d.string(e, &s) }
                    return
                }
                try d.slice(v, &tokens, type: "[]importers.freeOTPPlusToken", zero: FreeOTPPlusToken()) { e, x in
                    try d.object(e, type: "importers.freeOTPPlusToken",
                                 fields: ["issuerInt", "issuerExt", "issuerAlt", "label", "labelAlt", "type",
                                          "algo", "secret", "digits", "counter", "period"]) { f, v in
                        switch f {
                        case 0: try d.string(v, &x.issuerInt)
                        case 1: try d.string(v, &x.issuerExt)
                        case 2: try d.string(v, &x.issuerAlt)
                        case 3: try d.string(v, &x.label)
                        case 4: try d.string(v, &x.labelAlt)
                        case 5: try d.string(v, &x.type)
                        case 6: try d.string(v, &x.algo)
                        case 7: try d.slice(v, &x.secret, type: "[]int", zero: 0) { e, n in try d.int(e, &n) }
                        case 8: try d.int(v, &x.digits)
                        case 9: try d.int64(v, &x.counter)
                        default: try d.int(v, &x.period)
                        }
                    }
                }
            }
        }
        var out: [Account] = []
        out.reserveCapacity(tokens.count)
        for tk in tokens.items {
            var issuer = tk.issuerExt
            if issuer.isEmpty { issuer = tk.issuerInt }
            if !tk.issuerAlt.isEmpty { issuer = tk.issuerAlt }
            var label = tk.label
            if !tk.labelAlt.isEmpty { label = tk.labelAlt }
            var typ = tk.type
            // FreeOTP+ has no Steam token type: a Steam account is stored as a
            // TOTP token whose issuer is "Steam" and whose codes come out of a
            // different alphabet.
            if GoText.equalFold(issuer, ascii: "Steam") && GoText.equalFold(typ, ascii: "TOTP") {
                typ = "steam"
            }
            out.append(try wrapping("freeotp+ token \(GoText.quote(issuer)): ") {
                let secret = try javaSignedBytes(Array(tk.secret.items))
                var counter = tk.counter
                // FreeOTP+ persists counter-1 and adds one back when it rebuilds
                // the URI, so the stored value is one step behind the real counter.
                if GoText.equalFold(tk.type, ascii: "HOTP") { counter &+= 1 }
                return try buildAccountRaw(type: typ, issuer: issuer, acct: label, secret: secret,
                                           algo: tk.algo, digits: tk.digits, period: tk.period, counter: counter)
            })
        }
        return out
    }

    /// Converts Gson's byte[] rendering — a JSON array of Java signed bytes,
    /// -128..127 — into raw key bytes.
    static func javaSignedBytes(_ vals: [Int]) throws -> Data {
        if vals.isEmpty { throw ImporterError.malformed(.missingSecret, "missing secret") }
        if vals.contains(where: { $0 < -128 || $0 > 255 }) {
            throw ImporterError.malformed(.invalidSecret, "secret byte out of range")
        }
        return Data(vals.map { UInt8(truncatingIfNeeded: $0) })
    }
}
