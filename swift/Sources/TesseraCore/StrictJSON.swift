import Foundation

enum JSONFailure: Error, Equatable, Sendable {
    case syntax
    case duplicateKey
    case nonCanonicalNumber
    case tooDeep
}

/// How the strict reader treats a number token: payload documents accept only
/// canonical int64 text, envelope documents any number that fits a finite double.
enum JSONNumberRule: Sendable {
    case canonicalInt64
    case finiteDouble
}

struct JSONMember: Sendable {
    let key: [UInt8]
    let value: JSONNode
}

struct JSONNode: Sendable {
    enum Kind: Sendable { case object, array, string, number, literal }
    let kind: Kind
    let range: Range<Int>
    var members: [JSONMember] = []
    var items: [JSONNode] = []
    var string: [UInt8] = []

    func member(_ key: String) -> JSONNode? {
        let k = Array(key.utf8)
        return members.first { $0.key == k }?.value
    }
}

/// A strict JSON reader that mirrors the Go core's `json.Decoder` token walk:
/// document order, duplicate keys compared after unescaping, the nesting limit
/// checked when a container opens, and only end of input after the root.
enum StrictJSON {
    static let maxNesting = 64

    static func parse(_ bytes: [UInt8], rule: JSONNumberRule, level: Int = 1) throws -> JSONNode {
        var p = Parser(b: bytes, rule: rule)
        p.skipSpace()
        let node = try p.value(level: level)
        p.skipSpace()
        if p.pos != bytes.count { throw JSONFailure.syntax }
        return node
    }

    static func isCanonicalInt(_ text: some Collection<UInt8>) -> Bool {
        let t = Array(text)
        if t == [0x30] { return true }
        var body = t[...]
        if body.first == 0x2D { body = body.dropFirst() }
        guard let first = body.first, first >= 0x31, first <= 0x39 else { return false }
        for c in body.dropFirst() where c < 0x30 || c > 0x39 { return false }
        return Int64(String(decoding: t, as: UTF8.self)) != nil
    }

    // MARK: Encoding

    private static let hexDigits: [UInt8] = Array("0123456789abcdef".utf8)

    /// Appends `src` as a JSON string exactly as Go's encoder does with HTML
    /// escaping off.
    static func appendQuoted(_ src: some Collection<UInt8>, to dst: inout [UInt8]) {
        let s = Array(src)
        dst.append(0x22)
        var i = 0
        while i < s.count {
            let c = s[i]
            if c < 0x80 {
                switch c {
                case 0x22, 0x5C: dst.append(0x5C); dst.append(c)
                case 0x08: dst.append(0x5C); dst.append(0x62)
                case 0x0C: dst.append(0x5C); dst.append(0x66)
                case 0x0A: dst.append(0x5C); dst.append(0x6E)
                case 0x0D: dst.append(0x5C); dst.append(0x72)
                case 0x09: dst.append(0x5C); dst.append(0x74)
                case 0x00..<0x20:
                    dst.append(contentsOf: [0x5C, 0x75, 0x30, 0x30, hexDigits[Int(c >> 4)], hexDigits[Int(c & 0xF)]])
                default: dst.append(c)
                }
                i += 1
                continue
            }
            let (r, size) = GoUTF8.decodeRune(s, at: i)
            if r == GoUTF8.runeError && size == 1 {
                dst.append(contentsOf: Array("\\ufffd".utf8))
            } else if r == 0x2028 || r == 0x2029 {
                dst.append(contentsOf: [0x5C, 0x75, 0x32, 0x30, 0x32, hexDigits[Int(r & 0xF)]])
            } else {
                dst.append(contentsOf: s[i..<(i + size)])
            }
            i += size
        }
        dst.append(0x22)
    }

    static func quoted(_ s: String) -> [UInt8] {
        var out: [UInt8] = []
        appendQuoted(s.utf8, to: &out)
        return out
    }

