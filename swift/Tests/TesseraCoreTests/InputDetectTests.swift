import XCTest
import Foundation
@testable import TesseraCore

/// Ports the "Detection test table" in spec/otpauth.md verbatim. The shared
/// table is the byte-identical guarantee between the Swift `InputDetect` and the
/// Go `internal/detect` core: same input strings, same expected `InputKind`.
final class InputDetectTests: XCTestCase {

    private let migrationURI =
        "otpauth-migration://offline?data=CjEKCkhlbGxvId6tvu8SGEV4YW1wbGU6YWxpY2VAZ29vZ2xlLmNvbRoHRXhhbXBsZSABKAEwAhABGAEgACjr4JKkBg%3D%3D"
    private let raivoJSON =
        #"[{ "issuer": "GitHub", "account": "john@example.com", "secret": "JBSWY3DPEHPK3PXP", "algorithm": "SHA1", "digits": "6", "kind": "TOTP", "timer": "30", "counter": "0" }]"#
    private let twofasJSON =
        #"{ "schemaVersion": 4, "services": [{ "name": "GitHub", "secret": "JBSWY3DPEHPK3PXP", "otp": { "tokenType": "TOTP" } }] }"#
    private let aegisJSON =
        #"{ "version": 1, "db": { "version": 3, "entries": [] } }"#

    // MARK: - Detection table (classify)

    func testDetectionTable() {
        let cases: [(String, InputDetect.InputKind)] = [
            ("ZB573K4APD63E6RLD3WAHI3QFZ35RLEP", .setupKey),
            ("zb573k4a pd63e6rl d3wahi3q fz35rlep", .setupKey),
            ("zb573k4a-pd63e6rl-d3wahi3q-fz35rlep", .setupKey),
            ("\tZB573K4APD63E6RLD3WAHI3QFZ35RLEP\n", .setupKey),
            ("GEZDGNBV", .invalid),
            ("hello world", .invalid),
            ("otpauth://totp/Example:alice@google.com?secret=JBSWY3DPEHPK3PXP&issuer=Example", .otpauth),
            ("otpauth://hotp/Bank:ops?secret=JBSWY3DPEHPK3PXP&counter=5&digits=8", .otpauth),
            ("otpauth://steam/Steam:me?secret=ONSWG4TFOQ&digits=5", .otpauth),
            (migrationURI, .migration),
            (raivoJSON, .exportJSON),
            (twofasJSON, .exportJSON),
            (aegisJSON, .exportJSON),
            ("", .invalid),
            ("   \t  ", .invalid),
            ("SGVsbG8gd29ybGQhISE=", .invalid),
        ]
        for (input, want) in cases {
            XCTAssertEqual(InputDetect.classify(input), want, "classify(\(input))")
        }
    }

    func testSchemeIsCaseInsensitive() {
        XCTAssertEqual(InputDetect.classify("OTPAUTH://totp/A?secret=JBSWY3DPEHPK3PXP"), .otpauth)
        XCTAssertEqual(InputDetect.classify("OtpAuth-Migration://offline?data=AAAA"), .migration)
    }

    // MARK: - isLikelyBase32Secret guardrail

    func testGuardrail() {
        XCTAssertTrue(InputDetect.isLikelyBase32Secret("ZB573K4APD63E6RLD3WAHI3QFZ35RLEP"))
        XCTAssertTrue(InputDetect.isLikelyBase32Secret("zb573k4a pd63e6rl d3wahi3q fz35rlep"))
        XCTAssertTrue(InputDetect.isLikelyBase32Secret("zb573k4a-pd63e6rl-d3wahi3q-fz35rlep"))
        XCTAssertFalse(InputDetect.isLikelyBase32Secret("GEZDGNBV"))            // 8 < 16
        XCTAssertFalse(InputDetect.isLikelyBase32Secret("hello world"))         // 10 < 16
        XCTAssertFalse(InputDetect.isLikelyBase32Secret("SGVsbG8gd29ybGQhISE=")) // base64, not base32
    }

    // MARK: - parseText

