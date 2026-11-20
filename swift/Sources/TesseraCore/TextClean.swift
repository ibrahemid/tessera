import Foundation

/// Cleans display text where it enters the model: otpauth labels and issuers,
/// migration names, and importer issuer, account and note fields
/// (spec/vault-format.md § text cleaning). Mirrors Go `account.CleanText` and
/// `account.CleanNote` scalar for scalar.
public enum TextClean {

    /// Removes C0 and C1 controls, DEL, the bidi embedding, override and
    /// isolate controls and U+FEFF, then trims Unicode white space from both
    /// ends. Line breaks and tabs are removed like any other control.
    public static func text(_ s: String) -> String {
        clean(s.unicodeScalars, keepLineBreaks: false)
    }

    /// As `text`, except that CRLF and a lone CR become LF first, and LF and
    /// tab are kept.
    public static func note(_ s: String) -> String {
        var normalized = String.UnicodeScalarView()
        var afterCR = false
        for u in s.unicodeScalars {
            if u == "\r" {
                normalized.append("\n")
                afterCR = true
                continue
            }
            if u == "\n" && afterCR {
                afterCR = false
                continue
            }
            afterCR = false
            normalized.append(u)
        }
        return clean(normalized, keepLineBreaks: true)
    }

    static func isRemoved(_ v: UInt32) -> Bool {
        switch v {
        case 0...0x1F, 0x7F, 0x80...0x9F, 0x202A...0x202E, 0x2066...0x2069, 0xFEFF: return true
        default: return false
        }
    }

    private static func clean(_ scalars: String.UnicodeScalarView, keepLineBreaks: Bool) -> String {
        var out = String.UnicodeScalarView()
        for u in scalars {
            if isRemoved(u.value) && !(keepLineBreaks && (u == "\n" || u == "\t")) { continue }
            out.append(u)
        }
        return GoText.trimSpace(String(out))
    }
}

/// Go's simple per-scalar case mappings, read from the generated `CaseTables`
/// so the result does not depend on the runtime's Unicode version.
enum CaseMap {
    /// The Unicode version the tables were generated from.
    static var unicodeVersion: String { CaseTables.unicodeVersion }

    /// Go `unicode.ToLower`.
    static func lower(_ v: UInt32) -> UInt32 {
        if v < 0x80 { return v >= 0x41 && v <= 0x5A ? v + 0x20 : v }
        return map(v, CaseTables.lower)
    }

    /// Go `unicode.ToUpper`.
    static func upper(_ v: UInt32) -> UInt32 {
        if v < 0x80 { return v >= 0x61 && v <= 0x7A ? v - 0x20 : v }
        return map(v, CaseTables.upper)
    }

    private static func map(_ v: UInt32, _ table: [Int32]) -> UInt32 {
        guard v <= 0x10FFFF else { return v }
        let r = Int32(v)
        var lo = 0, hi = table.count / 4
        while lo < hi {
            let mid = (lo + hi) / 2
            let base = mid * 4
            if r < table[base] {
                hi = mid
            } else if r > table[base + 1] {
                lo = mid + 1
            } else {
                guard (r - table[base]) % table[base + 2] == 0 else { return v }
                return UInt32(r + table[base + 3])
            }
        }
        return v
    }
}

/// String and number helpers with Go's semantics, for the code paths that must
/// accept and reject exactly what the Go core does.
enum GoText {
    static let runeError: UInt32 = 0xFFFD

    /// Go `unicode.IsSpace`.
    static func isSpace(_ v: UInt32) -> Bool {
        switch v {
        case 0x09...0x0D, 0x20, 0x85, 0xA0, 0x1680, 0x2000...0x200A, 0x2028, 0x2029, 0x202F, 0x205F, 0x3000:
            return true
        default:
            return false
        }
    }

    /// Go `strings.TrimSpace`.
    static func trimSpace(_ s: String) -> String {
        let u = s.unicodeScalars
        guard let first = u.firstIndex(where: { !isSpace($0.value) }) else { return "" }
        let last = u.lastIndex(where: { !isSpace($0.value) })!
        return String(u[first...last])
    }

    static func trimSpace(_ s: Substring) -> String { trimSpace(String(s)) }

