import Foundation

/// The parts of Go's `net/url` that otpauth and otpauth-migration parsing
/// depend on, ported byte for byte: `Parse`, `unescape` and `escape` per
/// encoding mode, and `EscapedPath`. Foundation's URL types accept and
/// normalise a different set of inputs, so the link parsers use this instead.
struct GoURL: Equatable {
    struct ParseError: Error, Equatable, CustomStringConvertible {
        let description: String
    }

    enum Mode: Equatable {
        case path, pathSegment, host, zone, userPassword, queryComponent, fragment
    }

    var scheme = ""
    var opaque: [UInt8] = []
    var host: [UInt8] = []
    var path: [UInt8] = []
    var rawPath: [UInt8] = []
    var rawQuery: [UInt8] = []
    var forceQuery = false
    var fragment: [UInt8] = []

    /// Go `url.Parse`.
    static func parse(_ raw: String) throws -> GoURL {
        let bytes = Array(raw.utf8)
        let (u, frag, _) = cut(bytes, UInt8(ascii: "#"))
        var url = try parseReference(u)
        if !frag.isEmpty { url.fragment = try unescape(frag, .fragment) }
        return url
    }

    /// Go `(*URL).EscapedPath`.
    var escapedPath: [UInt8] {
        if !rawPath.isEmpty && GoURL.validEncoded(rawPath, .path),
           let p = try? GoURL.unescape(rawPath, .path), p == path {
            return rawPath
        }
        if path == [UInt8(ascii: "*")] { return path }
        return GoURL.escape(path, .path)
    }

    // MARK: - Escaping

    static func shouldEscape(_ c: UInt8, _ mode: Mode) -> Bool {
        if isAlnum(c) { return false }
        let s = Unicode.Scalar(c)
        if mode == .host || mode == .zone {
            switch s {
            case "!", "$", "&", "'", "(", ")", "*", "+", ",", ";", "=", ":", "[", "]", "<", ">", "\"":
                return false
            default: break
            }
        }
        switch s {
        case "-", "_", ".", "~":
            return false
        case "$", "&", "+", ",", "/", ":", ";", "=", "?", "@":
            switch mode {
            case .path: return s == "?"
            case .pathSegment: return s == "/" || s == ";" || s == "," || s == "?"
            case .userPassword: return s == "@" || s == "/" || s == "?" || s == ":"
            case .queryComponent: return true
            case .fragment: return false
            case .host, .zone: break
            }
        default: break
        }
        if mode == .fragment {
            switch s {
            case "!", "(", ")", "*": return false
            default: break
            }
        }
        return true
    }

    /// Go `url.unescape`.
    static func unescape(_ s: [UInt8], _ mode: Mode) throws -> [UInt8] {
        var n = 0
        var hasPlus = false
        var i = 0
        while i < s.count {
            switch s[i] {
            case UInt8(ascii: "%"):
                n += 1
                if i + 2 >= s.count || !isHex(s[i + 1]) || !isHex(s[i + 2]) {
                    throw escapeError(Array(s[i..<min(i + 3, s.count)]))
                }
                let triple = Array(s[i..<i + 3])
                let isPercent25 = triple == Array("%25".utf8)
                if mode == .host && unhex(s[i + 1]) < 8 && !isPercent25 {
                    throw escapeError(triple)
                }
                if mode == .zone {
                    let v = unhex(s[i + 1]) << 4 | unhex(s[i + 2])
                    if !isPercent25 && v != UInt8(ascii: " ") && shouldEscape(v, .host) {
                        throw escapeError(triple)
                    }
                }
                i += 3
            case UInt8(ascii: "+"):
                hasPlus = mode == .queryComponent
                i += 1
            default:
                if (mode == .host || mode == .zone) && s[i] < 0x80 && shouldEscape(s[i], mode) {
                    throw ParseError(description: "invalid character "
                        + GoText.quote(GoText.string([s[i]])) + " in host name")
                }
                i += 1
            }
        }
        if n == 0 && !hasPlus { return s }
        let plus: UInt8 = mode == .queryComponent ? UInt8(ascii: " ") : UInt8(ascii: "+")
        var t: [UInt8] = []
        t.reserveCapacity(s.count - 2 * n)
        i = 0
        while i < s.count {
            switch s[i] {
            case UInt8(ascii: "%"):
                t.append(unhex(s[i + 1]) << 4 | unhex(s[i + 2]))
                i += 2
            case UInt8(ascii: "+"):
                t.append(plus)
            default:
                t.append(s[i])
            }
            i += 1
        }
        return t
    }

