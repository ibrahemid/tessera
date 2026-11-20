import Foundation

/// Classifies a text payload and turns it into accounts. Mirrors the Go
/// `internal/detect` package and the precedence in `spec/otpauth.md` ("Input
/// detection") byte-for-byte, so a given input yields the same `InputKind`,
/// accounts and per-item errors in both cores. Used for pasted text, decoded
/// QR payloads, and text/JSON files.
public enum InputDetect {

    /// The classification of a single-payload (single-line) input.
    public enum InputKind: Sendable, Equatable {
        case migration
        case otpauth
        /// An app-export JSON blob (Aegis, 2FAS, Raivo, andOTP, FreeOTP+,
        /// Stratum, Bitwarden, Proton Authenticator, Ente Auth, 1PUX).
        case exportJSON
        /// An app-export CSV (Apple Passwords, 1Password).
        case exportCSV
        /// A binary app export: a 1Password .1pux archive or an encrypted
        /// Stratum backup.
        case exportBinary
        case setupKey
        case invalid

        /// The spec-canonical name of the kind.
        var name: String {
            switch self {
            case .migration: return "migration"
            case .otpauth: return "otpauth"
            case .exportJSON: return "export-json"
            case .exportCSV: return "export-csv"
            case .exportBinary: return "export-binary"
            case .setupKey: return "setup-key"
            case .invalid: return "invalid"
            }
        }

        var isExport: Bool { self == .exportJSON || self == .exportCSV || self == .exportBinary }
    }

    /// One item that failed to parse in a batch. Carries the line index, a
    /// redacted preview of the offending input (never a full secret), the
    /// Go core's reason text, and the kind of failure. Recorded per item so a
    /// batch never aborts.
    public struct ItemError: Error, Equatable, Sendable {
        /// 1-based line number within the input.
        public let line: Int
        /// Redacted, safe-to-show preview of the input: its kind and at most
        /// its first four characters.
        public let display: String
        /// Factual reason the item was not imported.
        public let reason: String
        /// What kind of failure `reason` describes.
        public let kind: Kind

        public init(line: Int, display: String, reason: String, kind: Kind) {
            self.line = line
            self.display = display
            self.reason = reason
            self.kind = kind
        }

        /// Why an item was not imported.
        public enum Kind: Sendable, Equatable {
            /// The input is larger than `Importers.maxInputBytes`.
            case tooLarge
            /// A recognized app export that is encrypted.
            case encryptedExport(Importers.EncryptedSource)
            /// A recognized app export that could not be imported.
            case malformedExport(Importers.MalformedKind)
            /// JSON, CSV or binary input that is no supported app's export.
            case unrecognizedExport
            /// An `otpauth-migration://` link that does not parse.
            case invalidMigrationLink
            /// An `otpauth://` link that does not parse.
            case invalidLink
            /// A line that is none of the supported inputs.
            case unrecognizedInput

            init(_ error: Importers.ImporterError) {
                switch error {
                case .encrypted(let source, _): self = .encryptedExport(source)
                case .malformed(let kind, _): self = .malformedExport(kind)
                }
            }
        }
    }

    private static let minSetupKeyLen = 16

    /// Classify a single payload by the first matching rule (see spec).
    public static func classify(_ input: String) -> InputKind {
        let trimmed = GoText.trimSpace(input)
        guard let first = trimmed.utf8.first else { return .invalid }
        let lower = GoText.toLower(trimmed)
        if GoText.hasPrefix(lower, "otpauth-migration://") { return .migration }
        if GoText.hasPrefix(lower, "otpauth://") { return .otpauth }
        if first == UInt8(ascii: "[") || first == UInt8(ascii: "{") { return .exportJSON }
        let bytes = Array(trimmed.utf8)
        if Importers.matchCSVHeader(bytes) != nil { return .exportCSV }
        // The binary magics must be checked before the setup-key guardrail: both
        // spellings of the Stratum header are 16 letters that decode as base32.
        if Importers.isZip(bytes) || Importers.isStratumEncrypted(bytes) { return .exportBinary }
        if isLikelyBase32Secret(trimmed) { return .setupKey }
        return .invalid
    }

    /// The base32 setup-key guardrail (spec rule 4). After stripping ASCII
    /// spaces and `-`, the input qualifies only if it is a single token matching
    /// `^[A-Za-z2-7]+$` (case-insensitive), at least 16 bytes, that decodes
    /// cleanly under the lenient base32 rules.
    public static func isLikelyBase32Secret(_ input: String) -> Bool {
        let clean = stripSpacesDashes(input)
        if clean.utf8.count < minSetupKeyLen { return false }
        for v in clean.unicodeScalars.map(\.value) {
            let isBase32 = (v >= 0x41 && v <= 0x5A) || (v >= 0x61 && v <= 0x7A) || (v >= 0x32 && v <= 0x37)
            if !isBase32 { return false }
        }
        return (try? Base32.decode(clean)) != nil
    }