    /// Go `utf8.DecodeRune`: an invalid or truncated sequence decodes as
    /// U+FFFD with width 1.
    static func decodeRune(_ b: [UInt8], _ i: Int) -> (UInt32, Int) {
        let n = b.count - i
        guard n > 0 else { return (runeError, 0) }
        let b0 = b[i]
        if b0 < 0x80 { return (UInt32(b0), 1) }
        let size: Int, lo: UInt8, hi: UInt8
        switch b0 {
        case 0xC2...0xDF: size = 2; lo = 0x80; hi = 0xBF
        case 0xE0: size = 3; lo = 0xA0; hi = 0xBF
        case 0xE1...0xEC, 0xEE...0xEF: size = 3; lo = 0x80; hi = 0xBF
        case 0xED: size = 3; lo = 0x80; hi = 0x9F
        case 0xF0: size = 4; lo = 0x90; hi = 0xBF
        case 0xF1...0xF3: size = 4; lo = 0x80; hi = 0xBF
        case 0xF4: size = 4; lo = 0x80; hi = 0x8F
        default: return (runeError, 1)
        }
        guard n >= size else { return (runeError, 1) }
        let b1 = b[i + 1]
        guard b1 >= lo && b1 <= hi else { return (runeError, 1) }
        if size == 2 { return (UInt32(b0 & 0x1F) << 6 | UInt32(b1 & 0x3F), 2) }
        let b2 = b[i + 2]
        guard b2 >= 0x80 && b2 <= 0xBF else { return (runeError, 1) }
        if size == 3 {
            return (UInt32(b0 & 0x0F) << 12 | UInt32(b1 & 0x3F) << 6 | UInt32(b2 & 0x3F), 3)
        }
        let b3 = b[i + 3]
        guard b3 >= 0x80 && b3 <= 0xBF else { return (runeError, 1) }
        return (UInt32(b0 & 0x07) << 18 | UInt32(b1 & 0x3F) << 12 | UInt32(b2 & 0x3F) << 6 | UInt32(b3 & 0x3F), 4)
    }

    /// Go `utf8.DecodeLastRune` over `b[..<end]`.
    static func decodeLastRune(_ b: [UInt8], _ end: Int) -> (UInt32, Int) {
        guard end > 0 else { return (runeError, 0) }
        if b[end - 1] < 0x80 { return (UInt32(b[end - 1]), 1) }
        let lim = max(end - 4, 0)
        var start = end - 1
        while start > lim {
            if b[start] & 0xC0 != 0x80 { break }
            start -= 1
        }
        let (r, size) = decodeRune(Array(b[start..<end]), 0)
        return start + size == end ? (r, size) : (runeError, 1)
    }

    /// Converts bytes to a string the way ranging over a Go string sees them:
    /// each byte of an invalid sequence becomes one U+FFFD.
    static func string<S: Sequence>(_ bytes: S) -> String where S.Element == UInt8 {
        let b = Array(bytes)
        var out = String.UnicodeScalarView()
        var i = 0
        while i < b.count {
            let (r, w) = decodeRune(b, i)
            out.append(Unicode.Scalar(r)!)
            i += w
        }
        return String(out)
    }

    /// Go `bytes.TrimSpace`.
    static func trimSpace(bytes b: [UInt8]) -> ArraySlice<UInt8> {
        var start = 0
        while start < b.count {
            let (r, w) = decodeRune(b, start)
            if !isSpace(r) || (r == runeError && w == 1) { break }
            start += w
        }
        var end = b.count
        while end > start {
            let (r, w) = decodeLastRune(b, end)
            if !isSpace(r) || end - w < start { break }
            end -= w
        }
        return b[start..<end]
    }

    /// Go `unicode.ToUpper` on one scalar (the simple mapping).
    static func upper(_ u: Unicode.Scalar) -> Unicode.Scalar {
        Unicode.Scalar(CaseMap.upper(u.value)) ?? u
    }

    /// Go `unicode.ToLower` on one scalar (the simple mapping).
    static func lower(_ u: Unicode.Scalar) -> Unicode.Scalar {
        Unicode.Scalar(CaseMap.lower(u.value)) ?? u
    }

    /// Go `strings.ToUpper`.
    static func toUpper(_ s: String) -> String {
        var out = String.UnicodeScalarView()
        for u in s.unicodeScalars { out.append(upper(u)) }
        return String(out)
    }

    /// Go `strings.ToLower`.
    static func toLower(_ s: String) -> String {
        var out = String.UnicodeScalarView()
        for u in s.unicodeScalars { out.append(lower(u)) }
        return String(out)
    }

