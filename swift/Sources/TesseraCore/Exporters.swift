#if canImport(CryptoKit)
import CryptoKit
#else
import Crypto
#endif
import Foundation

/// Renders vault accounts into other applications' export formats, byte for
/// byte as the Go `exporters` package does. Rendering is pure: files are built
/// in memory and nothing is written. Rendered files carry secrets; a skip
/// label or an error never does.
public enum Exporters {

    /// An export format, in the order the CLI lists them.
    public enum Format: String, CaseIterable, Sendable {
        case twoFAS = "2fas"
        case aegis
        case andOTP = "andotp"
        case appleCSV = "apple-csv"
        case bitwarden
        case googleMigration = "google-migration"
        case proton

        /// The `--format` value of `tess export`.
        public var name: String { rawValue }

        /// Whether one export of this format can render more than one file.
        /// It is a property of the format: a batching format still renders a
        /// directory's worth of files for a small vault.
        public var isMultiFile: Bool { self == .googleMigration }

        /// The format registered under `name`, compared after trimming space
        /// and lowercasing as `exporters.Lookup` does.
        public static func lookup(_ name: String) -> Format? {
            let key = Array(GoText.toLower(GoText.trimSpace(name)).utf8)
            return allCases.first { Array($0.rawValue.utf8) == key }
        }
    }

    /// Why a format left an account, or one field of it, out. The raw value
    /// is the Go `SkipKind` and never changes with the reason's wording.
    public enum SkipKind: String, CaseIterable, Sendable {
        /// The format has no entry for the account's type.
        case unsupportedType = "unsupported_type"
        /// The format cannot store the account's algorithm.
        case unsupportedAlgorithm = "unsupported_algorithm"
        /// The format cannot store the account's digit count.
        case unsupportedDigits = "unsupported_digits"
        /// The format cannot store the account's period.
        case unsupportedPeriod = "unsupported_period"
        /// The issuer or account name starts like a spreadsheet formula, so the
        /// whole account is left out.
        case formulaName = "formula_name"
        /// The note starts like a spreadsheet formula, so the account is written
        /// without its note.
        case formulaNote = "formula_note"
    }

    /// One account, or one field of it, a format cannot carry. `label` never
    /// contains a secret; `reason` is the Go text a person reads.
    public struct Skipped: Equatable, Sendable {
        public let label: String
        public let reason: String
        public let kind: SkipKind

        public init(label: String, reason: String, kind: SkipKind) {
            self.label = label
            self.reason = reason
            self.kind = kind
        }
    }

    /// One rendered file. `name` is a base name, never a path.
    public struct File: Equatable, Sendable {
        public let name: String
        public let data: Data

        public init(name: String, data: Data) {
            self.name = name
            self.data = data
        }
    }

    /// One Google Authenticator transfer QR: the PNG file name the CLI gives it
    /// and the `otpauth-migration://` link the image encodes.
    public struct TransferCode: Equatable, Sendable {
        public let name: String
        public let uri: String

        public init(name: String, uri: String) {
            self.name = name
            self.uri = uri
        }
    }

    /// What one export produced: files to save, or transfer links to show as
    /// QR codes. Either list is empty when every account was skipped.
    public enum Output: Equatable, Sendable {
        case files([File])
        case transferCodes([TransferCode])
    }

    /// The result of rendering one format.
    public struct Rendered: Equatable, Sendable {
        public let format: Format
        public let output: Output
        public let skipped: [Skipped]

        public init(format: Format, output: Output, skipped: [Skipped]) {
            self.format = format
            self.output = output
            self.skipped = skipped
        }
    }

    /// A rendering failure. Carries no account data.
    public enum ExportError: Error, Equatable, Sendable, CustomStringConvertible, LocalizedError {
        /// The transfer encoder skipped a different set of accounts than the
        /// google-migration skip rules name.
        case transferSkipsDisagree

        public var description: String {
            switch self {
            case .transferSkipsDisagree:
                return "google-migration: the transfer encoder and the skip rules disagree on which accounts to leave out"
            }
        }

        public var errorDescription: String? { description }
    }

    /// Renders `accounts`, in vault order, as `format`. `batchID` is the id
    /// every transfer batch of a google-migration export shares; the other
    /// formats ignore it.
    public static func render(_ format: Format, _ accounts: [Account],
                              batchID: Int32 = Int32.random(in: .min ... .max)) throws -> Rendered {
        switch format {
        case .twoFAS:
            return Rendered(format: format, output: .files([renderTwoFAS(accounts)]), skipped: [])
        case .aegis:
            return Rendered(format: format, output: .files([renderAegis(accounts)]), skipped: [])
        case .andOTP:
            return Rendered(format: format, output: .files([renderAndOTP(accounts)]), skipped: [])
        case .appleCSV:
            let (file, skipped) = renderAppleCSV(accounts)
            return Rendered(format: format, output: .files(file.map { [$0] } ?? []), skipped: skipped)
        case .bitwarden:
            return Rendered(format: format, output: .files([renderBitwarden(accounts)]), skipped: [])
        case .googleMigration:
            let (codes, skipped) = try renderTransferCodes(accounts, batchID: batchID)
            return Rendered(format: format, output: .transferCodes(codes), skipped: skipped)
        case .proton:
            let (file, skipped) = renderProton(accounts)
            return Rendered(format: format, output: .files([file]), skipped: skipped)
        }
    }