    /// Parse a (possibly multiline) text payload into accounts, recording a
    /// per-item `ItemError` for anything that does not parse. Never throws;
    /// never aborts the batch. A blob that classifies as a whole-blob export
    /// is parsed as one export; otherwise each non-empty line is classified
    /// independently by the same precedence as `classify`.
    public static func parseText(_ input: String) -> (accounts: [Account], errors: [ItemError]) {
        if input.utf8.count > Importers.maxInputBytes {
            return ([], [ItemError(line: 1, display: "", reason: Importers.inputTooLarge, kind: .tooLarge)])
        }
        let trimmed = GoText.trimSpace(input)
        if trimmed.isEmpty { return ([], []) }
        let blobKind = classify(trimmed)
        if blobKind.isExport {
            switch Importers.detect(Array(input.utf8)) {
            case .success(let found)?:
                return (found.accounts, [])
            case .failure(let source, let error)?:
                return ([], [ItemError(line: 1, display: redact(blobKind, source), reason: error.description,
                                      kind: ItemError.Kind(error))])
            case nil:
                return ([], [ItemError(line: 1, display: redact(.invalid, trimmed), reason: "unrecognized export",
                                      kind: .unrecognizedExport)])
            }
        }

        // Wrapped-URI repair (spec § input detection): textareas and mail
        // clients hard-wrap long URIs, so a single URI with embedded line
        // breaks is one URI, not a batch. Applies only when the whole input
        // holds exactly one scheme and the first line alone is a true fragment
        // (fails to parse); a complete first line means the input is a batch.
        if trimmed.unicodeScalars.contains(where: { $0 == "\n" || $0 == "\r" }),
           blobKind == .otpauth || blobKind == .migration,
           countOccurrences(Array(GoText.toLower(trimmed).utf8), of: Array("otpauth".utf8)) == 1 {
            let scalars = trimmed.unicodeScalars
            let end = scalars.firstIndex { $0 == "\n" || $0 == "\r" } ?? scalars.endIndex
            let firstLine = GoText.trimSpace(String(scalars[..<end]))
            let joined = String(String.UnicodeScalarView(scalars.filter { !GoText.isSpace($0.value) }))
            if blobKind == .migration, (try? Migration.parse(firstLine)) == nil,
               let parsed = try? Migration.parse(joined) {
                return (parsed, [])
            }
            if blobKind == .otpauth, (try? OTPAuth.parse(firstLine)) == nil,
               let parsed = try? OTPAuth.parse(joined) {
                return ([parsed], [])
            }
        }

        var accounts: [Account] = []
        var errors: [ItemError] = []
        let lines = Array(input.utf8).split(separator: 0x0A, omittingEmptySubsequences: false)
        for (i, raw) in lines.enumerated() {
            let line = GoText.trimSpace(GoText.string(raw))
            if line.isEmpty { continue }
            let lineNo = i + 1
            let kind = classify(line)
            switch kind {
            case .migration:
                do { accounts.append(contentsOf: try Migration.parse(line)) }
                catch {
                    errors.append(ItemError(line: lineNo, display: redact(kind, line), reason: Importers.message(error),
                                            kind: .invalidMigrationLink))
                }
            case .otpauth:
                do { accounts.append(try OTPAuth.parse(line)) }
                catch {
                    errors.append(ItemError(line: lineNo, display: redact(kind, line), reason: Importers.message(error),
                                            kind: .invalidLink))
                }
            case .exportJSON, .exportCSV, .exportBinary:
                switch Importers.detect(Array(line.utf8)) {
                case .success(let found)?:
                    accounts.append(contentsOf: found.accounts)
                case .failure(let source, let error)?:
                    errors.append(ItemError(line: lineNo, display: redact(kind, source), reason: error.description,
                                            kind: ItemError.Kind(error)))
                case nil:
                    errors.append(ItemError(line: lineNo, display: redact(.invalid, line), reason: "unrecognized export",
                                            kind: .unrecognizedExport))
                }
            case .setupKey:
                accounts.append(setupKeyAccount(line))
            case .invalid:
                errors.append(ItemError(line: lineNo, display: redact(.invalid, line), reason: "unrecognized input",
                                        kind: .unrecognizedInput))
            }
        }
        return (accounts, errors)
    }

    // MARK: - Helpers

    /// A safe display form for a payload: its kind and its first four
    /// characters, so an error can name the item without exposing the secret.
    static func redact(_ kind: InputKind, _ raw: String) -> String {
        let scalars = Array(GoText.trimSpace(raw).unicodeScalars)
        var prefix = String(String.UnicodeScalarView(scalars.prefix(4)))
        if scalars.count > 4 { prefix += "…" }
        return prefix.isEmpty ? kind.name : kind.name + " " + prefix
    }

    /// A TOTP account from a bare base32 setup key with the spec defaults. The
    /// key already passed `isLikelyBase32Secret`, so the decode cannot fail.
    private static func setupKeyAccount(_ key: String) -> Account {
        let secret = (try? Base32.decode(stripSpacesDashes(key))) ?? Data()
        return Account(id: "", type: .totp, issuer: "", account: "",
                       secret: secret, algorithm: "SHA1", digits: 6, period: 30)
    }

    private static func stripSpacesDashes(_ s: String) -> String {
        String(String.UnicodeScalarView(s.unicodeScalars.filter { $0 != " " && $0 != "-" }))
    }

    /// Go `strings.Count`: non-overlapping occurrences of a non-empty needle.
    private static func countOccurrences(_ hay: [UInt8], of needle: [UInt8]) -> Int {
        var count = 0
        var i = 0
        while i + needle.count <= hay.count {
            if hay[i..<(i + needle.count)].elementsEqual(needle) {
                count += 1
                i += needle.count
            } else {
                i += 1
            }
        }
        return count
    }
}