    /// Re-encodes a parsed value canonically: object keys sorted by bytes,
    /// strings re-escaped, numbers and literals verbatim, no whitespace.
    static func appendCanonical(_ node: JSONNode, source: [UInt8], to out: inout [UInt8]) {
        switch node.kind {
        case .object:
            out.append(0x7B)
            let sorted = node.members.sorted { bytesLess($0.key, $1.key) }
            for (i, m) in sorted.enumerated() {
                if i > 0 { out.append(0x2C) }
                appendQuoted(m.key, to: &out)
                out.append(0x3A)
                appendCanonical(m.value, source: source, to: &out)
            }
            out.append(0x7D)
        case .array:
            out.append(0x5B)
            for (i, item) in node.items.enumerated() {
                if i > 0 { out.append(0x2C) }
                appendCanonical(item, source: source, to: &out)
            }
            out.append(0x5D)
        case .string:
            appendQuoted(node.string, to: &out)
        case .number, .literal:
            out.append(contentsOf: source[node.range])
        }
    }

    /// Removes insignificant whitespace and keeps string contents verbatim, as
    /// Go's `json.Compact` does.
    static func compact(_ src: some Collection<UInt8>) -> [UInt8] {
        var out: [UInt8] = []
        out.reserveCapacity(src.count)
        var inString = false
        var escaped = false
        for c in src {
            if inString {
                out.append(c)
                if escaped { escaped = false } else if c == 0x5C { escaped = true } else if c == 0x22 { inString = false }
                continue
            }
            switch c {
            case 0x20, 0x09, 0x0A, 0x0D: continue
            case 0x22: inString = true; out.append(c)
            default: out.append(c)
            }
        }
        return out
    }

    /// Indents compact JSON with two spaces per level, as Go's `json.Indent`
    /// does with an empty prefix.
    static func indent(_ src: [UInt8]) -> [UInt8] {
        var out: [UInt8] = []
        out.reserveCapacity(src.count * 2)
        var needIndent = false
        var depth = 0
        var inString = false
        var escaped = false
        func newline() {
            out.append(0x0A)
            for _ in 0..<depth { out.append(0x20); out.append(0x20) }
        }
        for c in src {
            if inString {
                out.append(c)
                if escaped { escaped = false } else if c == 0x5C { escaped = true } else if c == 0x22 { inString = false }
                continue
            }
            if c == 0x20 || c == 0x09 || c == 0x0A || c == 0x0D { continue }
            if needIndent && c != 0x7D && c != 0x5D {
                needIndent = false
                depth += 1
                newline()
            }
            switch c {
            case 0x7B, 0x5B:
                needIndent = true
                out.append(c)
            case 0x2C:
                out.append(c)
                newline()
            case 0x3A:
                out.append(c)
                out.append(0x20)
            case 0x7D, 0x5D:
                if needIndent {
                    needIndent = false
                } else {
                    depth -= 1
                    newline()
                }
                out.append(c)
            case 0x22:
                inString = true
                out.append(c)
            default:
                out.append(c)
            }
        }
        return out
    }

    static func bytesLess(_ a: [UInt8], _ b: [UInt8]) -> Bool {
        a.lexicographicallyPrecedes(b)
    }

    static func utf8Less(_ a: String, _ b: String) -> Bool {
        a.utf8.lexicographicallyPrecedes(b.utf8)
    }

    // MARK: Parser

    private struct Parser {
        let b: [UInt8]
        let rule: JSONNumberRule
        var pos = 0

        init(b: [UInt8], rule: JSONNumberRule) {
            self.b = b
            self.rule = rule
        }

        mutating func skipSpace() {
            while pos < b.count {
                switch b[pos] {
                case 0x20, 0x09, 0x0A, 0x0D: pos += 1
                default: return
                }
            }
        }

        mutating func value(level: Int) throws -> JSONNode {
            guard pos < b.count else { throw JSONFailure.syntax }
            switch b[pos] {
            case 0x7B: return try object(level: level)
            case 0x5B: return try array(level: level)
            case 0x22:
                let start = pos
                let s = try string()
                return JSONNode(kind: .string, range: start..<pos, string: s)
            case 0x2D, 0x30...0x39: return try number()
            case 0x74: return try literal(Array("true".utf8))
            case 0x66: return try literal(Array("false".utf8))
            case 0x6E: return try literal(Array("null".utf8))
            default: throw JSONFailure.syntax
            }
        }

