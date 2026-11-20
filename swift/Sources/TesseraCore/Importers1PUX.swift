import Foundation

extension Importers {

    struct OnePUXLoginField {
        var value = ""
        var designation = ""
    }

    struct OnePUXField {
        /// The raw `value` node, nil when the member is absent.
        var value: Int?
    }

    struct OnePUXSection {
        var fields = GoSlice<OnePUXField>()
    }

    struct OnePUXItem {
        var notesPlain = ""
        var loginFields = GoSlice<OnePUXLoginField>()
        var sections = GoSlice<OnePUXSection>()
        var title = ""
        var tags: Int?
        var favIndex: Int?
    }

    struct OnePUXVault {
        var attrs: Int?
        var items = GoSlice<OnePUXItem>()
    }

    struct OnePUXAccount {
        var vaults = GoSlice<OnePUXVault>()
    }

    private enum OnePUXType {
        static let items = #"[]importers.onePUXItem"#
        static let vault = #"struct { Attrs jsontext.Value "json:\"attrs\""; Items []importers.onePUXItem "json:\"items\"" }"#
        static let vaults = "[]" + vault
        static let account = #"struct { Vaults []struct { Attrs jsontext.Value "json:\"attrs\""; Items []importers.onePUXItem "json:\"items\"" } "json:\"vaults\"" }"#
        static let accounts = "[]" + account
        static let loginField = #"struct { Value string "json:\"value\""; Designation string "json:\"designation\"" }"#
        static let field = #"struct { Value jsontext.Value "json:\"value\"" }"#
        static let section = #"struct { Fields []struct { Value jsontext.Value "json:\"value\"" } "json:\"fields\"" }"#
        static let details = #"struct { NotesPlain string "json:\"notesPlain\""; LoginFields []struct { Value string "json:\"value\""; Designation string "json:\"designation\"" } "json:\"loginFields\""; Sections []struct { Fields []struct { Value jsontext.Value "json:\"value\"" } "json:\"fields\"" } "json:\"sections\"" }"#
        static let overview = #"struct { Title string "json:\"title\""; Tags jsontext.Value "json:\"tags\"" }"#
    }

    private static func decodeOnePUXItem(_ d: GoJSONDecoder, _ node: Int, _ item: inout OnePUXItem) throws {
        try d.object(node, type: "importers.onePUXItem", fields: ["details", "overview", "favIndex"]) { f, v in
            if f == 2 {
                item.favIndex = v
                return
            }
            if f == 1 {
                try d.object(v, type: OnePUXType.overview, fields: ["title", "tags"]) { f, v in
                    if f == 0 { try d.string(v, &item.title) } else { item.tags = v }
                }
                return
            }
            try d.object(v, type: OnePUXType.details, fields: ["notesPlain", "loginFields", "sections"]) { f, v in
                switch f {
                case 0:
                    try d.string(v, &item.notesPlain)
                case 1:
                    try d.slice(v, &item.loginFields, type: "[]" + OnePUXType.loginField, zero: OnePUXLoginField()) { e, lf in
                        try d.object(e, type: OnePUXType.loginField, fields: ["value", "designation"]) { f, v in
                            if f == 0 { try d.string(v, &lf.value) } else { try d.string(v, &lf.designation) }
                        }
                    }
                default:
                    try d.slice(v, &item.sections, type: "[]" + OnePUXType.section, zero: OnePUXSection()) { e, s in
                        try d.object(e, type: OnePUXType.section, fields: ["fields"]) { _, v in
                            try d.slice(v, &s.fields, type: "[]" + OnePUXType.field, zero: OnePUXField()) { e, fl in
                                try d.object(e, type: OnePUXType.field, fields: ["value"]) { _, v in fl.value = v }
                            }
                        }
                    }
                }
            }
        }
    }

    /// The `totp` member of a field value decoded as `struct { TOTP string }`,
    /// or nil where that decode fails.
    private static func onePUXTOTP(_ doc: GoJSON, _ node: Int) -> String? {
        var totp = ""
        let d = GoJSONDecoder(doc, root: "")
        do {
            try d.object(node, type: "", fields: ["totp"]) { _, v in try d.string(v, &totp) }
        } catch {
            return nil
        }
        return totp
    }

    /// Reads the export.data document at the heart of a 1Password 1PUX archive.
    static func parse1PUXData(_ doc: GoJSON) throws -> [Account] {
        var accounts = GoSlice<OnePUXAccount>()
        try wrapping("1password: ") {
            let d = GoJSONDecoder(doc, root: "onePUXFile")
            try d.object(doc.root, type: "importers.onePUXFile", fields: ["accounts"]) { _, v in
                try d.slice(v, &accounts, type: OnePUXType.accounts, zero: OnePUXAccount()) { e, acct in
                    try d.object(e, type: OnePUXType.account, fields: ["vaults"]) { _, v in
                        try d.slice(v, &acct.vaults, type: OnePUXType.vaults, zero: OnePUXVault()) { e, vault in
                            try d.object(e, type: OnePUXType.vault, fields: ["attrs", "items"]) { f, v in
                                if f == 0 {
                                    vault.attrs = v
                                    return
                                }
                                try d.slice(v, &vault.items, type: OnePUXType.items, zero: OnePUXItem()) { e, item in
                                    try decodeOnePUXItem(d, e, &item)
                                }
                            }
                        }
                    }
                }
            }
        }
        var out: [Account] = []
        for acct in accounts.items {
            for vault in acct.vaults.items {
                let vaultName = rawObject(doc, vault.attrs, fields: ["name"]).flatMap { rawString(doc, $0[0]) } ?? ""
                for item in vault.items.items {
                    let username = item.loginFields.items
                        .first { GoText.equal($0.designation, "username") }?.value ?? ""
                    for section in item.sections.items {
                        for field in section.fields.items {
                            guard let node = field.value, let totp = onePUXTOTP(doc, node) else { continue }
                            let parsed: Account?
                            do { parsed = try parseOTPValue(totp, item.title, username) }
                            catch {
                                throw entryError("1password item \(quoteBounded(item.title)): ", error)
                            }
                            guard var a = parsed else { continue }
                            a.note = TextClean.note(item.notesPlain)
                            a.folder = TextClean.text(vaultName)
                            a.tags = cleanTags(rawStrings(doc, item.tags))
                            a.pinned = rawIsPositive(doc, item.favIndex)
                            out.append(a)
                        }
                    }
                }
            }
        }
        return out
    }
}
