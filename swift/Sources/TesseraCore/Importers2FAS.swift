import Foundation

extension Importers {

    static func parse2FAS(_ probe: [String: Any]) throws -> [Account] {
        let services = probe["services"] as? [[String: Any]] ?? []
        var out: [Account] = []
        out.reserveCapacity(services.count)
        for s in services {
            let name = str(s["name"])
            let otp = s["otp"] as? [String: Any] ?? [:]
            var issuer = str(otp["issuer"])
            if issuer.isEmpty { issuer = name }
            do {
                out.append(try buildAccount(
                    type: str(otp["tokenType"]),
                    issuer: issuer,
                    acct: str(otp["account"]),
                    secretB32: str(s["secret"]),
                    algo: str(otp["algorithm"]),
                    digits: intVal(otp["digits"]),
                    period: intVal(otp["period"]),
                    counter: int64Val(otp["counter"])))
            } catch {
                throw ImporterError.malformed("2fas service \"\(name)\": \(message(error))")
            }
        }
        return out
    }
}
