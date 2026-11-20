import Foundation

extension Importers {

    struct AegisInfo {
        var secret = ""
        var algo = ""
        var digits = 0
        var period = 0
        var counter: Int64 = 0
    }

    struct AegisEntry {
        var type = ""
        var name = ""
        var issuer = ""
        var note = ""
        var info = AegisInfo()
        var groups: Int?
        var group: Int?
        var favorite: Int?
    }

    private static let aegisInfoType = #"struct { Secret string "json:\"secret\""; Algo string "json:\"algo\""; Digits int "json:\"digits\""; Period int "json:\"period\""; Counter int64 "json:\"counter\"" }"#

    static func parseAegis(_ doc: GoJSON) throws -> [Account] {
        var version = 0
        var db: Int?
        try wrapping("aegis: ") {
            let d = GoJSONDecoder(doc, root: "aegisFile")
            try d.object(doc.root, type: "importers.aegisFile", fields: ["version", "db"]) { f, v in
                if f == 0 { try d.int(v, &version) } else { db = v }
            }
        }
        guard let dbNode = db else {
            throw ImporterError.malformed(.invalidJSON, "aegis db: unexpected end of JSON input")
        }
        // In an encrypted export "db" is a base64 string, not an object.
        if doc.kind(dbNode) == .string {
            throw ImporterError.encrypted(.aegis, "Aegis export is encrypted; export with encryption off (Aegis: Settings > Import/Export > Export, untick encryption) and try again")
        }
        var entries = GoSlice<AegisEntry>()
        var groupList: Int?
        try wrapping("aegis db: ") {
            let d = GoJSONDecoder(doc, root: "aegisDB")
            try d.object(dbNode, type: "importers.aegisDB", fields: ["entries", "groups"]) { f, v in
                if f == 1 { groupList = v; return }
                try d.slice(v, &entries, type: "[]importers.aegisEntry", zero: AegisEntry()) { e, x in
                    try d.object(e, type: "importers.aegisEntry",
                                 fields: ["type", "name", "issuer", "note", "info", "groups", "group", "favorite"]) { f, v in
                        switch f {
                        case 0: try d.string(v, &x.type)
                        case 1: try d.string(v, &x.name)
                        case 2: try d.string(v, &x.issuer)
                        case 3: try d.string(v, &x.note)
                        case 5: x.groups = v
                        case 6: x.group = v
                        case 7: x.favorite = v
                        default:
                            try d.object(v, type: aegisInfoType,
                                         fields: ["secret", "algo", "digits", "period", "counter"]) { f, v in
                                switch f {
                                case 0: try d.string(v, &x.info.secret)
                                case 1: try d.string(v, &x.info.algo)
                                case 2: try d.int(v, &x.info.digits)
                                case 3: try d.int(v, &x.info.period)
                                default: try d.int64(v, &x.info.counter)
                                }
                            }
                        }
                    }
                }
            }
        }
        let groups = indexGroups(doc, groupList, idKey: "uuid")
        var out: [Account] = []
        out.reserveCapacity(entries.count)
        for e in entries.items {
            var a: Account
            do {
                a = try buildAccount(
                    type: e.type, issuer: e.issuer, acct: e.name, secretB32: e.info.secret,
                    algo: e.info.algo, digits: e.info.digits, period: e.info.period, counter: e.info.counter)
            } catch {
                throw entryError("aegis entry \(quoteBounded(e.name)): ", error)
            }
            a.note = TextClean.note(e.note)
            var names: [String] = []
            groupNames(&names, groups, doc, e.groups)
            if let legacy = rawString(doc, e.group) { names.append(legacy) }
            (a.folder, a.tags) = placeGroups(names)
            a.pinned = rawIsTrue(doc, e.favorite)
            out.append(a)
        }
        return out
    }
}