    /// Go `url.escape`.
    static func escape(_ s: [UInt8], _ mode: Mode) -> [UInt8] {
        let upperhex = Array("0123456789ABCDEF".utf8)
        var t: [UInt8] = []
        t.reserveCapacity(s.count)
        for c in s {
            if c == UInt8(ascii: " ") && mode == .queryComponent {
                t.append(UInt8(ascii: "+"))
            } else if shouldEscape(c, mode) {
                t.append(UInt8(ascii: "%"))
                t.append(upperhex[Int(c >> 4)])
                t.append(upperhex[Int(c & 15)])
            } else {
                t.append(c)
            }
        }
        return t
    }

    static func escape(_ s: String, _ mode: Mode) -> String {
        String(decoding: escape(Array(s.utf8), mode), as: UTF8.self)
    }

    // MARK: - Parsing

    private static func parseReference(_ raw: [UInt8]) throws -> GoURL {
        if raw.contains(where: { $0 < 0x20 || $0 == 0x7F }) {
            throw ParseError(description: "net/url: invalid control character in URL")
        }
        var url = GoURL()
        if raw == [UInt8(ascii: "*")] {
            url.path = raw
            return url
        }
        let (scheme, afterScheme) = try getScheme(raw)
        url.scheme = String(decoding: scheme.map(asciiLower), as: UTF8.self)
        var rest = afterScheme

        let q = UInt8(ascii: "?")
        if rest.last == q && rest.filter({ $0 == q }).count == 1 {
            url.forceQuery = true
            rest.removeLast()
        } else {
            let (before, after, _) = cut(rest, q)
            rest = before
            url.rawQuery = after
        }

        let slash = UInt8(ascii: "/")
        if rest.first != slash {
            if !url.scheme.isEmpty {
                url.opaque = rest
                return url
            }
            let (segment, _, _) = cut(rest, slash)
            if segment.contains(UInt8(ascii: ":")) {
                throw ParseError(description: "first path segment in URL cannot contain colon")
            }
        }

        if (!url.scheme.isEmpty || !hasPrefix(rest, "///")) && hasPrefix(rest, "//") {
            var authority = Array(rest[2...])
            rest = []
            if let i = authority.firstIndex(of: slash) {
                rest = Array(authority[i...])
                authority = Array(authority[..<i])
            }
            url.host = try parseAuthority(authority, scheme: url.scheme)
        }
        url.path = try unescape(rest, .path)
        url.rawPath = escape(url.path, .path) == rest ? [] : rest
        return url
    }

    private static func getScheme(_ raw: [UInt8]) throws -> ([UInt8], [UInt8]) {
        for (i, c) in raw.enumerated() {
            if isAlpha(c) { continue }
            if isDigit(c) || c == UInt8(ascii: "+") || c == UInt8(ascii: "-") || c == UInt8(ascii: ".") {
                if i == 0 { return ([], raw) }
                continue
            }
            if c == UInt8(ascii: ":") {
                if i == 0 { throw ParseError(description: "missing protocol scheme") }
                return (Array(raw[..<i]), Array(raw[(i + 1)...]))
            }
            return ([], raw)
        }
        return ([], raw)
    }

