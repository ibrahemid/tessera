import Foundation

extension Importers {

    struct ProtonContent {
        var uri = ""
        var entryType = ""
        var name = ""
    }

    struct ProtonEntry {
        var content = ProtonContent()
        var note = ""
    }

    private static let protonContentType = #"struct { URI string "json:\"uri\""; EntryType string "json:\"entry_type\""; Name string "json:\"name\"" }"#

    static func parseProton(_ doc: GoJSON) throws -> [Account] {
        var entries = GoSlice<ProtonEntry>()
        try wrapping("proton: ") {
            let d = GoJSONDecoder(doc, root: "protonFile")
            try d.object(doc.root, type: "importers.protonFile", fields: ["entries"]) { _, v in
                try d.slice(v, &entries, type: "[]importers.protonEntry", zero: ProtonEntry()) { e, x in
                    try d.object(e, type: "importers.protonEntry", fields: ["content", "note"]) { f, v in
                        if f == 1 { try d.string(v, &x.note); return }
                        try d.object(v, type: protonContentType, fields: ["uri", "entry_type", "name"]) { f, v in
                            switch f {
                            case 0: try d.string(v, &x.content.uri)
                            case 1: try d.string(v, &x.content.entryType)
                            default: try d.string(v, &x.content.name)
                            }
                        }
                    }
                }
            }
        }
        var out: [Account] = []
        out.reserveCapacity(entries.count)
        for e in entries.items {
            // Every parameter lives in the uri; entry_type only sanity-checks that
            // an entry marked Steam really carries a Steam secret.
            let name = GoText.quote(e.content.name)
            let parsed: Account?
            do { parsed = try parseOTPValue(e.content.uri, "", e.content.name) }
            catch { throw entryError("proton entry \(name): ", error) }
            guard var a = parsed else { continue }
            if GoText.equalFold(e.content.entryType, ascii: "Steam") && a.type != .steam {
                throw ImporterError.malformed(.invalidSecret, "proton entry \(name): marked Steam but its uri is not a Steam secret")
            }
            a.note = TextClean.note(e.note)
            out.append(a)
        }
        return out
    }
}