    func testParseTextSetupKey() {
        let (accounts, errors) = InputDetect.parseText("ZB573K4APD63E6RLD3WAHI3QFZ35RLEP")
        XCTAssertTrue(errors.isEmpty)
        XCTAssertEqual(accounts.count, 1)
        let a = accounts[0]
        XCTAssertEqual(a.type, .totp)
        XCTAssertEqual(a.issuer, "")
        XCTAssertEqual(a.account, "")
        XCTAssertEqual(a.algorithm, "SHA1")
        XCTAssertEqual(a.digits, 6)
        XCTAssertEqual(a.period, 30)
        XCTAssertEqual(a.secret, try? Base32.decode("ZB573K4APD63E6RLD3WAHI3QFZ35RLEP"))
    }

    func testParseTextSpacedAndDashedKeysMatch() {
        let base = InputDetect.parseText("ZB573K4APD63E6RLD3WAHI3QFZ35RLEP").accounts.first?.secret
        let spaced = InputDetect.parseText("zb573k4a pd63e6rl d3wahi3q fz35rlep").accounts.first?.secret
        let dashed = InputDetect.parseText("zb573k4a-pd63e6rl-d3wahi3q-fz35rlep").accounts.first?.secret
        XCTAssertNotNil(base)
        XCTAssertEqual(base, spaced)
        XCTAssertEqual(base, dashed)
    }

    func testParseTextOTPAuth() {
        let (accounts, errors) = InputDetect.parseText(
            "otpauth://totp/Example:alice@google.com?secret=JBSWY3DPEHPK3PXP&issuer=Example")
        XCTAssertTrue(errors.isEmpty)
        XCTAssertEqual(accounts.count, 1)
        XCTAssertEqual(accounts[0].issuer, "Example")
        XCTAssertEqual(accounts[0].account, "alice@google.com")
    }

    func testParseTextMigration() {
        let (accounts, errors) = InputDetect.parseText(migrationURI)
        XCTAssertTrue(errors.isEmpty)
        XCTAssertEqual(accounts.count, 1)
        XCTAssertEqual(accounts[0].issuer, "Example")
        XCTAssertEqual(accounts[0].account, "alice@google.com")
    }

    func testParseTextExportJSON() {
        let (accounts, errors) = InputDetect.parseText(raivoJSON)
        XCTAssertTrue(errors.isEmpty)
        XCTAssertEqual(accounts.count, 1)
        XCTAssertEqual(accounts[0].issuer, "GitHub")
    }

    /// A pretty-printed (multiline) app export is one JSON blob, not per-line
    /// input: the leading '{' / '[' short-circuits whole-text JSON parsing.
    /// Mirrors Go detect.ParseText — 1 account, 0 errors, not N "Not recognized".
    func testParseTextMultilineJSONExport() {
        let pretty = """
        {
          "schemaVersion": 4,
          "services": [
            {
              "name": "GitHub",
              "secret": "JBSWY3DPEHPK3PXP",
              "otp": { "tokenType": "TOTP" }
            }
          ]
        }
        """
        let (accounts, errors) = InputDetect.parseText(pretty)
        XCTAssertEqual(accounts.count, 1)
        XCTAssertTrue(errors.isEmpty)
        XCTAssertEqual(accounts[0].issuer, "GitHub")
    }

    /// Spec row: three lines classify per-line as otpauth, invalid, setup-key.
    /// Two accounts import; the invalid line is recorded and never aborts.
    func testParseTextMixedMultiline() {
        let input = [
            "otpauth://totp/A?secret=JBSWY3DPEHPK3PXP",
            "hello world",
            "ZB573K4APD63E6RLD3WAHI3QFZ35RLEP",
        ].joined(separator: "\n")
        let (accounts, errors) = InputDetect.parseText(input)
        XCTAssertEqual(accounts.count, 2)
        XCTAssertEqual(errors.count, 1)
        XCTAssertEqual(errors[0].line, 2)
        XCTAssertEqual(errors[0].reason, "unrecognized input")
        XCTAssertEqual(errors[0].display, "invalid (11 characters)")
    }

    /// Spec row: leading/trailing whitespace is trimmed before every rule, on
    /// the per-line path as well as for a single payload.
    func testParseTextTrimsEachLine() {
        let input = "otpauth://totp/A?secret=JBSWY3DPEHPK3PXP\n\tZB573K4APD63E6RLD3WAHI3QFZ35RLEP  \n"
        let (accounts, errors) = InputDetect.parseText(input)
        XCTAssertTrue(errors.isEmpty, "\(errors)")
        XCTAssertEqual(accounts.count, 2)
    }