    private static func parseAuthority(_ authority: [UInt8], scheme: String) throws -> [UInt8] {
        guard let at = authority.lastIndex(of: UInt8(ascii: "@")) else {
            return try parseHost(authority, scheme: scheme)
        }
        let host = try parseHost(Array(authority[(at + 1)...]), scheme: scheme)
        let userinfo = Array(authority[..<at])
        guard userinfo.allSatisfy(isUserinfoByte) else {
            throw ParseError(description: "net/url: invalid userinfo")
        }
        let (user, password, hasPassword) = cut(userinfo, UInt8(ascii: ":"))
        _ = try unescape(user, .userPassword)
        if hasPassword { _ = try unescape(password, .userPassword) }
        return host
    }

    private static func parseHost(_ host: [UInt8], scheme: String) throws -> [UInt8] {
        if let open = host.lastIndex(of: UInt8(ascii: "[")) {
            guard open == 0 else { throw ParseError(description: "invalid IP-literal") }
            guard let close = host.lastIndex(of: UInt8(ascii: "]")) else {
                throw ParseError(description: "missing ']' in host")
            }
            let colonPort = Array(host[(close + 1)...])
            guard isValidOptionalPort(colonPort) else {
                throw ParseError(description: "invalid port " + GoText.quote(GoText.string(colonPort)) + " after host")
            }
            let port = try unescape(colonPort, .host)
            let hostname = close > open ? Array(host[(open + 1)..<close]) : []
            let unescaped: [UInt8]
            if let zone = firstIndex(of: Array("%25".utf8), in: hostname) {
                unescaped = try unescape(Array(hostname[..<zone]), .host)
                    + unescape(Array(hostname[zone...]), .zone)
            } else {
                unescaped = try unescape(hostname, .host)
            }
            guard GoIP.isIPv6Literal(unescaped) else {
                throw ParseError(description: "invalid host")
            }
            return [UInt8(ascii: "[")] + unescaped + [UInt8(ascii: "]")] + port
        }
        if let firstColon = host.firstIndex(of: UInt8(ascii: ":")) {
            let strict = scheme == "http" || scheme == "https"
            let colon = strict ? firstColon : host.lastIndex(of: UInt8(ascii: ":"))!
            let colonPort = Array(host[colon...])
            guard isValidOptionalPort(colonPort) else {
                throw ParseError(description: "invalid port " + GoText.quote(GoText.string(colonPort)) + " after host")
            }
        }
        return try unescape(host, .host)
    }

    // MARK: - Byte helpers

    static func cut(_ s: [UInt8], _ sep: UInt8) -> ([UInt8], [UInt8], Bool) {
        guard let i = s.firstIndex(of: sep) else { return (s, [], false) }
        return (Array(s[..<i]), Array(s[(i + 1)...]), true)
    }

    private static func validEncoded(_ s: [UInt8], _ mode: Mode) -> Bool {
        for c in s {
            switch Unicode.Scalar(c) {
            case "!", "$", "&", "'", "(", ")", "*", "+", ",", ";", "=", ":", "@", "[", "]", "%":
                continue
            default:
                if shouldEscape(c, mode) { return false }
            }
        }
        return true
    }

    private static func isValidOptionalPort(_ port: [UInt8]) -> Bool {
        guard let first = port.first else { return true }
        guard first == UInt8(ascii: ":") else { return false }
        return port.dropFirst().allSatisfy(isDigit)
    }

    private static func isUserinfoByte(_ c: UInt8) -> Bool {
        if isAlnum(c) { return true }
        switch Unicode.Scalar(c) {
        case "-", ".", "_", ":", "~", "!", "$", "&", "'", "(", ")", "*", "+", ",", ";", "=", "%", "@":
            return true
        default:
            return false
        }
    }

    private static func escapeError(_ s: [UInt8]) -> ParseError {
        ParseError(description: "invalid URL escape " + GoText.quote(GoText.string(s)))
    }

    private static func hasPrefix(_ s: [UInt8], _ prefix: String) -> Bool {
        s.starts(with: prefix.utf8)
    }

    private static func firstIndex(of needle: [UInt8], in s: [UInt8]) -> Int? {
        guard needle.count <= s.count else { return nil }
        for i in 0...(s.count - needle.count) where s[i..<(i + needle.count)].elementsEqual(needle) {
            return i
        }
        return nil
    }