        mutating func object(level: Int) throws -> JSONNode {
            let start = pos
            pos += 1
            if level > StrictJSON.maxNesting { throw JSONFailure.tooDeep }
            var members: [JSONMember] = []
            var seen = Set<[UInt8]>()
            skipSpace()
            if pos < b.count && b[pos] == 0x7D {
                pos += 1
                return JSONNode(kind: .object, range: start..<pos)
            }
            while true {
                skipSpace()
                guard pos < b.count, b[pos] == 0x22 else { throw JSONFailure.syntax }
                let key = try string()
                if !seen.insert(key).inserted { throw JSONFailure.duplicateKey }
                skipSpace()
                guard pos < b.count, b[pos] == 0x3A else { throw JSONFailure.syntax }
                pos += 1
                skipSpace()
                let v = try value(level: level + 1)
                members.append(JSONMember(key: key, value: v))
                skipSpace()
                guard pos < b.count else { throw JSONFailure.syntax }
                if b[pos] == 0x2C { pos += 1; continue }
                if b[pos] == 0x7D { pos += 1; break }
                throw JSONFailure.syntax
            }
            return JSONNode(kind: .object, range: start..<pos, members: members)
        }

        mutating func array(level: Int) throws -> JSONNode {
            let start = pos
            pos += 1
            if level > StrictJSON.maxNesting { throw JSONFailure.tooDeep }
            var items: [JSONNode] = []
            skipSpace()
            if pos < b.count && b[pos] == 0x5D {
                pos += 1
                return JSONNode(kind: .array, range: start..<pos)
            }
            while true {
                skipSpace()
                items.append(try value(level: level + 1))
                skipSpace()
                guard pos < b.count else { throw JSONFailure.syntax }
                if b[pos] == 0x2C { pos += 1; continue }
                if b[pos] == 0x5D { pos += 1; break }
                throw JSONFailure.syntax
            }
            return JSONNode(kind: .array, range: start..<pos, items: items)
        }

        mutating func literal(_ word: [UInt8]) throws -> JSONNode {
            let start = pos
            guard pos + word.count <= b.count, Array(b[pos..<(pos + word.count)]) == word else {
                throw JSONFailure.syntax
            }
            pos += word.count
            return JSONNode(kind: .literal, range: start..<pos)
        }

        private func isDigit(_ i: Int) -> Bool {
            i < b.count && b[i] >= 0x30 && b[i] <= 0x39
        }

        mutating func number() throws -> JSONNode {
            let start = pos
            if b[pos] == 0x2D { pos += 1 }
            guard pos < b.count else { throw JSONFailure.syntax }
            if b[pos] == 0x30 {
                pos += 1
            } else if b[pos] >= 0x31 && b[pos] <= 0x39 {
                while isDigit(pos) { pos += 1 }
            } else {
                throw JSONFailure.syntax
            }
            if pos < b.count && b[pos] == 0x2E {
                pos += 1
                guard isDigit(pos) else { throw JSONFailure.syntax }
                while isDigit(pos) { pos += 1 }
            }
            if pos < b.count && (b[pos] == 0x65 || b[pos] == 0x45) {
                pos += 1
                if pos < b.count && (b[pos] == 0x2B || b[pos] == 0x2D) { pos += 1 }
                guard isDigit(pos) else { throw JSONFailure.syntax }
                while isDigit(pos) { pos += 1 }
            }
            let text = b[start..<pos]
            switch rule {
            case .canonicalInt64:
                guard StrictJSON.isCanonicalInt(text) else { throw JSONFailure.nonCanonicalNumber }
            case .finiteDouble:
                guard let d = Double(String(decoding: text, as: UTF8.self)), d.isFinite else {
                    throw JSONFailure.syntax
                }
            }
            return JSONNode(kind: .number, range: start..<pos)
        }

        private func hex4(at i: Int) -> UInt32? {
            guard i + 4 <= b.count else { return nil }
            var v: UInt32 = 0
            for c in b[i..<(i + 4)] {
                let d: UInt32
                switch c {
                case 0x30...0x39: d = UInt32(c - 0x30)
                case 0x61...0x66: d = UInt32(c - 0x61 + 10)
                case 0x41...0x46: d = UInt32(c - 0x41 + 10)
                default: return nil
                }
                v = v << 4 | d
            }
            return v
        }