    func testParseTextWrappedURIRepair() {
        let wrapped = "otpauth://totp/Demo:reviewer@example.com?\nsecret=JBSWY3DPEHPK3PXP&issuer=Demo"
        let (accounts, errors) = InputDetect.parseText(wrapped)
        XCTAssertTrue(errors.isEmpty, "\(errors)")
        XCTAssertEqual(accounts.count, 1)
        XCTAssertEqual(accounts.first?.issuer, "Demo")
        XCTAssertEqual(accounts.first?.account, "reviewer@example.com")

        let broken = "otpauth://totp/A?\nhello world"
        let (a2, e2) = InputDetect.parseText(broken)
        XCTAssertTrue(a2.isEmpty)
        XCTAssertEqual(e2.count, 2)

        let batch = "otpauth://totp/A?secret=JBSWY3DPEHPK3PXP\notpauth://totp/B?secret=JBSWY3DPEHPK3PXP"
        let (a3, e3) = InputDetect.parseText(batch)
        XCTAssertEqual(a3.count, 2)
        XCTAssertTrue(e3.isEmpty)

        let uriPlusKey = "otpauth://totp/A?secret=JBSWY3DPEHPK3PXP\nZB573K4APD63E6RLD3WAHI3QFZ35RLEP"
        let (a4, e4) = InputDetect.parseText(uriPlusKey)
        XCTAssertEqual(a4.count, 2)
        XCTAssertTrue(e4.isEmpty)
    }

    func testParseTextEmpty() {
        for input in ["", " \n\t\r\n "] {
            let (accounts, errors) = InputDetect.parseText(input)
            XCTAssertTrue(accounts.isEmpty)
            XCTAssertEqual(errors, [InputDetect.ItemError(line: 1, display: "", reason: "no input", kind: .noInput)])
        }
    }

    func testParseTextRejectsOversizedInput() {
        let big = String(repeating: "a", count: Importers.maxInputBytes + 1)
        let (accounts, errors) = InputDetect.parseText(big)
        XCTAssertTrue(accounts.isEmpty)
        XCTAssertEqual(errors, [InputDetect.ItemError(line: 1, display: "", reason: "input is larger than 64 MiB",
                                                       kind: .tooLarge)])
    }

    func testParseTextExportBlobReportsSource() {
        let encrypted = #"{ "version": 1, "header": { "slots": [], "params": {} }, "db": "c2VhbGVk" }"#
        let (accounts, errors) = InputDetect.parseText(encrypted)
        XCTAssertTrue(accounts.isEmpty)
        XCTAssertEqual(errors.count, 1)
        XCTAssertEqual(errors.first?.line, 1)
        XCTAssertEqual(errors.first?.display, "export-json Aegi…")
        XCTAssertEqual(errors.first?.kind, .encryptedExport(.aegis))

        let unknown = #"{ "hello": "world" }"#
        let (a2, e2) = InputDetect.parseText(unknown)
        XCTAssertTrue(a2.isEmpty)
        XCTAssertEqual(e2, [InputDetect.ItemError(line: 1, display: "invalid { \"h…", reason: "unrecognized export",
                                                   kind: .unrecognizedExport)])
    }

    func testParseTextReportsEachBadLineAndKeepsTheRest() {
        let input = [
            "otpauth://totp/A?secret=JBSWY3DPEHPK3PXP",
            "otpauth://hotp/B?secret=JBSWY3DPEHPK3PXP",
            "otpauth://totp/C?secret=JBSWY3DPEHPK3PXP",
        ].joined(separator: "\n")
        let (accounts, errors) = InputDetect.parseText(input)
        XCTAssertEqual(accounts.map(\.account), ["A", "C"])
        XCTAssertEqual(errors.count, 1)
        XCTAssertEqual(errors.first?.line, 2)
        XCTAssertEqual(errors.first?.display, "otpauth (40 characters)")
        XCTAssertEqual(errors.first?.reason.hasPrefix("otpauth:"), true)
        XCTAssertEqual(errors.first?.kind, .invalidLink)
    }

