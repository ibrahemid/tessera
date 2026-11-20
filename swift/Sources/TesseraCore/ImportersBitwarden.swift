import Foundation

extension Importers {

    struct BitwardenLogin {
        var username = ""
        var totp = ""
    }

    struct BitwardenItem {
        var name = ""
        var notes = ""
        var login: BitwardenLogin?
    }

    private static let bitwardenLoginType = #"struct { Username string "json:\"username\""; TOTP string "json:\"totp\"" }"#

    /// Walks the item list shared by the Bitwarden password manager and
    /// Bitwarden Authenticator exports. Items with no login object and items
    /// whose totp field is empty carry no second factor and are skipped.
    static func parseBitwardenItems(_ doc: GoJSON) throws -> [Account] {
        var items = GoSlice<BitwardenItem>()
        try wrapping("bitwarden: ") {
            let d = GoJSONDecoder(doc, root: "bitwardenFile")
            try d.object(doc.root, type: "importers.bitwardenFile", fields: ["items"]) { _, v in
                try d.slice(v, &items, type: "[]importers.bitwardenItem", zero: BitwardenItem()) { e, x in
                    try d.object(e, type: "importers.bitwardenItem", fields: ["name", "notes", "login"]) { f, v in
                        switch f {
                        case 0: try d.string(v, &x.name)
                        case 1: try d.string(v, &x.notes)
                        default:
                            try d.pointer(v, &x.login, zero: BitwardenLogin()) { p, login in
                                try d.object(p, type: bitwardenLoginType, fields: ["username", "totp"]) { f, v in
                                    if f == 0 { try d.string(v, &login.username) } else { try d.string(v, &login.totp) }
                                }
                            }
                        }
                    }
                }
            }
        }
        var out: [Account] = []
        out.reserveCapacity(items.count)
        for it in items.items {
            guard let login = it.login else { continue }
            let parsed: Account?
            do { parsed = try parseOTPValue(login.totp, it.name, login.username) }
            catch { throw ImporterError.malformed("bitwarden item \(GoText.quote(it.name)): \(message(error))") }
            guard var a = parsed else { continue }
            a.note = TextClean.note(it.notes)
            out.append(a)
        }
        return out
    }
}
