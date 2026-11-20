import Foundation

extension Importers {

    struct TwofasOTP {
        var account = ""
        var issuer = ""
        var digits = 0
        var period = 0
        var algorithm = ""
        var tokenType = ""
        var counter: Int64 = 0
    }

    struct TwofasService {
        var name = ""
        var secret = ""
        var otp = TwofasOTP()
    }

    private static let twofasOTPType = #"struct { Account string "json:\"account\""; Issuer string "json:\"issuer\""; Digits int "json:\"digits\""; Period int "json:\"period\""; Algorithm string "json:\"algorithm\""; TokenType string "json:\"tokenType\""; Counter int64 "json:\"counter\"" }"#

    static func parse2FAS(_ doc: GoJSON) throws -> [Account] {
        var services = GoSlice<TwofasService>()
        try wrapping("2fas: ") {
            let d = GoJSONDecoder(doc, root: "twofasFile")
            try d.object(doc.root, type: "importers.twofasFile", fields: ["services"]) { _, v in
                try d.slice(v, &services, type: "[]importers.twofasService", zero: TwofasService()) { e, s in
                    try d.object(e, type: "importers.twofasService", fields: ["name", "secret", "otp"]) { f, v in
                        switch f {
                        case 0: try d.string(v, &s.name)
                        case 1: try d.string(v, &s.secret)
                        default:
                            try d.object(v, type: twofasOTPType,
                                         fields: ["account", "issuer", "digits", "period", "algorithm", "tokenType", "counter"]) { f, v in
                                switch f {
                                case 0: try d.string(v, &s.otp.account)
                                case 1: try d.string(v, &s.otp.issuer)
                                case 2: try d.int(v, &s.otp.digits)
                                case 3: try d.int(v, &s.otp.period)
                                case 4: try d.string(v, &s.otp.algorithm)
                                case 5: try d.string(v, &s.otp.tokenType)
                                default: try d.int64(v, &s.otp.counter)
                                }
                            }
                        }
                    }
                }
            }
        }
        var out: [Account] = []
        out.reserveCapacity(services.count)
        for s in services.items {
            var issuer = s.otp.issuer
            if issuer.isEmpty { issuer = s.name }
            do {
                out.append(try buildAccount(
                    type: s.otp.tokenType, issuer: issuer, acct: s.otp.account, secretB32: s.secret,
                    algo: s.otp.algorithm, digits: s.otp.digits, period: s.otp.period, counter: s.otp.counter))
            } catch {
                throw ImporterError.malformed("2fas service \(GoText.quote(s.name)): \(message(error))")
            }
        }
        return out
    }
}