        mutating func string() throws -> [UInt8] {
            pos += 1
            var out: [UInt8] = []
            while true {
                guard pos < b.count else { throw JSONFailure.syntax }
                let c = b[pos]
                if c == 0x22 {
                    pos += 1
                    return out
                }
                if c < 0x20 { throw JSONFailure.syntax }
                if c == 0x5C {
                    guard pos + 1 < b.count else { throw JSONFailure.syntax }
                    switch b[pos + 1] {
                    case 0x22, 0x5C, 0x2F: out.append(b[pos + 1])
                    case 0x62: out.append(0x08)
                    case 0x66: out.append(0x0C)
                    case 0x6E: out.append(0x0A)
                    case 0x72: out.append(0x0D)
                    case 0x74: out.append(0x09)
                    case 0x75:
                        guard let r = hex4(at: pos + 2) else { throw JSONFailure.syntax }
                        pos += 6
                        var scalar = r
                        if r >= 0xD800 && r < 0xE000 {
                            scalar = GoUTF8.runeError
                            if r < 0xDC00, pos + 6 <= b.count, b[pos] == 0x5C, b[pos + 1] == 0x75,
                               let r2 = hex4(at: pos + 2), r2 >= 0xDC00, r2 < 0xE000 {
                                scalar = ((r - 0xD800) << 10 | (r2 - 0xDC00)) + 0x10000
                                pos += 6
                            }
                        }
                        GoUTF8.appendRune(scalar, to: &out)
                        continue
                    default:
                        throw JSONFailure.syntax
                    }
                    pos += 2
                    continue
                }
                if c < 0x80 {
                    out.append(c)
                    pos += 1
                    continue
                }
                let (r, size) = GoUTF8.decodeRune(b, at: pos)
                if r == GoUTF8.runeError && size == 1 {
                    GoUTF8.appendRune(GoUTF8.runeError, to: &out)
                } else {
                    out.append(contentsOf: b[pos..<(pos + size)])
                }
                pos += size
            }
        }
    }
}

/// UTF-8 decoding with Go's `utf8.DecodeRune` rules: each byte of an invalid
/// sequence becomes one U+FFFD.
enum GoUTF8 {
    static let runeError: UInt32 = 0xFFFD

    static func decodeRune(_ s: [UInt8], at i: Int) -> (UInt32, Int) {
        let n = s.count - i
        guard n > 0 else { return (runeError, 0) }
        let b0 = s[i]
        if b0 < 0x80 { return (UInt32(b0), 1) }
        let size: Int
        var lo: UInt8 = 0x80
        var hi: UInt8 = 0xBF
        switch b0 {
        case 0xC2...0xDF: size = 2
        case 0xE0: size = 3; lo = 0xA0
        case 0xE1...0xEC, 0xEE...0xEF: size = 3
        case 0xED: size = 3; hi = 0x9F
        case 0xF0: size = 4; lo = 0x90
        case 0xF1...0xF3: size = 4
        case 0xF4: size = 4; hi = 0x8F
        default: return (runeError, 1)
        }
        if n < size { return (runeError, 1) }
        let b1 = s[i + 1]
        if b1 < lo || b1 > hi { return (runeError, 1) }
        if size == 2 {
            return (UInt32(b0 & 0x1F) << 6 | UInt32(b1 & 0x3F), 2)
        }
        let b2 = s[i + 2]
        if b2 < 0x80 || b2 > 0xBF { return (runeError, 1) }
        if size == 3 {
            return (UInt32(b0 & 0x0F) << 12 | UInt32(b1 & 0x3F) << 6 | UInt32(b2 & 0x3F), 3)
        }
        let b3 = s[i + 3]
        if b3 < 0x80 || b3 > 0xBF { return (runeError, 1) }
        return (UInt32(b0 & 0x07) << 18 | UInt32(b1 & 0x3F) << 12 | UInt32(b2 & 0x3F) << 6 | UInt32(b3 & 0x3F), 4)
    }

    static func appendRune(_ r: UInt32, to out: inout [UInt8]) {
        let v = (r > 0x10FFFF || (r >= 0xD800 && r < 0xE000)) ? runeError : r
        switch v {
        case 0..<0x80:
            out.append(UInt8(v))
        case 0x80..<0x800:
            out.append(UInt8(0xC0 | (v >> 6)))
            out.append(UInt8(0x80 | (v & 0x3F)))
        case 0x800..<0x10000:
            out.append(UInt8(0xE0 | (v >> 12)))
            out.append(UInt8(0x80 | ((v >> 6) & 0x3F)))
            out.append(UInt8(0x80 | (v & 0x3F)))
        default:
            out.append(UInt8(0xF0 | (v >> 18)))
            out.append(UInt8(0x80 | ((v >> 12) & 0x3F)))
            out.append(UInt8(0x80 | ((v >> 6) & 0x3F)))
            out.append(UInt8(0x80 | (v & 0x3F)))
        }
    }

