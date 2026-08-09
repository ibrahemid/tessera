import Foundation

/// Parses and emits otpauth:// URIs into the canonical account model.
public enum OTPAuth {

    public static func parse(_ uri: String) throws -> Account {
        guard let comps = URLComponents(string: uri.trimmingCharacters(in: .whitespacesAndNewlines)),
              comps.scheme == "otpauth" else {
            throw AccountError.invalid("not an otpauth uri")
        }
        let host = (comps.host ?? "").lowercased()
        var type: OTPType
        switch host {
        case "totp": type = .totp
        case "hotp": type = .hotp
        case "steam": type = .steam
        default: throw AccountError.invalid("unsupported type \(host)")
        }

        // spec/otpauth.md § label encoding: split the ENCODED label on its first
        // literal ':', then percent-decode each side once. Decoding first would
        // make a '%3A' inside an issuer or account look like the separator.
        var label = comps.percentEncodedPath
        if label.hasPrefix("/") { label.removeFirst() }
        var rawIssuer = "", rawAccount = label
        if let r = label.range(of: ":") {
            rawIssuer = String(label[..<r.lowerBound])
            rawAccount = String(label[r.upperBound...])
        }
        var issuer = (rawIssuer.removingPercentEncoding ?? rawIssuer).trimmingCharacters(in: .whitespaces)
        let account = (rawAccount.removingPercentEncoding ?? rawAccount).trimmingCharacters(in: .whitespaces)

        // spec/otpauth.md § query encoding: decode the still-encoded query items
        // ourselves so a '+' is read as a space. URLComponents.queryItems leaves
        // '+' literal, which would break every URI emitted in the legacy form
        // (Go url.Values.Encode, Aegis, and others).
        let items = (comps.percentEncodedQueryItems ?? []).map {
            URLQueryItem(name: decodeQueryComponent($0.name), value: $0.value.map(decodeQueryComponent))
        }
        func q(_ name: String) -> String? { items.first { $0.name == name }?.value }

        guard let secretParam = q("secret"), !secretParam.isEmpty else {
            throw AccountError.invalid("missing secret")
        }
        let secret = try Base32.decode(secretParam)

        if let iss = q("issuer"), !iss.isEmpty {
            if !issuer.isEmpty && issuer != iss {
                throw AccountError.invalid("issuer mismatch")
            }
            issuer = iss
        }

        // Steam heuristic: issuer "Steam" with a TOTP type is treated as Steam.
        if type == .totp && issuer.lowercased() == "steam" {
            type = .steam
        }

        var algorithm = "SHA1"
        if let alg = q("algorithm"), !alg.isEmpty {
            algorithm = try OTP.Algorithm.parse(alg).rawValue
        }
        var digits = type == .steam ? 5 : 6
        if let d = q("digits"), !d.isEmpty {
            let n = Int(d)
            let bad = type == .steam ? (n != 5) : (n == nil || n! < 6 || n! > 8)
            if bad { throw AccountError.invalid("invalid digits") }
            digits = n!
        }
        var period = 30
        if let p = q("period"), !p.isEmpty {
            guard let n = Int(p), n > 0 else { throw AccountError.invalid("invalid period") }
            period = n
        }
        var counter: Int64 = 0
        if type == .hotp {
            guard let c = q("counter"), let n = Int64(c), n >= 0 else {
                throw AccountError.invalid("hotp requires a valid counter")
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
        default: typ = "totp"
        }
        // Each component is escaped on its own so the only bare ':' in the label
        // is the separator (spec/otpauth.md § label encoding).
        let escapedAccount = escapeLabelPart(a.account)
        let label = a.issuer.isEmpty ? escapedAccount : "\(escapeLabelPart(a.issuer)):\(escapedAccount)"
        var comps = URLComponents()
        comps.scheme = "otpauth"
        comps.host = typ
        comps.percentEncodedPath = "/" + label
        var items: [URLQueryItem] = [URLQueryItem(name: "secret", value: Base32.encodeNoPad(a.secret))]
        if !a.issuer.isEmpty { items.append(URLQueryItem(name: "issuer", value: a.issuer)) }
        if a.algorithm != "SHA1" { items.append(URLQueryItem(name: "algorithm", value: a.algorithm)) }
        if a.type == .steam {
            items.append(URLQueryItem(name: "digits", value: "5"))
        } else if a.digits != 6 {
            items.append(URLQueryItem(name: "digits", value: String(a.digits)))
        }
        if a.type != .hotp && a.period != 30 { items.append(URLQueryItem(name: "period", value: String(a.period))) }
        if a.type == .hotp { items.append(URLQueryItem(name: "counter", value: String(a.counter))) }
        comps.percentEncodedQueryItems = items.map {
            URLQueryItem(name: encodeQueryComponent($0.name), value: $0.value.map(encodeQueryComponent))
        }
        return comps.string ?? ""
    }

    /// Percent-decodes one query component the way Go's `url.Query` does: '+'
    /// is a space, and it is substituted BEFORE percent-decoding so that a
    /// literal plus written as `%2B` survives as a plus.
    private static func decodeQueryComponent(_ s: String) -> String {
        let plusAsSpace = s.replacingOccurrences(of: "+", with: " ")
        return plusAsSpace.removingPercentEncoding ?? plusAsSpace
    }

    /// Percent-encodes one query component (spec/otpauth.md § query encoding):
    /// everything outside the RFC 3986 unreserved set is escaped, so a space is
    /// `%20` and a literal plus is `%2B`. URLComponents' own query escaping
    /// leaves '+' alone, which a parser that reads '+' as a space would turn
    /// into whitespace.
    private static func encodeQueryComponent(_ s: String) -> String {
        s.addingPercentEncoding(withAllowedCharacters: queryComponentAllowed) ?? s
    }

    /// RFC 3986 unreserved characters, ASCII only. `CharacterSet.alphanumerics`
    /// would let non-ASCII letters through unescaped; Go escapes every byte
    /// outside this set, and both cores must emit the same query.
    private static let queryComponentAllowed = CharacterSet(
        charactersIn: "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-._~")

    /// Percent-encodes one label component. ':' and '/' are always escaped: the
    /// first is the issuer/account separator and the second ends the label.
    private static func escapeLabelPart(_ s: String) -> String {
        var allowed = CharacterSet.urlPathAllowed
        allowed.remove(charactersIn: ":/")
        return s.addingPercentEncoding(withAllowedCharacters: allowed) ?? s
    }
}
