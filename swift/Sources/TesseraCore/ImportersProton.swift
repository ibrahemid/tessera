import Foundation

extension Importers {

    static func parseProton(_ entries: [Any]) throws -> [Account] {
        var out: [Account] = []
        out.reserveCapacity(entries.count)
        for raw in entries {
            let entry = raw as? [String: Any] ?? [:]
            let content = entry["content"] as? [String: Any] ?? [:]
            let name = str(content["name"])
            let account: Account?
            do {
                // Every parameter lives in the uri; entry_type only sanity-checks
                // that an entry marked Steam really carries a Steam secret.
                account = try parseOTPValue(str(content["uri"]), "", name)
            } catch {
                throw ImporterError.malformed("proton entry \"\(name)\": \(message(error))")
            }
            guard let a = account else { continue }
            if str(content["entry_type"]).caseInsensitiveCompare("Steam") == .orderedSame,
               a.type != .steam {
                throw ImporterError.malformed("proton entry \"\(name)\": marked Steam but its uri is not a Steam secret")
            }
            out.append(a)
        }
        return out
    }
}
