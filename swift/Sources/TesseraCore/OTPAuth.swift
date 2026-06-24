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
        default: throw AccountError.invalid("unsupported type \(host)")
        }

        var label = comps.path
        if label.hasPrefix("/") { label.removeFirst() }
        label = label.removingPercentEncoding ?? label
        var issuer = "", account = ""
        if let r = label.range(of: ":") {
            issuer = String(label[..<r.lowerBound]).trimmingCharacters(in: .whitespaces)
            account = String(label[r.upperBound...]).trimmingCharacters(in: .whitespaces)
        } else {
            account = label.trimmingCharacters(in: .whitespaces)
        }

        let items = comps.queryItems ?? []
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

        var algorithm = "SHA1"
        if let alg = q("algorithm"), !alg.isEmpty {
            algorithm = try OTP.Algorithm.parse(alg).rawValue
        }
        var digits = 6
        if let d = q("digits"), let n = Int(d) {
            if n < 6 || n > 8 { throw AccountError.invalid("invalid digits") }
            digits = n
        }
        var period = 30
        if let p = q("period"), let n = Int(p) {
            if n <= 0 { throw AccountError.invalid("invalid period") }
            period = n
        }
        var counter: Int64 = 0
        if type == .hotp {
            guard let c = q("counter"), let n = Int64(c), n >= 0 else {
                throw AccountError.invalid("hotp requires a valid counter")
            }
            counter = n
        }

        if type == .totp && issuer.lowercased() == "steam" {
            type = .steam
        }
        return Account(id: "", type: type, issuer: issuer, account: account, secret: secret,
                       algorithm: algorithm, digits: digits, period: period, counter: counter)
    }

    public static func format(_ a: Account) -> String {
        let typ = a.type == .hotp ? "hotp" : "totp"
        let label = a.issuer.isEmpty ? a.account : "\(a.issuer):\(a.account)"
        var comps = URLComponents()
        comps.scheme = "otpauth"
        comps.host = typ
        comps.path = "/" + label
        var items: [URLQueryItem] = [URLQueryItem(name: "secret", value: Base32.encodeNoPad(a.secret))]
        if !a.issuer.isEmpty { items.append(URLQueryItem(name: "issuer", value: a.issuer)) }
        if a.algorithm != "SHA1" { items.append(URLQueryItem(name: "algorithm", value: a.algorithm)) }
        if a.digits != 6 { items.append(URLQueryItem(name: "digits", value: String(a.digits))) }
        if a.type != .hotp && a.period != 30 { items.append(URLQueryItem(name: "period", value: String(a.period))) }
        if a.type == .hotp { items.append(URLQueryItem(name: "counter", value: String(a.counter))) }
        comps.queryItems = items
        return comps.string ?? ""
    }
}
