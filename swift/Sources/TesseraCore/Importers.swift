import Foundation

/// Parses plaintext (unencrypted) account exports from other authenticator apps
/// and password managers into the canonical `Account` model. The container
/// switch and the format ladders are the interop contract in
/// `/spec/otpauth.md` ("Input detection"); the Go `internal/importers` package
/// must match. Encrypted exports are detected and rejected with a clear error
/// rather than parsed wrong. Secrets are decoded to raw bytes here and never
/// logged.
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
    /// export is recognized but encrypted, malformed, or holds no OTP entries,
    /// so the caller surfaces the real reason instead of a generic parse
    /// failure.
    ///
    /// The container is classified from the raw bytes — an encrypted Stratum
    /// backup and a `.1pux` archive are not valid UTF-8, so a text-first path
    /// would never reach their messages.
    public static func parse(_ data: Data) throws -> (accounts: [Account], source: String)? {
        let trimmed = trimSpace(data)
        guard let first = trimmed.first else { return nil }
        switch first {
        case UInt8(ascii: "["):
            return try parseJSONArrayExport(trimmed)
        case UInt8(ascii: "{"):
            return try parseJSONObjectExport(trimmed)
        default:
            break
        }
        if let format = matchCSVHeader(data) {
            return try recognized(format.name, parseCSV(data, format))
        }
        if isZip(data) {
            // Foundation has no zip reader and TesseraCore takes no new
            // dependencies. The shared interop contract is the inner
            // export.data document, which both cores parse.
            throw ImporterError.malformed("1Password .1pux archives are read by the tess CLI; in the app, unzip it and import export.data")
        }
        if isStratumEncrypted(data) {
            throw ImporterError.encrypted("Stratum backup is encrypted; in Stratum choose Settings > Backup > Export unencrypted and try again")
        }
        return nil
    }

    /// Finishes one format branch. A recognized export that parsed cleanly but
    /// yielded no accounts is a failure, not an empty success: silent skipping
    /// of items without a second factor would otherwise make a password-manager
    /// export indistinguishable from an empty input.
    static func recognized(_ source: String, _ accounts: [Account]) throws -> (accounts: [Account], source: String) {
        if accounts.isEmpty {
            throw ImporterError.malformed("\(source) export has no one-time-password entries; only items with a 2FA code are imported")
        }
        return (accounts, source)
    }

    /// Walks the top-level key ladder of `/spec/otpauth.md`. Each app's
    /// encrypted probe precedes its plaintext probe.
    private static func parseJSONObjectExport(_ trimmed: Data) throws -> (accounts: [Account], source: String)? {
        guard let probe = try? JSONSerialization.jsonObject(with: trimmed) as? [String: Any] else {
            return nil
        }
        if probe["db"] != nil {
            return try recognized("Aegis", parseAegis(probe))
        }
        // Encrypted 2FAS backups carry BOTH an empty "services" array and the
        // ciphertext in "servicesEncrypted" — check the ciphertext first.
        if let enc = probe["servicesEncrypted"] as? String, !enc.isEmpty {
            throw ImporterError.encrypted("2FAS export is encrypted; in 2FAS turn off the backup password (or decrypt) and export again")
        }
        if probe["services"] != nil {
            return try recognized("2FAS", parse2FAS(probe))
        }
        if probe["encryptedData"] != nil, probe["encryptionNonce"] != nil {
            throw ImporterError.encrypted("Ente Auth export is encrypted; in Ente Auth choose Export > plain text (unencrypted) and try again")
        }
        if let encrypted = probe["encrypted"] as? Bool, encrypted {
            throw ImporterError.encrypted("Bitwarden export is encrypted; in Bitwarden export again as unencrypted .json (File format: .json, not 'Password protected')")
        }
        if let items = probe["items"] as? [Any] {
            // A password-manager export always carries folders or collections; an
            // authenticator export carries neither. A stripped export can collide,
            // which changes only the reported source: one walker parses both.
            let isPasswordManager = probe["folders"] != nil || probe["collections"] != nil
            let source = isPasswordManager ? "Bitwarden" : "Bitwarden Authenticator"
            return try recognized(source, parseBitwardenItems(items))
        }
        if probe["salt"] != nil, probe["content"] != nil, probe["entries"] == nil {
            throw ImporterError.encrypted("Proton Authenticator export is encrypted; in Proton Authenticator export again without a password")
        }
        if let entries = probe["entries"] as? [Any] {
            return try recognized("Proton Authenticator", parseProton(entries))
        }
        if probe["tokens"] != nil, probe["tokenOrder"] != nil {
            return try recognized("FreeOTP+", parseFreeOTPPlus(probe))
        }
        if probe["Authenticators"] != nil {
            return try recognized("Stratum", parseStratum(probe))
        }
        if let accounts = probe["accounts"] as? [[String: Any]], accounts.first?["vaults"] != nil {
            return try recognized("1Password", parse1PUXData(accounts))
        }
        return nil
    }

    /// Probes the first element's keys: Raivo types its numbers as strings and
    /// uses "kind"; andOTP uses "type" plus "label".
    private static func parseJSONArrayExport(_ trimmed: Data) throws -> (accounts: [Account], source: String)? {
        guard let elems = try? JSONSerialization.jsonObject(with: trimmed) as? [[String: Any]] else {
            return nil
        }
        guard let first = elems.first else {
            return try recognized("Raivo", [])
        }
        if first["kind"] != nil {
            return try recognized("Raivo", parseRaivo(elems))
        }
        if first["type"] != nil, first["label"] != nil {
            return try recognized("andOTP", parseAndOTP(elems))
        }
        return nil
    }

    // MARK: - Binary container magics

    /// Reports whether data begins with the zip local-file-header magic.
    static func isZip(_ data: Data) -> Bool {
        data.starts(with: [0x50, 0x4b, 0x03, 0x04])
    }

    /// Reports the 16-byte ASCII magic of an encrypted Stratum / Authenticator
    /// Pro backup (all-caps is the current format, mixed case the legacy one).
    /// This check MUST run before the base32 setup-key rule: both spellings are
    /// 16 letters that decode cleanly as base32.
    static func isStratumEncrypted(_ data: Data) -> Bool {
        data.starts(with: Array("AUTHENTICATORPRO".utf8)) ||
            data.starts(with: Array("AuthenticatorPro".utf8))
    }
}
