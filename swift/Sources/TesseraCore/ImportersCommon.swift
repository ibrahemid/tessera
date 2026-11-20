import Foundation

extension Importers {

    /// Normalizes one imported entry whose secret is written in base32.
    static func buildAccount(type: String, issuer: String, acct: String,
                             secretB32: String, algo: String,
                             digits: Int, period: Int, counter: Int64) throws -> Account {
        let secret: Data
        do { secret = try Base32.decode(secretB32) }
        catch { throw ImporterError.malformed(.invalidSecret, "decode secret: \(message(error))") }
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
                       issuer: TextClean.text(issuer),
                       account: TextClean.text(acct),
                       secret: secret, algorithm: algorithm, digits: d, period: p, counter: counter)
    }

    /// Reads one opaque OTP field — the shape used by every password manager
    /// that stores a single string per item — under the shared rule in
    /// `/spec/otpauth.md`. Returns `nil` when the field is empty, which means
    /// the item carries no second factor and is skipped silently rather than
    /// failing.
    static func parseOTPValue(_ value: String, _ issuerFallback: String,
                              _ accountFallback: String) throws -> Account? {
        let v = GoText.trimSpace(value)
        if v.isEmpty { return nil }
        let lower = GoText.toLower(v)
        if GoText.hasPrefix(lower, "otpauth://") {
            do { return try OTPAuth.parse(v) }
            catch { throw ImporterError.malformed(.invalidLink, message(error)) }
        }
        if GoText.hasPrefix(lower, "steam://") {
            var body = String(String.UnicodeScalarView(v.unicodeScalars.dropFirst("steam://".count)))
            if body.utf8.last == UInt8(ascii: "/") { body.unicodeScalars.removeLast() }
            guard let secret = try? Base32.decode(body) else {
                throw ImporterError.malformed(.invalidSecret, "unsupported Steam secret encoding")
            }
            var acct = TextClean.text(accountFallback)
            if acct.isEmpty { acct = TextClean.text(issuerFallback) }
            return Account(id: "", type: .steam, issuer: "Steam", account: acct,
                           secret: secret, algorithm: "SHA1", digits: 5, period: 30)
        }
        let secret: Data
        do { secret = try Base32.decode(v) }
        catch { throw ImporterError.malformed(.invalidSecret, "decode secret: \(message(error))") }
        return Account(id: "", type: .totp,
                       issuer: TextClean.text(issuerFallback),
                       account: TextClean.text(accountFallback),
                       secret: secret, algorithm: "SHA1", digits: 6, period: 30)
    }

    /// Rejects OTP schemes Tessera can't generate (Yandex, mOTP, ...) so an
    /// import never silently produces wrong codes.
    static func mapType(_ s: String) throws -> OTPType {
        switch GoText.toLower(GoText.trimSpace(s)) {
        case "", "totp": return .totp
        case "hotp": return .hotp
        case "steam", "steam_totp", "steamtotp": return .steam
        default: throw ImporterError.malformed(.unsupportedType, "unsupported account type \(GoText.quote(s))")
        }
    }

    static func mapAlgo(_ s: String) throws -> String {
        switch GoText.toUpper(GoText.trimSpace(s)) {
        case "", "SHA1": return "SHA1"
        case "SHA256": return "SHA256"
        case "SHA512": return "SHA512"
        default: throw ImporterError.malformed(.unsupportedAlgorithm, "unsupported algorithm \(GoText.quote(s))")
        }
    }

    static func atoiDefault(_ s: String, _ def: Int) throws -> Int {
        let t = GoText.trimSpace(s)
        if t.isEmpty { return def }
        guard let n = GoText.atoi(t) else {
            throw ImporterError.malformed(.invalidNumber, "not a number: \(GoText.quote(t))")
        }
        return n
    }

    /// Runs one format's decode and per-item walk, prefixing a failure the
    /// way the Go importer wraps it.
    static func wrapping<T>(_ prefix: String, _ body: () throws -> T) throws -> T {
        do { return try body() }
        catch let e as ImporterError {
            switch e {
            case .encrypted(let s, let m): throw ImporterError.encrypted(s, prefix + m)
            case .malformed(let k, let m): throw ImporterError.malformed(k, prefix + m)
            }
        } catch {
            throw ImporterError.malformed(kind(of: error), prefix + message(error))
        }
    }

    /// A failed entry as a malformed-export error: `prefix` followed by Go's
    /// message for `error`, keeping the kind the entry failed with.
    static func entryError(_ prefix: String, _ error: Error) -> ImporterError {
        .malformed(kind(of: error), prefix + message(error))
    }

    /// The kind a non-encrypted import failure maps to.
    static func kind(of error: Error) -> MalformedKind {
        switch error {
        case let e as ImporterError: return e.kind ?? .invalidEntry
        case is GoJSONTypeError: return .invalidJSON
        default: return .invalidEntry
        }
    }

    /// The message Go's error chain prints for `error`.
    static func message(_ error: Error) -> String {
        switch error {
        case let e as ImporterError: return e.description
        case let e as GoJSONTypeError: return e.message
        case let e as AccountError:
            if case .invalid(let m) = e { return m }
            return e.description
        default: return "\(error)"
        }
    }

    /// Decodes a validated JSON document into a format's file struct, giving
    /// Go's `encoding/json` error text on a type mismatch.
    static func decodeJSON<T>(_ doc: GoJSON, root: String, at node: Int? = nil, into value: inout T,
                              _ decode: (GoJSONDecoder, Int, inout T) throws -> Void) throws {
        let decoder = GoJSONDecoder(doc, root: root)
        do { try decode(decoder, node ?? doc.root, &value) }
        catch let e as GoJSONTypeError { throw ImporterError.malformed(.invalidJSON, e.message) }
    }
}

extension GoText {
    /// Go `strings.HasPrefix`: a byte comparison, never a canonical one.
    static func hasPrefix(_ s: String, _ prefix: String) -> Bool {
        s.utf8.starts(with: prefix.utf8)
    }

    /// Go string equality: byte for byte.
    static func equal(_ a: String, _ b: String) -> Bool {
        a.utf8.elementsEqual(b.utf8)
    }
}
