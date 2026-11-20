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
        var groupID: Int?
    }

    private static let twofasOTPType = #"struct { Account string "json:\"account\""; Issuer string "json:\"issuer\""; Digits int "json:\"digits\""; Period int "json:\"period\""; Algorithm string "json:\"algorithm\""; TokenType string "json:\"tokenType\""; Counter int64 "json:\"counter\"" }"#

    static func parse2FAS(_ doc: GoJSON) throws -> [Account] {
        var services = GoSlice<TwofasService>()
        var groupList: Int?
        try wrapping("2fas: ") {
            let d = GoJSONDecoder(doc, root: "twofasFile")
            try d.object(doc.root, type: "importers.twofasFile", fields: ["services", "groups"]) { f, v in
                if f == 1 { groupList = v; return }
                try d.slice(v, &services, type: "[]importers.twofasService", zero: TwofasService()) { e, s in
                    try d.object(e, type: "importers.twofasService", fields: ["name", "secret", "otp", "groupId"]) { f, v in
                        switch f {
                        case 0: try d.string(v, &s.name)
                        case 1: try d.string(v, &s.secret)
                        case 3: s.groupID = v
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
        let groups = indexGroups(doc, groupList, idKey: "id")
        var out: [Account] = []
        out.reserveCapacity(services.count)
        for s in services.items {
            var issuer = s.otp.issuer
            if issuer.isEmpty { issuer = s.name }
            var a: Account
            do {
                a = try buildAccount(
                    type: s.otp.tokenType, issuer: issuer, acct: s.otp.account, secretB32: s.secret,
                    algo: s.otp.algorithm, digits: s.otp.digits, period: s.otp.period, counter: s.otp.counter)
            } catch {
                throw entryError("2fas service \(quoteBounded(s.name)): ", error)
            }
            var names: [String] = []
            groupName(&names, groups, doc, s.groupID)
            (a.folder, a.tags) = placeGroups(names)
            out.append(a)
        }
        return out
    }
}
