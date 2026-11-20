import Foundation

/// Parses plaintext (unencrypted) account exports from other authenticator apps
/// and password managers into the canonical `Account` model. The container
/// switch and the format ladders are the interop contract in
/// `/spec/otpauth.md` ("Input detection"); the Go `internal/importers` package
/// must match. Encrypted exports are detected and rejected with a clear error
/// rather than parsed wrong. Secrets are decoded to raw bytes here and never
/// logged.
public enum Importers {

    /// An error from a recognized-but-unusable export. The case and its kind
    /// say what went wrong for code; `description` / `errorDescription` carry
    /// the Go core's text.
    public enum ImporterError: Error, Equatable, Sendable, LocalizedError, CustomStringConvertible {
        /// A recognized export that is encrypted, and the app that wrote it.
        case encrypted(EncryptedSource, String)
        /// A recognized export with a malformed or unsupported entry.
        case malformed(MalformedKind, String)

        /// The app that wrote an encrypted export; nil for a malformed one.
        public var source: EncryptedSource? {
            if case .encrypted(let s, _) = self { return s }
            return nil
        }

        /// What was wrong with a malformed export; nil for an encrypted one.
        public var kind: MalformedKind? {
            if case .malformed(let k, _) = self { return k }
            return nil
        }

        public var description: String {
            switch self {
            case .encrypted(_, let m), .malformed(_, let m): return m
            }
        }
        public var errorDescription: String? { description }
    }

    /// The app whose encrypted export was recognized.
    public enum EncryptedSource: Sendable, Equatable, CaseIterable {
        case aegis
        case twoFAS
        case enteAuth
        case bitwarden
        case protonAuthenticator
        case stratum
    }

    /// Why a recognized export could not be imported.
    public enum MalformedKind: Sendable, Equatable, CaseIterable {
        /// The input is larger than `maxInputBytes`, or a `.1pux` archive's
        /// export.data is larger than 64 MiB.
        case tooLarge
        /// A 1Password `.1pux` archive is damaged or holds no export.data.
        case archive
        /// The export parsed but holds no item with a one-time password.
        case noEntries
        /// A JSON value has the wrong type or the document ends early.
        case invalidJSON
        /// The CSV text does not parse.
        case invalidCSV
        /// A numeric field holds something other than a number.
        case invalidNumber
        /// An entry has no secret.
        case missingSecret
        /// An entry's secret does not decode.
        case invalidSecret
        /// An entry's OTP type is one Tessera cannot generate.
        case unsupportedType
        /// An entry's hash algorithm is one Tessera cannot use.
        case unsupportedAlgorithm
        /// An entry's otpauth link does not parse.
        case invalidLink
        /// An entry is invalid for another reason.
        case invalidEntry
    }

    /// The largest text, JSON, CSV or archive input accepted
    /// (`/spec/otpauth.md`, input limits).
    public static let maxInputBytes = 64 << 20

    static let inputTooLarge = "input is larger than 64 MiB"

    /// Detects a supported app export and returns its accounts and source name.
    ///
    /// Returns `nil` when `data` is not a recognized app export (the caller may
    /// fall back to parsing otpauth lines). Throws `ImporterError` when the
    /// input is larger than `maxInputBytes`, or the export is recognized but
    /// encrypted, malformed, or holds no OTP entries, so the caller surfaces
    /// the real reason instead of a generic parse failure. The first bad item
    /// fails the whole export, as in the Go importers.
    ///
    /// The container is classified from the raw bytes — an encrypted Stratum
    /// backup and a `.1pux` archive are not valid UTF-8, so a text-first path
    /// would never reach their messages.
    public static func parse(_ data: Data) throws -> (accounts: [Account], source: String)? {
        if data.count > maxInputBytes { throw ImporterError.malformed(.tooLarge, inputTooLarge) }
        guard let outcome = detect([UInt8](data)) else { return nil }
        switch outcome {
        case .success(let found): return found
        case .failure(_, let error): throw error
        }
    }

    enum Outcome {
        case success((accounts: [Account], source: String))
        case failure(source: String, ImporterError)
    }

    /// Go `importers.Parse`: nil when the data is not a recognized export,
    /// otherwise the result or the failure together with the source name.
    static func detect(_ data: [UInt8]) -> Outcome? {
        let trimmed = Array(GoText.trimSpace(bytes: data))
        guard let first = trimmed.first else { return nil }
        switch first {
        case UInt8(ascii: "["):
            return parseJSONArrayExport(trimmed)
        case UInt8(ascii: "{"):
            return parseJSONObjectExport(trimmed)
        default:
            break
        }
        if let format = matchCSVHeader(data) {
            return recognized(format.name) { try parseCSV(data, format) }
        }
        if isZip(data) {
            return recognized("1Password") { try parse1PUX(data) }
        }
        if isStratumEncrypted(data) {
            return .failure(source: "Stratum", .encrypted(.stratum, "Stratum backup is encrypted; in Stratum choose Settings > Backup > Export unencrypted and try again"))
        }
        return nil
    }

