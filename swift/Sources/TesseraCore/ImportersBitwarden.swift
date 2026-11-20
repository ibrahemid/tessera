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
        var folderID: Int?
        var collectionIDs: Int?
        var favorite: Int?
    }

    private static let bitwardenLoginType = #"struct { Username string "json:\"username\""; TOTP string "json:\"totp\"" }"#

    /// Walks the item list shared by the Bitwarden password manager and
    /// Bitwarden Authenticator exports. Items with no login object and items
    /// whose totp field is empty carry no second factor and are skipped.
    static func parseBitwardenItems(_ doc: GoJSON) throws -> [Account] {
        var items = GoSlice<BitwardenItem>()
        var folderList: Int?
        var collectionList: Int?
        try wrapping("bitwarden: ") {
            let d = GoJSONDecoder(doc, root: "bitwardenFile")
            try d.object(doc.root, type: "importers.bitwardenFile", fields: ["items", "folders", "collections"]) { f, v in
                if f == 1 { folderList = v; return }
                if f == 2 { collectionList = v; return }
                try d.slice(v, &items, type: "[]importers.bitwardenItem", zero: BitwardenItem()) { e, x in
                    try d.object(e, type: "importers.bitwardenItem",
                                 fields: ["name", "notes", "login", "folderId", "collectionIds", "favorite"]) { f, v in
                        switch f {
                        case 0: try d.string(v, &x.name)
                        case 1: try d.string(v, &x.notes)
                        case 3: x.folderID = v
                        case 4: x.collectionIDs = v
                        case 5: x.favorite = v
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
        let folders = indexGroups(doc, folderList, idKey: "id")
        let collections = indexGroups(doc, collectionList, idKey: "id")
        var out: [Account] = []
        out.reserveCapacity(items.count)
        for it in items.items {
            guard let login = it.login else { continue }
            let parsed: Account?
            do { parsed = try parseOTPValue(login.totp, it.name, login.username) }
            catch { throw entryError("bitwarden item \(quoteBounded(it.name)): ", error) }
            guard var a = parsed else { continue }
            a.note = TextClean.note(it.notes)
            var names: [String] = []
            groupName(&names, folders, doc, it.folderID)
            groupNames(&names, collections, doc, it.collectionIDs)
            (a.folder, a.tags) = placeGroups(names)
            a.pinned = rawIsTrue(doc, it.favorite)
            out.append(a)
        }
        return out
    }
}
