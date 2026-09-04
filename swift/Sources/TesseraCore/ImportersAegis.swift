import Foundation

extension Importers {

    static func parseAegis(_ probe: [String: Any]) throws -> [Account] {
        // In an encrypted export "db" is a base64 string, not an object.
        if probe["db"] is String {
            throw ImporterError.encrypted("Aegis export is encrypted; export with encryption off (Aegis: Settings > Import/Export > Export, untick encryption) and try again")
        }
        guard let db = probe["db"] as? [String: Any] else {
            throw ImporterError.malformed("aegis db: not an object")
        }
        let entries = db["entries"] as? [[String: Any]] ?? []
        var out: [Account] = []
        out.reserveCapacity(entries.count)
        for e in entries {
            let name = str(e["name"])
            let info = e["info"] as? [String: Any] ?? [:]
            do {
                out.append(try buildAccount(
                    type: str(e["type"]),
                    issuer: str(e["issuer"]),
                    acct: name,
                    secretB32: str(info["secret"]),
                    algo: str(info["algo"]),
                    digits: intVal(info["digits"]),
                    period: intVal(info["period"]),
                    counter: int64Val(info["counter"])))
            } catch {
                throw ImporterError.malformed("aegis entry \"\(name)\": \(message(error))")
            }
        }
        return out
    }
}
