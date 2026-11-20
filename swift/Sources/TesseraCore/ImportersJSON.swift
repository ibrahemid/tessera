import Foundation

/// A JSON document read the way the Go importers read one with
/// `encoding/json`: RFC 8259 syntax checked in full before any value is used,
/// at most 10000 nested containers, duplicate names allowed, invalid UTF-8
/// inside strings accepted and decoded as U+FFFD per byte. The values are kept
/// as a flat pre-order list of nodes, so neither parsing nor walking recurses
/// on the input's nesting.
struct GoJSON {
    enum Kind { case null, falseValue, trueValue, number, string, array, object }

    struct Node {
        let kind: Kind
        let start: Int
        var end: Int
        var after: Int
    }

    static let maxDepth = 10000

    let bytes: [UInt8]
    private(set) var nodes: [Node] = []

    /// Parses `bytes` as exactly one JSON value surrounded by optional
    /// whitespace. Returns nil on any syntax error or on nesting deeper than
    /// `maxDepth`.
    init?(_ bytes: [UInt8]) {
        self.bytes = bytes
        guard parse() else { return nil }
    }

    var root: Int { 0 }

    private static func isSpace(_ c: UInt8) -> Bool {
        c == 0x20 || c == 0x09 || c == 0x0A || c == 0x0D
    }

    private func skipSpace(_ i: Int) -> Int {
        var j = i
        while j < bytes.count, Self.isSpace(bytes[j]) { j += 1 }
        return j
    }

    private func hex4(_ i: Int) -> UInt32? {
        guard i + 4 <= bytes.count else { return nil }
        var v: UInt32 = 0
        for k in i..<(i + 4) {
            let c = bytes[k]
            switch c {
            case 0x30...0x39: v = v << 4 | UInt32(c - 0x30)
            case 0x41...0x46: v = v << 4 | UInt32(c - 0x41 + 10)
            case 0x61...0x66: v = v << 4 | UInt32(c - 0x61 + 10)
            default: return nil
            }
        }
        return v
    }

    private func scanString(_ i: Int) -> Int? {
        var j = i + 1
        while j < bytes.count {
            let c = bytes[j]
            if c == 0x22 { return j + 1 }
            if c == 0x5C {
                guard j + 1 < bytes.count else { return nil }
                switch bytes[j + 1] {
                case 0x22, 0x5C, 0x2F, 0x62, 0x66, 0x6E, 0x72, 0x74:
                    j += 2
                case 0x75:
                    guard hex4(j + 2) != nil else { return nil }
                    j += 6
                default:
                    return nil
                }
                continue
            }
            if c < 0x20 { return nil }
            j += 1
        }
        return nil
    }

    private func isDigit(_ j: Int) -> Bool {
        j < bytes.count && bytes[j] >= 0x30 && bytes[j] <= 0x39
    }

    private func scanNumber(_ i: Int) -> Int? {
        var j = i
        if bytes[j] == 0x2D { j += 1 }
        guard j < bytes.count else { return nil }
        if bytes[j] == 0x30 {
            j += 1
        } else if isDigit(j) {
            while isDigit(j) { j += 1 }
        } else {
            return nil
        }
        if j < bytes.count, bytes[j] == 0x2E {
            j += 1
            guard isDigit(j) else { return nil }
            while isDigit(j) { j += 1 }
        }
        if j < bytes.count, bytes[j] == 0x65 || bytes[j] == 0x45 {
            j += 1
            if j < bytes.count, bytes[j] == 0x2B || bytes[j] == 0x2D { j += 1 }
            guard isDigit(j) else { return nil }
            while isDigit(j) { j += 1 }
        }
        return j
    }

    private func scanLiteral(_ i: Int, _ word: StaticString) -> Int? {
        let n = word.utf8CodeUnitCount
        guard i + n <= bytes.count else { return nil }
        let p = word.utf8Start
        for k in 0..<n where bytes[i + k] != p[k] { return nil }
        return i + n
    }

    private mutating func appendLeaf(_ kind: Kind, _ start: Int, _ end: Int) {
        nodes.append(Node(kind: kind, start: start, end: end, after: nodes.count + 1))
    }

    private mutating func parseKey(_ i: inout Int) -> Bool {
        i = skipSpace(i)
        guard i < bytes.count, bytes[i] == 0x22, let end = scanString(i) else { return false }
        appendLeaf(.string, i, end)
        i = skipSpace(end)
        guard i < bytes.count, bytes[i] == 0x3A else { return false }
        i += 1
        return true
    }

