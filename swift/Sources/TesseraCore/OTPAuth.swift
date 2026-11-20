import Foundation

/// Parses and emits otpauth:// URIs into the canonical account model. Accepts,
/// rejects and normalises exactly what Go `otpauth.Parse` and `otpauth.Format`
/// do (spec/otpauth.md), including their error text.
public enum OTPAuth {

    /// The largest HOTP counter a vault may hold (spec/vault-format.md).
    static let maxCounter: Int64 = Int64.max - 1

    public static func parse(_ uri: String) throws -> Account {
        let u: GoURL
        do { u = try GoURL.parse(GoText.trimSpace(uri)) } catch {
            throw AccountError.invalid("otpauth: invalid URI")
        }
        guard u.scheme == "otpauth" else {
            throw AccountError.invalid("otpauth: not an otpauth uri (scheme \(GoText.quoteValue(u.scheme)))")
        }
        var type: OTPType
        switch GoText.toLower(GoText.string(u.host)) {
        case "totp": type = .totp
        case "hotp": type = .hotp
        case "steam": type = .steam
        default: throw AccountError.invalid("otpauth: unsupported type \(GoText.quoteValue(bytes: u.host))")
        }

        let (labelIssuer, account) = try splitLabel(u.escapedPath)
        var issuer = labelIssuer
        let q = try parseQuery(u.rawQuery)

        func param(_ key: String) -> [UInt8] { q[Array(key.utf8)] ?? [] }

        let secretParam = GoText.string(param("secret"))
        if secretParam.isEmpty { throw AccountError.invalid("otpauth: missing secret") }
        let secret: Data
        do { secret = try Base32.decode(secretParam) } catch let AccountError.invalid(m) {
            throw AccountError.invalid("otpauth: bad secret: \(m)")
        }

        let iss = TextClean.text(GoText.string(param("issuer")))
        if !iss.isEmpty {
            if !issuer.isEmpty && issuer != iss {
                throw AccountError.invalid(
                    "otpauth: issuer mismatch (label \(GoText.quoteValue(issuer)), param \(GoText.quoteValue(iss)))")
            }
            issuer = iss
        }

        if type == .totp && GoText.equalFold(issuer, ascii: "Steam") { type = .steam }

        var algorithm = "SHA1"
        let alg = param("algorithm")
        if !alg.isEmpty {
            let upper = GoText.toUpper(GoText.string(alg))
            guard ["SHA1", "SHA256", "SHA512"].contains(upper) else {
                throw AccountError.invalid("otpauth: unsupported algorithm \(GoText.quoteValue(bytes: alg))")
            }
            algorithm = upper
        }

        var digits = type == .steam ? 5 : 6
        let d = param("digits")
        if !d.isEmpty {
            let n = GoText.atoi(GoText.string(d))
            let bad = type == .steam ? n != 5 : (n.map { $0 < 6 || $0 > 8 } ?? true)
            if bad { throw AccountError.invalid("otpauth: invalid digits \(GoText.quoteValue(bytes: d))") }
            digits = n!
        }

        var period = 30
        let p = param("period")
        if !p.isEmpty {
            guard let n = GoText.atoi(GoText.string(p)), n > 0 else {
                throw AccountError.invalid("otpauth: invalid period \(GoText.quoteValue(bytes: p))")
            }
            period = n
        }

        var counter: Int64 = 0
        if type == .hotp {
            let c = param("counter")
            if c.isEmpty { throw AccountError.invalid("otpauth: hotp requires counter") }
            guard let n = GoText.parseInt64(GoText.string(c)), n >= 0, n <= maxCounter else {
                throw AccountError.invalid("otpauth: invalid counter \(GoText.quoteValue(bytes: c))")
            }
            counter = n
        }

        return Account(id: "", type: type, issuer: issuer, account: account, secret: secret,
                       algorithm: algorithm, digits: digits, period: period, counter: counter)
    }

