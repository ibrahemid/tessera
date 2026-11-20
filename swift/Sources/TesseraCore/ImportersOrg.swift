import Foundation

extension Importers {

    static func rawString(_ doc: GoJSON, _ node: Int?) -> String? {
        guard let node, doc.kind(node) == .string else { return nil }
        return doc.string(node)
    }

    static func rawIsTrue(_ doc: GoJSON, _ node: Int?) -> Bool {
        guard let node else { return false }
        return doc.kind(node) == .trueValue
    }

    static func rawIsPositive(_ doc: GoJSON, _ node: Int?) -> Bool {
        guard let node, doc.kind(node) == .number else { return false }
        let raw = Array(doc.raw(node).utf8)
        guard let first = raw.first, first >= 0x30, first <= 0x39 else { return false }
        let mantissa = raw.prefix { $0 != 0x65 && $0 != 0x45 }
        return mantissa.contains { $0 >= 0x31 && $0 <= 0x39 }
    }

    static func rawElements(_ doc: GoJSON, _ node: Int?) -> [Int] {
        guard let node, doc.kind(node) == .array else { return [] }
        return doc.elements(node)
    }

    static func rawObject(_ doc: GoJSON, _ node: Int?, fields: [String]) -> [Int?]? {
        guard let node, doc.kind(node) == .object else { return nil }
        var values = [Int?](repeating: nil, count: fields.count)
        let d = GoJSONDecoder(doc, root: "")
        try? d.object(node, type: "", fields: fields) { f, v in values[f] = v }
        return values
    }

    static func rawStrings(_ doc: GoJSON, _ node: Int?) -> [String] {
        rawElements(doc, node).compactMap { rawString(doc, $0) }
    }

    static func indexGroups(_ doc: GoJSON, _ node: Int?, idKey: String) -> [[UInt8]: String] {
        var idx: [[UInt8]: String] = [:]
        for e in rawElements(doc, node) {
            guard let g = rawObject(doc, e, fields: [idKey, "name"]),
                  let id = rawString(doc, g[0]),
                  let name = rawString(doc, g[1]) else { continue }
            idx[Array(id.utf8)] = name
        }
        return idx
    }

    static func groupName(_ names: inout [String], _ idx: [[UInt8]: String], _ doc: GoJSON, _ node: Int?) {
        guard let id = rawString(doc, node), let name = idx[Array(id.utf8)] else { return }
        names.append(name)
    }

    static func groupNames(_ names: inout [String], _ idx: [[UInt8]: String], _ doc: GoJSON, _ node: Int?) {
        for e in rawElements(doc, node) { groupName(&names, idx, doc, e) }
    }

    static func cleanTags(_ tags: [String]) -> [String] {
        var out: [String] = []
        var seen = Set<[UInt8]>()
        for t in tags {
            let c = TextClean.text(t)
            if c.isEmpty || !seen.insert(Array(c.utf8)).inserted { continue }
            out.append(c)
        }
        return out
    }

    static func placeGroups(_ names: [String]) -> (folder: String, tags: [String]) {
        let clean = cleanTags(names)
        guard let first = clean.first else { return ("", []) }
        return (first, Array(clean.dropFirst()))
    }
}