    /// Converts bytes to a String the way Go's `string([]byte)` iteration sees
    /// them: invalid sequences become U+FFFD.
    static func string(_ bytes: [UInt8]) -> String {
        var out: [UInt8] = []
        out.reserveCapacity(bytes.count)
        var i = 0
        while i < bytes.count {
            let (r, size) = decodeRune(bytes, at: i)
            if r == runeError && size == 1 {
                appendRune(runeError, to: &out)
            } else {
                out.append(contentsOf: bytes[i..<(i + size)])
            }
            i += size
        }
        return String(decoding: out, as: UTF8.self)
    }
}

/// Standard base64 with padding, matching Go's `base64.StdEncoding`.
enum GoBase64 {
    private static let alphabet: [UInt8] = Array("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/".utf8)
    private static let decodeMap: [UInt8] = {
        var m = [UInt8](repeating: 0xFF, count: 256)
        for (i, c) in alphabet.enumerated() { m[Int(c)] = UInt8(i) }
        return m
    }()

    static func encode(_ data: some Collection<UInt8>) -> [UInt8] {
        Array(Data(data).base64EncodedString().utf8)
    }

    /// Decodes `s` only when it is exactly the padded standard encoding of the
    /// result: no line breaks, no base64url, no missing padding, zero trailing bits.
    static func decodeCanonical(_ s: some Collection<UInt8>) -> [UInt8]? {
        let src = Array(s)
        guard let decoded = try? decodeLenient(src) else { return nil }
        return encode(decoded) == src ? decoded : nil
    }

    static func decodeCanonical(_ s: String) -> Data? {
        decodeCanonical(s.utf8).map { Data($0) }
    }

    struct CorruptInput: Error, CustomStringConvertible {
        let offset: Int
        var description: String { "illegal base64 data at input byte \(offset)" }
    }

    /// Go's non-strict `StdEncoding.DecodeString`: line breaks are skipped,
    /// padding is required, trailing bits are ignored.
    static func decodeLenient(_ src: [UInt8]) throws -> [UInt8] {
        var out: [UInt8] = []
        out.reserveCapacity(src.count / 4 * 3)
        var si = 0
        while true {
            var dbuf = [UInt8](repeating: 0, count: 4)
            var dlen = 4
            var j = 0
            var trailing: CorruptInput?
            quantum: while j < 4 {
                if si == src.count {
                    if j == 0 { return out }
                    throw CorruptInput(offset: si - j)
                }
                let c = src[si]
                si += 1
                let v = decodeMap[Int(c)]
                if v != 0xFF {
                    dbuf[j] = v
                    j += 1
                    continue
                }
                if c == 0x0A || c == 0x0D { continue }
                if c != 0x3D { throw CorruptInput(offset: si - 1) }
                switch j {
                case 0, 1:
                    throw CorruptInput(offset: si - 1)
                case 2:
                    while si < src.count && (src[si] == 0x0A || src[si] == 0x0D) { si += 1 }
                    if si == src.count { throw CorruptInput(offset: src.count) }
                    if src[si] != 0x3D { throw CorruptInput(offset: si - 1) }
                    si += 1
                default:
                    break
                }
                while si < src.count && (src[si] == 0x0A || src[si] == 0x0D) { si += 1 }
                if si < src.count { trailing = CorruptInput(offset: si) }
                dlen = j
                break quantum
            }
            let val = UInt32(dbuf[0]) << 18 | UInt32(dbuf[1]) << 12 | UInt32(dbuf[2]) << 6 | UInt32(dbuf[3])
            let bytes = [UInt8(truncatingIfNeeded: val >> 16), UInt8(truncatingIfNeeded: val >> 8), UInt8(truncatingIfNeeded: val)]
            out.append(contentsOf: bytes.prefix(dlen - 1))
            if let t = trailing { throw t }
            if dlen < 4 { return out }
        }
    }
}