    public static func format(_ a: Account) -> String {
        let typ: String
        switch a.type {
        case .hotp: typ = "hotp"
        case .steam: typ = "steam"
        case .totp, .unrecognized: typ = "totp"
        }
        var label = escapeLabelPart(a.account)
        if !a.issuer.isEmpty { label = escapeLabelPart(a.issuer) + ":" + label }

        var v: [String: String] = ["secret": Base32.encodeNoPad(a.secret)]
        if !a.issuer.isEmpty { v["issuer"] = a.issuer }
        if !a.algorithm.isEmpty && a.algorithm != "SHA1" { v["algorithm"] = a.algorithm }
        if a.type == .steam {
            v["digits"] = "5"
        } else if a.digits != 0 && a.digits != 6 {
            v["digits"] = String(a.digits)
        }
        if a.type != .hotp && a.period != 0 && a.period != 30 { v["period"] = String(a.period) }
        if a.type == .hotp { v["counter"] = String(a.counter) }
        return "otpauth://\(typ)/\(label)?\(encodeQuery(v))"
    }

    /// Go `url.Values.Encode` with every `+` then written as `%20`: keys sorted,
    /// keys and values query-escaped.
    static func encodeQuery(_ v: [String: String]) -> String {
        v.keys.sorted { Array($0.utf8).lexicographicallyPrecedes(Array($1.utf8)) }
            .map { GoURL.escape($0, .queryComponent) + "=" + GoURL.escape(v[$0]!, .queryComponent) }
            .joined(separator: "&")
            .replacingOccurrences(of: "+", with: "%20")
    }

    private static func escapeLabelPart(_ s: String) -> String {
        GoURL.escape(s, .pathSegment).replacingOccurrences(of: ":", with: "%3A")
    }

    /// Splits the still-escaped label on its first ':' and unescapes each side
    /// once, so a `%3A` inside a component stays data (spec/otpauth.md).
    private static func splitLabel(_ escapedPath: [UInt8]) throws -> (String, String) {
        var label = escapedPath[...]
        if label.first == UInt8(ascii: "/") { label = label.dropFirst() }
        var (rawIssuer, rawAccount, hasIssuer) = GoURL.cut(Array(label), UInt8(ascii: ":"))
        if !hasIssuer { (rawIssuer, rawAccount) = ([], Array(label)) }
        let issuer: [UInt8], account: [UInt8]
        do { issuer = try GoURL.unescape(rawIssuer, .pathSegment) } catch {
            throw AccountError.invalid("otpauth: bad label issuer: \(error)")
        }
        do { account = try GoURL.unescape(rawAccount, .pathSegment) } catch {
            throw AccountError.invalid("otpauth: bad label account: \(error)")
        }
        return (TextClean.text(GoText.string(issuer)), TextClean.text(GoText.string(account)))
    }

    /// The query as Go `otpauth.parseQuery` reads it: pairs split on '&', empty
    /// pairs skipped, every key and value query-unescaped (one bad escape
    /// anywhere rejects the link), and the first occurrence of a key wins.
    private static func parseQuery(_ rawQuery: [UInt8]) throws -> [[UInt8]: [UInt8]] {
        var q: [[UInt8]: [UInt8]] = [:]
        var rest = rawQuery[...]
        while !rest.isEmpty {
            let (pair, after, _) = GoURL.cut(rest, UInt8(ascii: "&"))
            rest = after
            if pair.isEmpty { continue }
            let (rawKey, rawValue, _) = GoURL.cut(pair, UInt8(ascii: "="))
            let key: [UInt8], value: [UInt8]
            do {
                key = try GoURL.unescape(Array(rawKey), .queryComponent)
                value = try GoURL.unescape(Array(rawValue), .queryComponent)
            } catch {
                throw AccountError.invalid("otpauth: invalid percent-encoding in query")
            }
            if q[key] == nil { q[key] = value }
        }
        return q
    }
}
