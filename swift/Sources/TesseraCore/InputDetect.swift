import Foundation

/// Classifies a text payload and turns it into accounts. Mirrors the Go
/// `internal/detect` package and the precedence in `spec/otpauth.md` ("Input
/// detection") byte-for-byte, so a given input yields the same `InputKind` in
/// both cores. Used for pasted text, decoded QR payloads, and text/JSON files.
public enum InputDetect {

    /// The classification of a single-payload (single-line) input.
    public enum InputKind: Sendable, Equatable {
        case migration
        case otpauth
        case exportJSON
        case setupKey
        case invalid
    }

    /// One item that failed to parse in a batch. Carries the line index, a
    /// redacted preview of the offending input (never a full secret), and a
    /// factual reason. Recorded per item so a batch never aborts.
    public struct ItemError: Error, Equatable, Sendable {
        /// 1-based line number within the input.
        public let line: Int
        /// Redacted, safe-to-show preview of the input.
        public let display: String
        /// Factual reason the item was not imported.
        public let reason: String

        public init(line: Int, display: String, reason: String) {
            self.line = line
            self.display = display
            self.reason = reason
        }
    }

    /// Classify a single payload by the first matching rule (see spec).
    public static func classify(_ input: String) -> InputKind {
        let trimmed = input.trimmingCharacters(in: .whitespacesAndNewlines)
        if trimmed.isEmpty { return .invalid }
        let lower = trimmed.lowercased()
        if lower.hasPrefix("otpauth-migration://") { return .migration }
        if lower.hasPrefix("otpauth://") { return .otpauth }
        if let first = trimmed.first, first == "[" || first == "{" { return .exportJSON }
        if isLikelyBase32Secret(input) { return .setupKey }
        return .invalid
    }

    /// The base32 setup-key guardrail (spec rule 4). After stripping ASCII
    /// spaces and `-`, the input qualifies only if it is a single token matching
    /// `^[A-Za-z2-7]+$` (case-insensitive), length >= 16, that decodes cleanly
    /// under the lenient base32 rules.
    public static func isLikelyBase32Secret(_ input: String) -> Bool {
        var stripped = ""
        stripped.reserveCapacity(input.count)
        for ch in input {
            if ch == " " || ch == "-" { continue }
            stripped.append(ch)
        }
        if stripped.count < 16 { return false }
        for ch in stripped {
            let isBase32 = (ch >= "A" && ch <= "Z") || (ch >= "a" && ch <= "z")
                || (ch >= "2" && ch <= "7")
            if !isBase32 { return false }
        }
        guard let decoded = try? Base32.decode(stripped), !decoded.isEmpty else { return false }
        return true
    }