    /// Finishes one format branch. A recognized export that parsed cleanly but
    /// yielded no accounts is a failure, not an empty success: silent skipping
    /// of items without a second factor would otherwise make a password-manager
    /// export indistinguishable from an empty input.
    static func recognized(_ source: String, _ body: () throws -> [Account]) -> Outcome {
        do {
            let accounts = try body()
            if accounts.isEmpty {
                return .failure(source: source, .malformed(.noEntries, "\(source) export has no one-time-password entries; only items with a 2FA code are imported"))
            }
            return .success((accounts, source))
        } catch let e as ImporterError {
            return .failure(source: source, e)
        } catch {
            return .failure(source: source, .malformed(kind(of: error), message(error)))
        }
    }

    private static func has(_ probe: [[UInt8]: Int], _ key: String) -> Bool {
        probe[Array(key.utf8)] != nil
    }

    /// Walks the top-level key ladder of `/spec/otpauth.md`. Each app's
    /// encrypted probe precedes its plaintext probe. Keys match byte for byte
    /// and a repeated key keeps its last value, as in a Go map.
    private static func parseJSONObjectExport(_ trimmed: [UInt8]) -> Outcome? {
        guard let doc = GoJSON(trimmed), doc.kind(doc.root) == .object else { return nil }
        let probe = doc.probe(doc.root)
        func raw(_ key: String) -> Int? { probe[Array(key.utf8)] }

        if has(probe, "db") {
            return recognized("Aegis") { try parseAegis(doc) }
        }
        // Encrypted 2FAS backups carry BOTH an empty "services" array and the
        // ciphertext in "servicesEncrypted" — check the ciphertext first.
        if let enc = raw("servicesEncrypted"), doc.kind(enc) == .string, !doc.unquotedBytes(enc).isEmpty {
            return .failure(source: "2FAS", .encrypted(.twoFAS, "2FAS export is encrypted; in 2FAS turn off the backup password (or decrypt) and export again"))
        }
        if has(probe, "services") {
            return recognized("2FAS") { try parse2FAS(doc) }
        }
        if has(probe, "encryptedData"), has(probe, "encryptionNonce") {
            return .failure(source: "Ente Auth", .encrypted(.enteAuth, "Ente Auth export is encrypted; in Ente Auth choose Export > plain text (unencrypted) and try again"))
        }
        if let enc = raw("encrypted"), doc.kind(enc) == .trueValue {
            return .failure(source: "Bitwarden", .encrypted(.bitwarden, "Bitwarden export is encrypted; in Bitwarden export again as unencrypted .json (File format: .json, not 'Password protected')"))
        }
        if let items = raw("items"), doc.kind(items) == .array {
            // A password-manager export always carries folders or collections; an
            // authenticator export carries neither. A stripped export can collide,
            // which changes only the reported source: one walker parses both.
            let isPasswordManager = has(probe, "folders") || has(probe, "collections")
            let source = isPasswordManager ? "Bitwarden" : "Bitwarden Authenticator"
            return recognized(source) { try parseBitwardenItems(doc) }
        }
        if has(probe, "salt"), has(probe, "content"), !has(probe, "entries") {
            return .failure(source: "Proton Authenticator", .encrypted(.protonAuthenticator, "Proton Authenticator export is encrypted; in Proton Authenticator export again without a password"))
        }
        if let entries = raw("entries"), doc.kind(entries) == .array {
            return recognized("Proton Authenticator") { try parseProton(doc) }
        }
        if has(probe, "tokens"), has(probe, "tokenOrder") {
            return recognized("FreeOTP+") { try parseFreeOTPPlus(doc) }
        }
        if has(probe, "Authenticators") {
            return recognized("Stratum") { try parseStratum(doc) }
        }
        if let accounts = raw("accounts"), let elems = doc.arrayOfProbes(accounts),
           let first = elems.first, has(first, "vaults") {
            return recognized("1Password") { try parse1PUXData(doc) }
        }
        return nil
    }

    /// Probes the first element's keys: Raivo types its numbers as strings and
    /// uses "kind"; andOTP uses "type" plus "label".
    private static func parseJSONArrayExport(_ trimmed: [UInt8]) -> Outcome? {
        guard let doc = GoJSON(trimmed), doc.kind(doc.root) == .array,
              let elems = doc.arrayOfProbes(doc.root) else { return nil }
        guard let first = elems.first else {
            return recognized("Raivo") { [] }
        }
        if has(first, "kind") {
            return recognized("Raivo") { try parseRaivo(doc) }
        }
        if has(first, "type"), has(first, "label") {
            return recognized("andOTP") { try parseAndOTP(doc) }
        }
        return nil
    }

    // MARK: - Binary container magics

    /// Reports whether data begins with the zip local-file-header magic.
    static func isZip<D: Collection>(_ data: D) -> Bool where D.Element == UInt8 {
        data.starts(with: [0x50, 0x4b, 0x03, 0x04])
    }

    /// Reports the 16-byte ASCII magic of an encrypted Stratum / Authenticator
    /// Pro backup (all-caps is the current format, mixed case the legacy one).
    /// This check MUST run before the base32 setup-key rule: both spellings are
    /// 16 letters that decode cleanly as base32.
    static func isStratumEncrypted<D: Collection>(_ data: D) -> Bool where D.Element == UInt8 {
        data.starts(with: Array("AUTHENTICATORPRO".utf8)) ||
            data.starts(with: Array("AuthenticatorPro".utf8))
    }
}
