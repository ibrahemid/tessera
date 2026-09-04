import Foundation

extension Importers {

    /// Normalizes one imported entry whose secret is written in base32.
    static func buildAccount(type: String, issuer: String, acct: String,
                             secretB32: String, algo: String,
                             digits: Int, period: Int, counter: Int64) throws -> Account {
        let secret: Data
        do { secret = try Base32.decode(secretB32) }
        catch { throw ImporterError.malformed("decode secret: \(message(error))") }
        return try buildAccountRaw(type: type, issuer: issuer, acct: acct, secret: secret,
                                   algo: algo, digits: digits, period: period, counter: counter)
    }

    /// `buildAccount` for sources that store the secret as raw key bytes instead
    /// of base32 (FreeOTP+ ships a Java byte array).
    static func buildAccountRaw(type: String, issuer: String, acct: String,
                                secret: Data, algo: String,
                                digits: Int, period: Int, counter: Int64) throws -> Account {
        let t = try mapType(type)
        let algorithm = try mapAlgo(algo)
        var d = digits
        if d == 0 { d = 6 }
        if t == .steam { d = 5 }
        var p = period
        if t != .hotp && p == 0 { p = 30 }
        return Account(id: "", type: t,
                       issuer: issuer.trimmingCharacters(in: .whitespacesAndNewlines),
                       account: acct.trimmingCharacters(in: .whitespacesAndNewlines),
                       secret: secret, algorithm: algorithm, digits: d, period: p, counter: counter)
    }

    /// Reads one opaque OTP field — the shape used by every password manager
    /// that stores a single string per item — under the shared rule in
    /// `/spec/otpauth.md`. Returns `nil` when the field is empty, which means
    /// the item carries no second factor and is skipped silently rather than
    /// failing.
    static func parseOTPValue(_ value: String, _ issuerFallback: String,
                              _ accountFallback: String) throws -> Account? {
        let v = value.trimmingCharacters(in: .whitespacesAndNewlines)
        if v.isEmpty { return nil }
        let lower = v.lowercased()
        if lower.hasPrefix("otpauth://") {
            return try OTPAuth.parse(v)
        }
        if lower.hasPrefix("steam://") {
            var body = String(v.dropFirst("steam://".count))
            if body.hasSuffix("/") { body = String(body.dropLast()) }
            guard let secret = try? Base32.decode(body) else {
                throw ImporterError.malformed("unsupported Steam secret encoding")
            }
            var acct = accountFallback
            if acct.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty { acct = issuerFallback }
            return Account(id: "", type: .steam, issuer: "Steam",
                           account: acct.trimmingCharacters(in: .whitespacesAndNewlines),
                           secret: secret, algorithm: "SHA1", digits: 5, period: 30)
        }
        let secret: Data
        do { secret = try Base32.decode(v) }
        catch { throw ImporterError.malformed("decode secret: \(message(error))") }
        return Account(id: "", type: .totp,
                       issuer: issuerFallback.trimmingCharacters(in: .whitespacesAndNewlines),
                       account: accountFallback.trimmingCharacters(in: .whitespacesAndNewlines),
                       secret: secret, algorithm: "SHA1", digits: 6, period: 30)
    }

    /// Rejects OTP schemes Tessera can't generate (Yandex, mOTP, ...) so an
    /// import never silently produces wrong codes.
    static func mapType(_ s: String) throws -> OTPType {
        switch s.trimmingCharacters(in: .whitespacesAndNewlines).lowercased() {
        case "", "totp": return .totp
        case "hotp": return .hotp
        case "steam", "steam_totp", "steamtotp": return .steam
        default: throw ImporterError.malformed("unsupported account type \"\(s)\"")
        }
    }

    static func mapAlgo(_ s: String) throws -> String {
        switch s.trimmingCharacters(in: .whitespacesAndNewlines).uppercased() {
        case "", "SHA1": return "SHA1"
        case "SHA256": return "SHA256"
        case "SHA512": return "SHA512"
        default: throw ImporterError.malformed("unsupported algorithm \"\(s)\"")
        }
    }

    static func atoiDefault(_ s: String, _ def: Int) throws -> Int {
        let t = s.trimmingCharacters(in: .whitespacesAndNewlines)
        if t.isEmpty { return def }
        guard let n = Int(t) else {
            throw ImporterError.malformed("not a number: \"\(s)\"")
        }
        return n
    }

    // MARK: - helpers

    static func str(_ v: Any?) -> String { v as? String ?? "" }
    static func intVal(_ v: Any?) -> Int { (v as? NSNumber)?.intValue ?? 0 }
    static func int64Val(_ v: Any?) -> Int64 { (v as? NSNumber)?.int64Value ?? 0 }

    static func message(_ error: Error) -> String {
        if let ie = error as? ImporterError { return ie.description }
        if let ae = error as? AccountError { return ae.description }
        return "\(error)"
    }

    static func trimSpace(_ data: Data) -> Data {
        let ws: Set<UInt8> = [0x20, 0x09, 0x0a, 0x0d, 0x0b, 0x0c]
        var start = data.startIndex
        var end = data.endIndex
        while start < end, ws.contains(data[start]) { start = data.index(after: start) }
        while end > start, ws.contains(data[data.index(before: end)]) { end = data.index(before: end) }
        return data.subdata(in: start..<end)
    }
}