    /// Parse a (possibly multiline) text payload into accounts, recording a
    /// per-line `ItemError` for anything that does not parse. Never throws;
    /// never aborts the batch. Each non-empty line is classified independently
    /// by the same precedence as `classify`.
    public static func parseText(_ input: String) -> (accounts: [Account], errors: [ItemError]) {
        var accounts: [Account] = []
        var errors: [ItemError] = []

        // A blob whose first non-whitespace byte is '[' or '{' is one JSON export
        // (app exports are legitimately multiline), not per-line input. Matches Go
        // detect.ParseText so a pretty-printed Aegis/2FAS/Raivo paste imports here
        // exactly as it does in the CLI.
        let trimmed = input.trimmingCharacters(in: .whitespacesAndNewlines)
        if let first = trimmed.first, first == "[" || first == "{" {
            do {
                if let result = try Importers.parse(Data(input.utf8)) {
                    return (result.accounts, [])
                }
                return ([], [ItemError(line: 1, display: jsonPreview(trimmed), reason: "Not recognized")])
            } catch {
                return ([], [ItemError(line: 1, display: jsonPreview(trimmed), reason: reason(error))])
            }
        }

        // Wrapped-URI repair (spec § input detection): textareas and mail
        // clients hard-wrap long URIs, so a single URI with embedded line
        // breaks is one URI, not a batch. Applies only when the whole input
        // holds exactly one scheme and the first line alone is a true fragment
        // (fails to parse); a complete first line means the input is a batch.
        // Matches Go detect.ParseText.
        if trimmed.contains(where: \.isNewline) {
            let kind = classify(trimmed)
            if kind == .otpauth || kind == .migration,
               trimmed.lowercased().components(separatedBy: "otpauth").count == 2 {
                let firstLine = trimmed.split(whereSeparator: \.isNewline)
                    .first.map { String($0).trimmingCharacters(in: .whitespaces) } ?? ""
                let joined = String(String.UnicodeScalarView(
                    trimmed.unicodeScalars.filter { !CharacterSet.whitespacesAndNewlines.contains($0) }))
                if kind == .migration, (try? Migration.parse(firstLine)) == nil,
                   let parsed = try? Migration.parse(joined) {
                    return (parsed, [])
                }
                if kind == .otpauth, (try? OTPAuth.parse(firstLine)) == nil,
                   let parsed = try? OTPAuth.parse(joined) {
                    return ([parsed], [])
                }
            }
        }

        let lines = input.split(omittingEmptySubsequences: false, whereSeparator: \.isNewline)
        for (i, raw) in lines.enumerated() {
            let line = String(raw)
            if line.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty { continue }
            let lineNo = i + 1
            switch classify(line) {
            case .migration:
                do { accounts.append(contentsOf: try Migration.parse(line)) }
                catch { errors.append(item(lineNo, line, error)) }
            case .otpauth:
                do { accounts.append(try OTPAuth.parse(line)) }
                catch { errors.append(item(lineNo, line, error)) }
            case .exportJSON:
                do {
                    if let result = try Importers.parse(Data(line.utf8)) {
                        accounts.append(contentsOf: result.accounts)
                    } else {
                        errors.append(ItemError(line: lineNo, display: redact(line), reason: "Not recognized"))
                    }
                } catch { errors.append(item(lineNo, line, error)) }
            case .setupKey:
                do {
                    let secret = try Base32.decode(line)
                    accounts.append(Account(id: "", type: .totp, issuer: "", account: "",
                                            secret: secret, algorithm: "SHA1", digits: 6, period: 30))
                } catch { errors.append(item(lineNo, line, error)) }
            case .invalid:
                errors.append(ItemError(line: lineNo, display: redact(line), reason: "Not recognized"))
            }
        }
        return (accounts, errors)
    }

    // MARK: - Redaction

    private static func item(_ line: Int, _ text: String, _ error: Error) -> ItemError {
        ItemError(line: line, display: redact(text), reason: reason(error))
    }

    /// A short, secret-safe preview of a JSON export blob for an error line. A
    /// recognized-but-malformed export can hold real secrets deeper in the JSON,
    /// so only a short leading prefix is shown.
    private static func jsonPreview(_ trimmed: String) -> String {
        trimmed.count > 24 ? String(trimmed.prefix(24)) + "…" : trimmed
    }

    private static func reason(_ error: Error) -> String {
        if let e = error as? Importers.ImporterError { return e.description }
        if let e = error as? AccountError { return e.description }
        return "\(error)"
    }

    /// Produce a preview of an input line that never exposes a full secret:
    /// otpauth `secret=`/migration `data=` values are masked, bare base32/base64
    /// tokens are truncated to a short prefix, and the whole preview is capped.
    static func redact(_ line: String) -> String {
        var s = line.trimmingCharacters(in: .whitespacesAndNewlines)
        s = maskParam(s, "secret")
        s = maskParam(s, "data")
        let masked = s.split(separator: " ", omittingEmptySubsequences: false)
            .map { looksLikeSecret($0) ? String($0.prefix(2)) + "…" : String($0) }
            .joined(separator: " ")
        if masked.count > 60 { return String(masked.prefix(60)) + "…" }
        return masked
    }

    /// Mask the value of a URI query parameter (`name=value`) up to the next
    /// `&` or end of string.
    private static func maskParam(_ s: String, _ name: String) -> String {
        guard let range = s.range(of: "\(name)=", options: .caseInsensitive) else { return s }
        var end = range.upperBound
        while end < s.endIndex, s[end] != "&" { end = s.index(after: end) }
        if end == range.upperBound { return s }
        return s.replacingCharacters(in: range.upperBound..<end, with: "…")
    }

    /// A bare token that could be a typed secret: long and made only of
    /// base32/base64 key characters (no URI punctuation).
    private static func looksLikeSecret(_ token: Substring) -> Bool {
        if token.count < 12 { return false }
        for ch in token {
            let ok = ch.isLetter || ch.isNumber || ch == "=" || ch == "+" || ch == "/" || ch == "_"
            if !ok { return false }
        }
        return true
    }
}
