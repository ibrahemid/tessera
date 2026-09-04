import Foundation

extension Importers {

    /// Reads the export.data document at the heart of a 1Password 1PUX archive.
    /// Both cores parse this document; only the CLI opens the zip.
    static func parse1PUXData(_ accounts: [[String: Any]]) throws -> [Account] {
        var out: [Account] = []
        for acct in accounts {
            let vaults = acct["vaults"] as? [[String: Any]] ?? []
            for vault in vaults {
                let items = vault["items"] as? [[String: Any]] ?? []
                for item in items {
                    let details = item["details"] as? [String: Any] ?? [:]
                    let overview = item["overview"] as? [String: Any] ?? [:]
                    let title = str(overview["title"])
                    var username = ""
                    for lf in details["loginFields"] as? [[String: Any]] ?? []
                    where str(lf["designation"]) == "username" {
                        username = str(lf["value"])
                        break
                    }
                    for section in details["sections"] as? [[String: Any]] ?? [] {
                        for field in section["fields"] as? [[String: Any]] ?? [] {
                            guard let value = field["value"] as? [String: Any] else { continue }
                            let account: Account?
                            do { account = try parseOTPValue(str(value["totp"]), title, username) }
                            catch { throw ImporterError.malformed("1password item \"\(title)\": \(message(error))") }
                            guard let a = account else { continue }
                            out.append(a)
                        }
                    }
                }
            }
        }
        return out
    }
}