    private mutating func parse() -> Bool {
        var stack: [Int] = []
        var i = 0
        var expectingValue = true
        while true {
            if expectingValue {
                i = skipSpace(i)
                guard i < bytes.count else { return false }
                let c = bytes[i]
                switch c {
                case 0x7B, 0x5B:
                    guard stack.count < Self.maxDepth else { return false }
                    let isObject = c == 0x7B
                    nodes.append(Node(kind: isObject ? .object : .array, start: i, end: i, after: 0))
                    stack.append(nodes.count - 1)
                    i = skipSpace(i + 1)
                    guard i < bytes.count else { return false }
                    if bytes[i] == (isObject ? 0x7D : 0x5D) {
                        i += 1
                        let top = stack.removeLast()
                        nodes[top].end = i
                        nodes[top].after = nodes.count
                        expectingValue = false
                        continue
                    }
                    if isObject, !parseKey(&i) { return false }
                    continue
                case 0x22:
                    guard let end = scanString(i) else { return false }
                    appendLeaf(.string, i, end)
                    i = end
                case 0x74:
                    guard let end = scanLiteral(i, "true") else { return false }
                    appendLeaf(.trueValue, i, end)
                    i = end
                case 0x66:
                    guard let end = scanLiteral(i, "false") else { return false }
                    appendLeaf(.falseValue, i, end)
                    i = end
                case 0x6E:
                    guard let end = scanLiteral(i, "null") else { return false }
                    appendLeaf(.null, i, end)
                    i = end
                case 0x2D, 0x30...0x39:
                    guard let end = scanNumber(i) else { return false }
                    appendLeaf(.number, i, end)
                    i = end
                default:
                    return false
                }
                expectingValue = false
            } else {
                i = skipSpace(i)
                guard let top = stack.last else { return i == bytes.count }
                guard i < bytes.count else { return false }
                let isObject = nodes[top].kind == .object
                if bytes[i] == 0x2C {
                    i += 1
                    if isObject, !parseKey(&i) { return false }
                    expectingValue = true
                } else if bytes[i] == (isObject ? 0x7D : 0x5D) {
                    i += 1
                    stack.removeLast()
                    nodes[top].end = i
                    nodes[top].after = nodes.count
                } else {
                    return false
                }
            }
        }
    }

    // MARK: - reading values

    func kind(_ i: Int) -> Kind { nodes[i].kind }

    /// The node indexes of an array's elements.
    func elements(_ i: Int) -> [Int] {
        var out: [Int] = []
        var j = i + 1
        while j < nodes[i].after {
            out.append(j)
            j = nodes[j].after
        }
        return out
    }

    /// An object's members as (name node, value node) pairs in input order.
    func members(_ i: Int) -> [(name: Int, value: Int)] {
        var out: [(Int, Int)] = []
        var j = i + 1
        while j < nodes[i].after {
            out.append((j, j + 1))
            j = nodes[j + 1].after
        }
        return out
    }

    /// The decoded bytes of a string node: escapes resolved, an unpaired
    /// surrogate escape and each byte of an invalid UTF-8 sequence replaced
    /// by U+FFFD. The result is always valid UTF-8.
    func unquotedBytes(_ i: Int) -> [UInt8] {
        let node = nodes[i]
        let end = node.end - 1
        var out: [UInt8] = []
        out.reserveCapacity(end - node.start)
        var j = node.start + 1
        while j < end {
            let c = bytes[j]
            if c == 0x5C {
                let e = bytes[j + 1]
                switch e {
                case 0x22, 0x5C, 0x2F: out.append(e); j += 2
                case 0x62: out.append(0x08); j += 2
                case 0x66: out.append(0x0C); j += 2
                case 0x6E: out.append(0x0A); j += 2
                case 0x72: out.append(0x0D); j += 2
                case 0x74: out.append(0x09); j += 2
                default:
                    let v1 = hex4(j + 2) ?? 0
                    j += 6
                    var r = v1
                    if (0xD800...0xDFFF).contains(v1) {
                        r = GoText.runeError
                        if j + 6 <= node.end, bytes[j] == 0x5C, bytes[j + 1] == 0x75,
                           let v2 = hex4(j + 2),
                           (0xD800..<0xDC00).contains(v1), (0xDC00..<0xE000).contains(v2) {
                            r = ((v1 - 0xD800) << 10 | (v2 - 0xDC00)) + 0x10000
                            j += 6
                        }
                    }
                    out.append(contentsOf: Array(String(Unicode.Scalar(r)!).utf8))
                }
            } else if c < 0x80 {
                out.append(c)
                j += 1
            } else {
                let (r, w) = GoText.decodeRune(bytes, j)
                if r == GoText.runeError && w == 1 {
                    out.append(contentsOf: [0xEF, 0xBF, 0xBD])
                } else {
                    out.append(contentsOf: bytes[j..<(j + w)])
                }
                j += w
            }
        }
        return out
    }

    func string(_ i: Int) -> String {
        String(decoding: unquotedBytes(i), as: UTF8.self)
    }

    func raw(_ i: Int) -> String {
        String(decoding: bytes[nodes[i].start..<nodes[i].end], as: UTF8.self)
    }

    /// A `map[string]json.RawMessage` view of an object: names to value
    /// nodes, the last duplicate winning.
    func probe(_ i: Int) -> [[UInt8]: Int] {
        var out: [[UInt8]: Int] = [:]
        for m in members(i) { out[unquotedBytes(m.name)] = m.value }
        return out
    }

