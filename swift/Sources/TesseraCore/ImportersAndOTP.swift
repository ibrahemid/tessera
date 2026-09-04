import Foundation

extension Importers {

    static func parseAndOTP(_ entries: [[String: Any]]) throws -> [Account] {
        var out: [Account] = []
        out.reserveCapacity(entries.count)
        for e in entries {
            let issuer = str(e["issuer"])
            do {
                out.append(try buildAccount(
                    type: str(e["type"]),
                    issuer: issuer,
                    acct: str(e["label"]),
                    secretB32: str(e["secret"]),
                    algo: str(e["algorithm"]),
                    digits: intVal(e["digits"]),
                    period: intVal(e["period"]),
                    counter: int64Val(e["counter"])))
            } catch {
                throw ImporterError.malformed("andotp entry \"\(issuer)\": \(message(error))")
            }
        }
        return out
    }
}