    /// Go `strings.EqualFold` where `target` is ASCII. The only non-ASCII
    /// scalars in an ASCII letter's fold orbit are U+017F (s) and U+212A (k).
    static func equalFold(_ s: String, ascii target: String) -> Bool {
        let a = Array(s.unicodeScalars), b = Array(target.unicodeScalars)
        guard a.count == b.count else { return false }
        for (x, y) in zip(a, b) where x != y {
            let fy = upper(y).value
            switch x.value {
            case 0x17F: if fy != 0x53 { return false }
            case 0x212A: if fy != 0x4B { return false }
            default:
                guard x.isASCII, upper(x).value == fy, (0x41...0x5A).contains(fy) else { return false }
            }
        }
        return true
    }

    /// Go `strconv.ParseInt(s, 10, 64)`.
    static func parseInt64(_ s: String) -> Int64? {
        var digits = Substring(s)
        var negative = false
        if let f = digits.first, f == "+" || f == "-" {
            negative = f == "-"
            digits = digits.dropFirst()
        }
        guard !digits.isEmpty else { return nil }
        var magnitude: UInt64 = 0
        for c in digits.unicodeScalars {
            guard c.value >= 0x30 && c.value <= 0x39 else { return nil }
            let (m1, o1) = magnitude.multipliedReportingOverflow(by: 10)
            let (m2, o2) = m1.addingReportingOverflow(UInt64(c.value - 0x30))
            guard !o1 && !o2 else { return nil }
            magnitude = m2
        }
        if negative {
            guard magnitude <= UInt64(Int64.max) + 1 else { return nil }
            return magnitude == UInt64(Int64.max) + 1 ? Int64.min : -Int64(magnitude)
        }
        guard magnitude <= UInt64(Int64.max) else { return nil }
        return Int64(magnitude)
    }

    /// Go `strconv.Atoi` on a 64-bit platform.
    static func atoi(_ s: String) -> Int? {
        parseInt64(s).map { Int($0) }
    }

    static let maxQuotedBytes = 16

    /// Go `otpauth.QuoteValue`: `quote` of the longest whole-rune prefix of at
    /// most `maxQuotedBytes` bytes, then an ellipsis and the full byte length.
    static func quoteValue(bytes b: [UInt8]) -> String {
        guard b.count > maxQuotedBytes else { return quote(bytes: b) }
        var n = 0
        while n < b.count {
            let (_, w) = decodeRune(b, n)
            if n + w > maxQuotedBytes { break }
            n += w
        }
        return quote(bytes: Array(b[..<n])) + "… (\(b.count) bytes)"
    }

    static func quoteValue(_ s: String) -> String { quoteValue(bytes: Array(s.utf8)) }

    /// Go `strconv.Quote`, for error messages.
    static func quote(_ s: String) -> String { quote(bytes: Array(s.utf8)) }

    /// Go `strconv.Quote` over raw bytes: each byte of an invalid sequence is
    /// written as `\xNN`.
    static func quote(bytes b: [UInt8]) -> String {
        var out = "\""
        var i = 0
        while i < b.count {
            let (r, w) = decodeRune(b, i)
            defer { i += w }
            if r == runeError && w == 1 {
                out += String(format: "\\x%02x", b[i])
                continue
            }
            let u = Unicode.Scalar(r)!
            switch u {
            case "\"": out += "\\\""
            case "\\": out += "\\\\"
            case "\u{07}": out += "\\a"
            case "\u{08}": out += "\\b"
            case "\u{0C}": out += "\\f"
            case "\n": out += "\\n"
            case "\r": out += "\\r"
            case "\t": out += "\\t"
            case "\u{0B}": out += "\\v"
            default:
                if isPrint(u) {
                    out.unicodeScalars.append(u)
                } else if u.value < 0x80 {
                    out += String(format: "\\x%02x", u.value)
                } else if u.value < 0x10000 {
                    out += String(format: "\\u%04x", u.value)
                } else {
                    out += String(format: "\\U%08x", u.value)
                }
            }
        }
        return out + "\""
    }

    private static func isPrint(_ u: Unicode.Scalar) -> Bool {
        if u == " " { return true }
        switch u.properties.generalCategory {
        case .uppercaseLetter, .lowercaseLetter, .titlecaseLetter, .modifierLetter, .otherLetter,
             .nonspacingMark, .spacingMark, .enclosingMark,
             .decimalNumber, .letterNumber, .otherNumber,
             .connectorPunctuation, .dashPunctuation, .openPunctuation, .closePunctuation,
             .initialPunctuation, .finalPunctuation, .otherPunctuation,
             .mathSymbol, .currencySymbol, .modifierSymbol, .otherSymbol:
            return true
        default:
            return false
        }
    }
}