    /// Decodes node `i` as `[]map[string]json.RawMessage`. Returns nil when Go
    /// would fail; a null element yields an empty map.
    func arrayOfProbes(_ i: Int) -> [[[UInt8]: Int]]? {
        switch kind(i) {
        case .null: return []
        case .array:
            var out: [[[UInt8]: Int]] = []
            for e in elements(i) {
                switch kind(e) {
                case .null: out.append([:])
                case .object: out.append(probe(e))
                default: return nil
                }
            }
            return out
        default:
            return nil
        }
    }
}

/// A Go slice as `encoding/json` fills it: a later duplicate member decodes
/// into the elements a former one left in the backing array.
struct GoSlice<T> {
    var backing: [T] = []
    var count = 0
    var items: ArraySlice<T> { backing[..<count] }
}

/// A Go `encoding/json` type error, with Go's message text.
struct GoJSONTypeError: Error {
    let message: String
}

/// Decodes `GoJSON` nodes into Swift values with the semantics of Go's
/// `json.Unmarshal` into the importers' structs: member names match a field
/// exactly or else case-insensitively, unknown members are skipped, null
/// leaves a value unchanged, and the first type mismatch in input order is
/// reported with the message Go gives.
final class GoJSONDecoder {
    let doc: GoJSON
    private let rootName: String
    private var path: [String] = []

    init(_ doc: GoJSON, root: String) {
        self.doc = doc
        self.rootName = root
    }

    private func kindName(_ i: Int) -> String {
        switch doc.kind(i) {
        case .null: return "null"
        case .falseValue, .trueValue: return "bool"
        case .number: return "number"
        case .string: return "string"
        case .array: return "array"
        case .object: return "object"
        }
    }

    private func typeError(_ value: String, _ type: String) -> GoJSONTypeError {
        guard let last = path.last else {
            return GoJSONTypeError(message: "json: cannot unmarshal \(value) into Go value of type \(type)")
        }
        let isIndex = !last.isEmpty && last.utf8.allSatisfy { $0 >= 0x30 && $0 <= 0x39 }
        let into = isIndex ? "" : "Go struct field "
        let field = path.joined(separator: ".")
        return GoJSONTypeError(message: "json: cannot unmarshal \(value) into \(into)\(rootName).\(field) of type \(type)")
    }

    func string(_ i: Int, _ out: inout String) throws {
        switch doc.kind(i) {
        case .null: return
        case .string: out = doc.string(i)
        default: throw typeError(kindName(i), "string")
        }
    }

    private func integer(_ i: Int, type: String) throws -> Int64? {
        switch doc.kind(i) {
        case .null:
            return nil
        case .number:
            let raw = doc.raw(i)
            guard let v = GoText.parseInt64(raw) else { throw typeError("number \(raw)", type) }
            return v
        default:
            throw typeError(kindName(i), type)
        }
    }

    func int(_ i: Int, _ out: inout Int) throws {
        if let v = try integer(i, type: "int") { out = Int(v) }
    }

    func int64(_ i: Int, _ out: inout Int64) throws {
        if let v = try integer(i, type: "int64") { out = v }
    }

    /// Decodes an object into a struct whose JSON names are `fields`, calling
    /// `body(fieldIndex, valueNode)` for each member that matches one.
    func object(_ i: Int, type: String, fields: [String], _ body: (Int, Int) throws -> Void) throws {
        switch doc.kind(i) {
        case .null:
            return
        case .object:
            let exact = fields.map { Array($0.utf8) }
            for m in doc.members(i) {
                let name = doc.unquotedBytes(m.name)
                let key = String(decoding: name, as: UTF8.self)
                guard let f = exact.firstIndex(of: name)
                        ?? fields.firstIndex(where: { GoText.equalFold(key, ascii: $0) }) else { continue }
                path.append(key)
                try body(f, m.value)
                path.removeLast()
            }
        default:
            throw typeError(kindName(i), type)
        }
    }

    func slice<T>(_ i: Int, _ s: inout GoSlice<T>, type: String, zero: T,
                  _ element: (Int, inout T) throws -> Void) throws {
        switch doc.kind(i) {
        case .null:
            s = GoSlice()
        case .array:
            var k = 0
            for e in doc.elements(i) {
                if k == s.backing.count { s.backing.append(zero) }
                path.append(String(k))
                try element(e, &s.backing[k])
                path.removeLast()
                k += 1
            }
            if k == 0 { s = GoSlice() } else { s.count = k }
        default:
            throw typeError(kindName(i), type)
        }
    }

    func pointer<T>(_ i: Int, _ p: inout T?, zero: T, _ pointee: (Int, inout T) throws -> Void) throws {
        if doc.kind(i) == .null {
            p = nil
            return
        }
        var v = p ?? zero
        defer { p = v }
        try pointee(i, &v)
    }
}
