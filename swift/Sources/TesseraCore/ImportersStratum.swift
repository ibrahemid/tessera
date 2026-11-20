import Foundation

extension Importers {

    struct StratumEntry {
        var type = 0
        var issuer = ""
        var username = ""
        var secret = ""
        var algorithm = 0
        var digits = 0
        var period = 0
        var counter: Int64 = 0
    }

    static func parseStratum(_ doc: GoJSON) throws -> [Account] {
        var entries = GoSlice<StratumEntry>()
        try wrapping("stratum: ") {
            let d = GoJSONDecoder(doc, root: "stratumFile")
            try d.object(doc.root, type: "importers.stratumFile", fields: ["Authenticators"]) { _, v in
                try d.slice(v, &entries, type: "[]importers.stratumEntry", zero: StratumEntry()) { e, x in
                    try d.object(e, type: "importers.stratumEntry",
                                 fields: ["Type", "Issuer", "Username", "Secret", "Algorithm", "Digits", "Period", "Counter"]) { f, v in
                        switch f {
                        case 0: try d.int(v, &x.type)
                        case 1: try d.string(v, &x.issuer)
                        case 2: try d.string(v, &x.username)
                        case 3: try d.string(v, &x.secret)
                        case 4: try d.int(v, &x.algorithm)
                        case 5: try d.int(v, &x.digits)
                        case 6: try d.int(v, &x.period)
                        default: try d.int64(v, &x.counter)
                        }
                    }
                }
            }
        }
        var out: [Account] = []
        out.reserveCapacity(entries.count)
        for e in entries.items {
            out.append(try wrapping("stratum entry \(GoText.quote(e.issuer)): ") {
                let typ = try stratumType(e.type)
                let algo = try stratumAlgorithm(e.algorithm)
                return try buildAccount(type: typ, issuer: e.issuer, acct: e.username, secretB32: e.secret,
                                        algo: algo, digits: e.digits, period: e.period, counter: e.counter)
            })
        }
        return out
    }

    private static func stratumType(_ t: Int) throws -> String {
        switch t {
        case 1: return "hotp"
        case 2: return "totp"
        case 3: throw ImporterError.malformed("unsupported account type mOTP")
        case 4: return "steam"
        case 5: throw ImporterError.malformed("unsupported account type Yandex OTP")
        default: throw ImporterError.malformed("unsupported account type \(t)")
        }
    }

    private static func stratumAlgorithm(_ a: Int) throws -> String {
        switch a {
        case 0: return "SHA1"
        case 1: return "SHA256"
        case 2: return "SHA512"
        default: throw ImporterError.malformed("unsupported algorithm \(a)")
        }
    }
}
