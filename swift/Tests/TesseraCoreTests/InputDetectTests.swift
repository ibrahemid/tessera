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
        XCTAssertEqual(errors[0].reason, "Not recognized")
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
        let (accounts, errors) = InputDetect.parseText("")
        XCTAssertTrue(accounts.isEmpty)
        XCTAssertTrue(errors.isEmpty)
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
}