    static func isHex(_ c: UInt8) -> Bool {
        isDigit(c) || (c >= 0x61 && c <= 0x66) || (c >= 0x41 && c <= 0x46)
    }

    static func unhex(_ c: UInt8) -> UInt8 { 9 &* (c >> 6) &+ (c & 15) }

    static func isDigit(_ c: UInt8) -> Bool { c >= 0x30 && c <= 0x39 }

    static func isAlpha(_ c: UInt8) -> Bool { (c >= 0x41 && c <= 0x5A) || (c >= 0x61 && c <= 0x7A) }

    static func isAlnum(_ c: UInt8) -> Bool { isAlpha(c) || isDigit(c) }

    static func asciiLower(_ c: UInt8) -> UInt8 { (c >= 0x41 && c <= 0x5A) ? c + 0x20 : c }
}

/// Go `netip.ParseAddr` acceptance for a bracketed URL host: true only for an
/// IPv6 address, optionally with a non-empty zone.
enum GoIP {
    static func isIPv6Literal(_ s: [UInt8]) -> Bool {
        for c in s {
            switch c {
            case UInt8(ascii: "."), UInt8(ascii: "%"): return false
            case UInt8(ascii: ":"): return parseIPv6(s)
            default: continue
            }
        }
        return false
    }

    private static func parseIPv6(_ input: [UInt8]) -> Bool {
        var s = input[...]
        if let pct = input.firstIndex(of: UInt8(ascii: "%")) {
            s = input[..<pct]
            if pct + 1 >= input.count { return false }
        }
        let colon = UInt8(ascii: ":")
        var ellipsis = -1
        if s.count >= 2 && s[s.startIndex] == colon && s[s.startIndex + 1] == colon {
            ellipsis = 0
            s = s.dropFirst(2)
            if s.isEmpty { return true }
        }
        var i = 0
        while i < 16 {
            var off = 0
            var acc: UInt32 = 0
            while off < s.count {
                let c = s[s.startIndex + off]
                guard let d = hexValue(c) else { break }
                acc = acc << 4 + d
                if off > 3 || acc > 0xFFFF { return false }
                off += 1
            }
            if off == 0 { return false }
            if off < s.count && s[s.startIndex + off] == UInt8(ascii: ".") {
                if ellipsis < 0 && i != 12 { return false }
                if i + 4 > 16 { return false }
                guard isIPv4(Array(s)) else { return false }
                s = []
                i += 4
                break
            }
            i += 2
            s = s.dropFirst(off)
            if s.isEmpty { break }
            if s[s.startIndex] != colon || s.count == 1 { return false }
            s = s.dropFirst()
            if s[s.startIndex] == colon {
                if ellipsis >= 0 { return false }
                ellipsis = i
                s = s.dropFirst()
                if s.isEmpty { break }
            }
        }
        if !s.isEmpty { return false }
        if i < 16 { return ellipsis >= 0 }
        return ellipsis < 0
    }

    private static func isIPv4(_ s: [UInt8]) -> Bool {
        var val = 0, pos = 0, digits = 0
        for (i, c) in s.enumerated() {
            if GoURL.isDigit(c) {
                if digits == 1 && val == 0 { return false }
                val = val * 10 + Int(c - 0x30)
                digits += 1
                if val > 255 { return false }
            } else if c == UInt8(ascii: ".") {
                if i == 0 || i == s.count - 1 || s[i - 1] == UInt8(ascii: ".") { return false }
                if pos == 3 { return false }
                pos += 1
                val = 0
                digits = 0
            } else {
                return false
            }
        }
        return pos == 3
    }

    private static func hexValue(_ c: UInt8) -> UInt32? {
        switch c {
        case 0x30...0x39: return UInt32(c - 0x30)
        case 0x61...0x66: return UInt32(c - 0x61 + 10)
        case 0x41...0x46: return UInt32(c - 0x41 + 10)
        default: return nil
        }
    }
}
