import Foundation

/// Parses plaintext (unencrypted) account exports from other authenticator apps
/// — Aegis, 2FAS, and Raivo — into the canonical `Account` model. Encrypted
/// exports are detected and rejected with a clear error rather than parsed
/// wrong; decrypt them in the source app and re-export, or export unencrypted.
/// Secrets are decoded from base32 to raw bytes here.
public enum Importers {

    /// An error from a recognized-but-unusable export. `description` /
    /// `errorDescription` carry the user-facing message (the app surfaces errors
    /// via string interpolation, like `AccountError`).
    public enum ImporterError: Error, LocalizedError, CustomStringConvertible {
        /// A recognized export that is encrypted.
        case encrypted(String)
        /// A recognized export with a malformed or unsupported entry.
        case malformed(String)

        public var description: String {
            switch self {
            case .encrypted(let m), .malformed(let m): return m
            }
        }
        public var errorDescription: String? { description }
    }

    /// Detects a supported app export and returns its accounts and source name.
    ///
    /// Returns `nil` when `data` is not a recognized app export (the caller may
    /// fall back to parsing otpauth lines). Throws `ImporterError` when the
    /// export is recognized but encrypted or malformed, so the caller surfaces
    /// the real reason instead of a generic parse failure.
    public static func parse(_ data: Data) throws -> (accounts: [Account], source: String)? {
        let trimmed = trimSpace(data)
        guard let first = trimmed.first else { return nil }
        switch first {
        case UInt8(ascii: "["):
            // A top-level JSON array is a Raivo export; malformed entries surface
            // their real error instead of falling through to the otpauth parser.
            guard isValidJSON(trimmed) else { return nil }
            return (try parseRaivo(trimmed), "Raivo")
        case UInt8(ascii: "{"):
            // Probe the keys to pick the format.
            guard let probe = try? JSONSerialization.jsonObject(with: trimmed) as? [String: Any] else {
                return nil
            }
            if probe["db"] != nil {
                return (try parseAegis(probe), "Aegis")
            }
            // Encrypted 2FAS backups carry BOTH an empty "services" array and the
            // ciphertext in "servicesEncrypted" — check the ciphertext first.
            if let enc = probe["servicesEncrypted"] as? String, !enc.isEmpty {
                throw ImporterError.encrypted("2FAS export is encrypted; in 2FAS turn off the backup password (or decrypt) and export again")
            }
            if probe["services"] != nil {
                return (try parse2FAS(probe), "2FAS")
            }
            return nil
        default:
            return nil
        }
    }

    // ---- Aegis ----

    private static func parseAegis(_ probe: [String: Any]) throws -> [Account] {
        // In an encrypted export "db" is a base64 string, not an object.
        if probe["db"] is String {
            throw ImporterError.encrypted("Aegis export is encrypted; export with encryption off (Aegis: Settings > Import/Export > Export, untick encryption) and try again")
        }
        guard let db = probe["db"] as? [String: Any] else {
            throw ImporterError.malformed("aegis db: not an object")
        }
        let entries = db["entries"] as? [[String: Any]] ?? []
        var out: [Account] = []
        out.reserveCapacity(entries.count)
        for e in entries {
            let name = e["name"] as? String ?? ""
            let info = e["info"] as? [String: Any] ?? [:]
            do {
                out.append(try buildAccount(
                    type: e["type"] as? String ?? "",
                    issuer: e["issuer"] as? String ?? "",
                    acct: name,
                    secretB32: info["secret"] as? String ?? "",
                    algo: info["algo"] as? String ?? "",
                    digits: intVal(info["digits"]),
                    period: intVal(info["period"]),
                    counter: int64Val(info["counter"])))
            } catch {
                throw ImporterError.malformed("aegis entry \"\(name)\": \(message(error))")
            }
        }
        return out
    }

    // ---- 2FAS ----

    private static func parse2FAS(_ probe: [String: Any]) throws -> [Account] {
        let services = probe["services"] as? [[String: Any]] ?? []
        var out: [Account] = []
        out.reserveCapacity(services.count)
        for s in services {
            let name = s["name"] as? String ?? ""
            let otp = s["otp"] as? [String: Any] ?? [:]
            var issuer = otp["issuer"] as? String ?? ""
            if issuer.isEmpty { issuer = name }
            do {
                out.append(try buildAccount(
                    type: otp["tokenType"] as? String ?? "",
                    issuer: issuer,
                    acct: otp["account"] as? String ?? "",
                    secretB32: s["secret"] as? String ?? "",
                    algo: otp["algorithm"] as? String ?? "",
                    digits: intVal(otp["digits"]),
                    period: intVal(otp["period"]),
                    counter: int64Val(otp["counter"])))
            } catch {
                throw ImporterError.malformed("2fas service \"\(name)\": \(message(error))")
            }
        }
        return out
    }