    func testParseTextNamesEachItemErrorKind() {
        let input = [
            "otpauth-migration://offline?data=%%%",
            "not a code",
            #"{"encrypted":true,"items":[]}"#,
            #"{"encrypted":false,"items":[]}"#,
            #"{"hello":"world"}"#,
        ].joined(separator: "\n")
        let (accounts, errors) = InputDetect.parseText(input)
        XCTAssertTrue(accounts.isEmpty)
        XCTAssertEqual(errors.map(\.line), [1, 2, 3, 4, 5])
        XCTAssertEqual(errors.map(\.kind), [
            .invalidMigrationLink,
            .unrecognizedInput,
            .encryptedExport(.bitwarden),
            .malformedExport(.noEntries),
            .unrecognizedExport,
        ])
    }

    func testRedactShowsKindAndLengthForSecretShapes() {
        let cases: [(InputDetect.InputKind, String, String)] = [
            (.setupKey, "  ab  ", "setup-key (2 characters)"),
            (.otpauth, "otpauth://totp/A?secret=JBSWY3DPEHPK3PXP&digits=99", "otpauth (50 characters)"),
            (.migration, "otpauth-migration://offline?data=%%%", "migration (36 characters)"),
            (.migration, " \t", "migration"),
            (.invalid, "ZB573K4APD63E6R", "invalid (15 characters)"),
            (.invalid, "ZB57 3K4A-PD63=E6R1", "invalid (19 characters)"),
            (.invalid, "x", "invalid (1 character)"),
            (.invalid, "see https://example.com", "invalid (23 characters)"),
            (.invalid, "\u{feff}otpauth://totp/A?secret=JBSWY3DPEHPK3PXP", "invalid (41 characters)"),
            (.invalid, "my key: abcd", "invalid my k…"),
            (.invalid, "--==", "invalid --=="),
            (.invalid, "e\u{301}\u{301}xyz", "invalid e\u{301}\u{301}x…"),
            (.exportJSON, "Aegis", "export-json Aegi…"),
        ]
        for (kind, raw, want) in cases {
            XCTAssertEqual(Array(InputDetect.redact(kind, raw).unicodeScalars), Array(want.unicodeScalars), raw)
        }
    }

    func testParseTextNeverShowsTheStartOfAMistypedKey() {
        for line in ["ZB573K4APD63E6R", "ZB573K4APD63E6R1", "zb57 3k4a pd63 e6r8", "ZB573K4APD63E6RL="] {
            let (_, errors) = InputDetect.parseText(line)
            XCTAssertEqual(errors.count, 1, line)
            XCTAssertFalse(errors.contains { $0.display.contains("ZB57") || $0.display.contains("zb57") }, line)
        }
    }

    func testClassifyByteOrderMarkIsInvalid() {
        XCTAssertEqual(InputDetect.classify("\u{feff}otpauth://totp/A?secret=JBSWY3DPEHPK3PXP"), .invalid)
        XCTAssertEqual(InputDetect.classify("\u{feff}[]"), .invalid)
    }

    func testParseTextSkipsBlankLines() {
        let input = "\n\notpauth://totp/A?secret=JBSWY3DPEHPK3PXP\n\n"
        let (accounts, errors) = InputDetect.parseText(input)
        XCTAssertEqual(accounts.count, 1)
        XCTAssertTrue(errors.isEmpty)
    }

    // MARK: - redaction never leaks a full secret

    func testRedactionMasksURISecret() {
        let (_, errors) = InputDetect.parseText("otpauth://totp/A?secret=JBSWY3DPEHPK3PXP&digits=99")
        XCTAssertEqual(errors.count, 1)
        XCTAssertFalse(errors[0].display.contains("JBSWY3DPEHPK3PXP"))
    }

    func testRedactionMasksBareToken() {
        // 15-char base32-ish token: fails the >=16 guardrail, classifies invalid,
        // and must not render verbatim (it could be a typo'd secret).
        let typo = "ZB573K4APD63E6R"
        let (accounts, errors) = InputDetect.parseText(typo)
        XCTAssertTrue(accounts.isEmpty)
        XCTAssertEqual(errors.count, 1)
        XCTAssertFalse(errors[0].display.contains(typo))
    }

