import Foundation

extension Importers {

    static func parseRaivo(_ entries: [[String: Any]]) throws -> [Account] {
        var out: [Account] = []
        out.reserveCapacity(entries.count)
        for e in entries {
            let issuer = str(e["issuer"])
            // Raivo stores numbers as strings.
            let secret = str(e["secret"])
            if secret.isEmpty {
                throw ImporterError.malformed("raivo entry \"\(issuer)\": missing secret")
            }
            let digits: Int, period: Int, counter: Int
            do { digits = try atoiDefault(str(e["digits"]), 6) }
            catch { throw ImporterError.malformed("raivo entry \"\(issuer)\": digits: \(message(error))") }
            do { period = try atoiDefault(str(e["timer"]), 30) }
            catch { throw ImporterError.malformed("raivo entry \"\(issuer)\": timer: \(message(error))") }
            do { counter = try atoiDefault(str(e["counter"]), 0) }
            catch { throw ImporterError.malformed("raivo entry \"\(issuer)\": counter: \(message(error))") }
            do {
                out.append(try buildAccount(
                    type: str(e["kind"]),
                    issuer: issuer,
                    acct: str(e["account"]),
                    secretB32: secret,
                    algo: str(e["algorithm"]),
                    digits: digits,
                    period: period,
                    counter: Int64(counter)))
            } catch {
                throw ImporterError.malformed("raivo entry \"\(issuer)\": \(message(error))")
            }
        }
        return out
    }
}
