import Foundation

extension Importers {

    static func parseStratum(_ probe: [String: Any]) throws -> [Account] {
        let entries = probe["Authenticators"] as? [[String: Any]] ?? []
        var out: [Account] = []
        out.reserveCapacity(entries.count)
        for e in entries {
            let issuer = str(e["Issuer"])
            do {
                let type = try stratumType(intVal(e["Type"]))
                let algo = try stratumAlgorithm(intVal(e["Algorithm"]))
                out.append(try buildAccount(
                    type: type,
                    issuer: issuer,
                    acct: str(e["Username"]),
                    secretB32: str(e["Secret"]),
                    algo: algo,
                    digits: intVal(e["Digits"]),
                    period: intVal(e["Period"]),
                    counter: int64Val(e["Counter"])))
            } catch {
                throw ImporterError.malformed("stratum entry \"\(issuer)\": \(message(error))")
            }
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