    // MARK: - Shared naming

    static func label(_ a: Account) -> String {
        switch (a.issuer.isEmpty, a.account.isEmpty) {
        case (false, false): return a.issuer + " (" + a.account + ")"
        case (false, true): return a.issuer
        case (true, false): return a.account
        case (true, true): return "unnamed account"
        }
    }

    static func displayName(_ a: Account) -> String {
        a.issuer.isEmpty ? a.account : a.issuer
    }

    static func otpauthLabel(_ a: Account) -> String {
        a.issuer.isEmpty ? a.account : a.issuer + ":" + a.account
    }

    static func tokenType(_ t: OTPType) -> String {
        GoText.toUpper(t.rawValue)
    }

    /// The id or uuid field derived from a vault id: stable across exports and
    /// well formed for any id.
    static func exportID(_ id: String) -> String { hashedID("tessera-export-id:" + id) }

    /// The group or folder id derived from a folder name.
    static func folderID(_ name: String) -> String { hashedID("tessera-export-folder:" + name) }

    /// The first 16 bytes of SHA-256(s) in lowercase hex, dashed 8-4-4-4-12.
    static func hashedID(_ s: String) -> String {
        let sum = Array(SHA256.hash(data: Data(s.utf8)))
        let hex = Array("0123456789abcdef".utf8)
        var out: [UInt8] = []
        out.reserveCapacity(36)
        for (i, b) in sum.prefix(16).enumerated() {
            if i == 4 || i == 6 || i == 8 || i == 10 { out.append(0x2D) }
            out.append(hex[Int(b >> 4)])
            out.append(hex[Int(b & 0x0F)])
        }
        return String(decoding: out, as: UTF8.self)
    }

    struct Folder {
        let id: String
        let name: String
        var lastUpdatedAt: Int64
    }

    /// The distinct non-empty folders of `accounts` in order of first
    /// appearance, each with the latest `updatedAt` of its accounts.
    static func folders(_ accounts: [Account]) -> [Folder] {
        var out: [Folder] = []
        var at: [[UInt8]: Int] = [:]
        for a in accounts where !a.folder.isEmpty {
            let key = Array(a.folder.utf8)
            let i: Int
            if let found = at[key] {
                i = found
            } else {
                i = out.count
                at[key] = i
                out.append(Folder(id: folderID(a.folder), name: a.folder, lastUpdatedAt: 0))
            }
            out[i].lastUpdatedAt = max(out[i].lastUpdatedAt, a.updatedAt)
        }
        return out
    }

    static func optionalFolderID(_ a: Account) -> ExportJSON {
        a.folder.isEmpty ? .null : .string(folderID(a.folder))
    }

    static func optionalString(_ s: String) -> ExportJSON {
        s.isEmpty ? .null : .string(s)
    }

    static func secretText(_ a: Account) -> String {
        Base32.encodeNoPad(a.secret)
    }
}

/// A JSON value written the way Go's `json.MarshalIndent(v, "", "  ")` writes a
/// struct: members in declaration order, HTML characters escaped.
enum ExportJSON {
    case object([(String, ExportJSON)])
    case array([ExportJSON])
    case string(String)
    case int(Int64)
    case bool(Bool)
    case null

    static func int(_ v: Int) -> ExportJSON { .int(Int64(v)) }

    /// The indented document with the trailing newline an export file ends with.
    var document: Data {
        var compact: [UInt8] = []
        append(to: &compact)
        var out = StrictJSON.indent(compact)
        out.append(0x0A)
        return Data(out)
    }

    func append(to out: inout [UInt8]) {
        switch self {
        case .object(let members):
            out.append(0x7B)
            for (i, m) in members.enumerated() {
                if i > 0 { out.append(0x2C) }
                Self.appendString(m.0, to: &out)
                out.append(0x3A)
                m.1.append(to: &out)
            }
            out.append(0x7D)
        case .array(let items):
            out.append(0x5B)
            for (i, item) in items.enumerated() {
                if i > 0 { out.append(0x2C) }
                item.append(to: &out)
            }
            out.append(0x5D)
        case .string(let s):
            Self.appendString(s, to: &out)
        case .int(let v):
            out.append(contentsOf: Array(String(v).utf8))
        case .bool(let v):
            out.append(contentsOf: Array((v ? "true" : "false").utf8))
        case .null:
            out.append(contentsOf: Array("null".utf8))
        }
    }

    /// Go's string encoding with HTML escaping on: the HTML-off encoding, then
    /// `<`, `>` and `&`, which that encoding only ever copies from the content.
    static func appendString(_ s: String, to out: inout [UInt8]) {
        for c in StrictJSON.quoted(s) {
            switch c {
            case 0x3C: out.append(contentsOf: Array("\\u003c".utf8))
            case 0x3E: out.append(contentsOf: Array("\\u003e".utf8))
            case 0x26: out.append(contentsOf: Array("\\u0026".utf8))
            default: out.append(c)
            }
        }
    }
}
