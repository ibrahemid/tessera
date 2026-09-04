import Foundation

extension Importers {

    /// Walks the item list shared by the Bitwarden password manager and
    /// Bitwarden Authenticator exports. Items with no login object and items
    /// whose totp field is empty carry no second factor and are skipped.
    static func parseBitwardenItems(_ items: [Any]) throws -> [Account] {
        var out: [Account] = []
        out.reserveCapacity(items.count)
        for raw in items {
            guard let item = raw as? [String: Any] else { continue }
            guard let login = item["login"] as? [String: Any] else { continue }
            let name = str(item["name"])
            do {
                guard let a = try parseOTPValue(str(login["totp"]), name, str(login["username"])) else {
                    continue
                }
                out.append(a)
            } catch {
                throw ImporterError.malformed("bitwarden item \"\(name)\": \(message(error))")
            }
        }
        return out
    }
}