    // MARK: - Detection table: the new export containers

    func testDetectionTableExports() {
        let cases: [(String, InputDetect.InputKind)] = [
            (#"[{ "secret": "JBSWY3DPEHPK3PXP", "issuer": "GitHub", "label": "john@example.com", "digits": 6, "type": "TOTP", "algorithm": "SHA1", "period": 30, "tags": [] }]"#, .exportJSON),
            (#"{ "tokens": [], "tokenOrder": [] }"#, .exportJSON),
            (#"{ "Authenticators": [], "Categories": [] }"#, .exportJSON),
            (#"{ "encrypted": false, "items": [] }"#, .exportJSON),
            (#"{ "encrypted": false, "folders": [], "items": [] }"#, .exportJSON),
            (#"{ "encrypted": true, "passwordProtected": true, "salt": "c2FsdA==", "data": "ZGF0YQ==" }"#, .exportJSON),
            (#"{ "version": 1, "entries": [] }"#, .exportJSON),
            (#"{ "version": 1, "salt": "c2FsdA==", "content": "Y29udGVudA==" }"#, .exportJSON),
            (#"{ "version": 1, "encryptedData": "ZGF0YQ==", "encryptionNonce": "bm9uY2U=" }"#, .exportJSON),
            (#"{ "accounts": [ { "vaults": [] } ] }"#, .exportJSON),
            ("Title,URL,Username,Password,Notes,OTPAuth\nExample,https://e.com,alice@example.com,hunter2,,otpauth://totp/Example:alice@example.com?secret=JBSWY3DPEHPK3PXP", .exportCSV),
            ("Title,Url,Username,Password,OTPAuth,Favorite,Archived,Tags,Notes", .exportCSV),
            ("Title,URL,Username,Password,Notes", .invalid),
            ("PK\u{03}\u{04}\u{14}\u{00}", .exportBinary),
            ("AUTHENTICATORPRO\u{03}\u{04}", .exportBinary),
            ("AuthenticatorPro\u{03}\u{04}", .exportBinary),
        ]
        for (input, want) in cases {
            XCTAssertEqual(InputDetect.classify(input), want, "classify(\(input.prefix(48)))")
        }
    }

    /// The Stratum magic is 16 letters that decode cleanly as base32, so the
    /// binary rule must win over the setup-key guardrail.
    func testStratumMagicBeatsSetupKey() {
        XCTAssertTrue(InputDetect.isLikelyBase32Secret("AUTHENTICATORPRO"))
        XCTAssertEqual(InputDetect.classify("AUTHENTICATORPRO"), .exportBinary)
    }

    // MARK: - byte order: binary containers are classified before any UTF-8 decode

    func testNonUTF8BinaryContainersReachTheirMessages() {
        // Both blobs carry a byte no UTF-8 decoder accepts, so a text-first path
        // would lose these messages. AppModel.importDroppedFiles calls
        // Importers.parse on the raw Data first for exactly this reason.
        let invalidUTF8: [UInt8] = [0xff, 0xfe, 0x00, 0x80]

        let stratum = Data(Array("AUTHENTICATORPRO".utf8) + invalidUTF8)
        XCTAssertNil(String(data: stratum, encoding: .utf8))
        XCTAssertThrowsError(try Importers.parse(stratum)) { error in
            guard let e = error as? Importers.ImporterError, case .encrypted(.stratum, let m) = e else {
                XCTFail("expected .encrypted, got \(error)")
                return
            }
            XCTAssertEqual(m, "Stratum backup is encrypted; in Stratum choose Settings > Backup > Export unencrypted and try again")
        }

        let pux = Data([0x50, 0x4b, 0x03, 0x04] + invalidUTF8)
        XCTAssertNil(String(data: pux, encoding: .utf8))
        XCTAssertThrowsError(try Importers.parse(pux)) { error in
            guard let e = error as? Importers.ImporterError, case .malformed(.archive, let m) = e else {
                XCTFail("expected .malformed, got \(error)")
                return
            }
            XCTAssertEqual(m, "1password: open archive: zip: not a valid zip file")
        }
    }
}