    // ---- Raivo ----

    private static func parseRaivo(_ data: Data) throws -> [Account] {
        guard let entries = try? JSONSerialization.jsonObject(with: data) as? [[String: Any]] else {
            throw ImporterError.malformed("raivo: entries are not objects")
        }
        if entries.isEmpty {
            throw ImporterError.malformed("raivo: no entries")
        }
        var out: [Account] = []
        out.reserveCapacity(entries.count)
        for e in entries {
            let issuer = e["issuer"] as? String ?? ""
            // Raivo stores numbers as strings.
            let secret = e["secret"] as? String ?? ""
            if secret.isEmpty {
                throw ImporterError.malformed("raivo entry \"\(issuer)\": missing secret")
            }
            let digits: Int, period: Int, counter: Int
            do { digits = try atoiDefault(e["digits"] as? String ?? "", 6) }
            catch { throw ImporterError.malformed("raivo entry \"\(issuer)\": digits: \(message(error))") }
            do { period = try atoiDefault(e["timer"] as? String ?? "", 30) }
            catch { throw ImporterError.malformed("raivo entry \"\(issuer)\": timer: \(message(error))") }
            do { counter = try atoiDefault(e["counter"] as? String ?? "", 0) }
            catch { throw ImporterError.malformed("raivo entry \"\(issuer)\": counter: \(message(error))") }
            do {
                out.append(try buildAccount(
                    type: e["kind"] as? String ?? "",
                    issuer: issuer,
                    acct: e["account"] as? String ?? "",
                    secretB32: secret,
                    algo: e["algorithm"] as? String ?? "",
                    digits: digits,
                    period: period,
                    counter: Int64(counter)))
            } catch {
                throw ImporterError.malformed("raivo entry \"\(issuer)\": \(message(error))")
            }
        }
        return out
    }

    // ---- shared mapping ----

    private static func buildAccount(type: String, issuer: String, acct: String,
                                     secretB32: String, algo: String,
                                     digits: Int, period: Int, counter: Int64) throws -> Account {
        let secret: Data
        do { secret = try Base32.decode(secretB32) }
        catch { throw ImporterError.malformed("decode secret: \(message(error))") }
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

    /// Rejects OTP schemes Tessera can't generate (Yandex, mOTP, ...) so an
    /// import never silently produces wrong codes.
    private static func mapType(_ s: String) throws -> OTPType {
        switch s.trimmingCharacters(in: .whitespacesAndNewlines).lowercased() {
        case "", "totp": return .totp
        case "hotp": return .hotp
        case "steam", "steam_totp", "steamtotp": return .steam
        default: throw ImporterError.malformed("unsupported account type \"\(s)\"")
        }
    }

    private static func mapAlgo(_ s: String) throws -> String {
        switch s.trimmingCharacters(in: .whitespacesAndNewlines).uppercased() {
        case "", "SHA1": return "SHA1"
        case "SHA256": return "SHA256"
        case "SHA512": return "SHA512"
        default: throw ImporterError.malformed("unsupported algorithm \"\(s)\"")
        }
    }

    private static func atoiDefault(_ s: String, _ def: Int) throws -> Int {
        let t = s.trimmingCharacters(in: .whitespacesAndNewlines)
        if t.isEmpty { return def }
        guard let n = Int(t) else {
            throw ImporterError.malformed("not a number: \"\(s)\"")
        }
        return n
    }

    // ---- helpers ----

    private static func intVal(_ v: Any?) -> Int { (v as? NSNumber)?.intValue ?? 0 }
    private static func int64Val(_ v: Any?) -> Int64 { (v as? NSNumber)?.int64Value ?? 0 }

    private static func message(_ error: Error) -> String {
        if let ie = error as? ImporterError { return ie.description }
        if let ae = error as? AccountError { return ae.description }
        return "\(error)"
    }

    private static func isValidJSON(_ data: Data) -> Bool {
        (try? JSONSerialization.jsonObject(with: data)) != nil
    }

    private static func trimSpace(_ data: Data) -> Data {
        let ws: Set<UInt8> = [0x20, 0x09, 0x0a, 0x0d, 0x0b, 0x0c]
        var start = data.startIndex
        var end = data.endIndex
        while start < end, ws.contains(data[start]) { start = data.index(after: start) }
        while end > start, ws.contains(data[data.index(before: end)]) { end = data.index(before: end) }
        return data.subdata(in: start..<end)
    }
}
