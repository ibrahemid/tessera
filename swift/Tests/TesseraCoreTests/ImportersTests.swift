import XCTest
import Foundation
@testable import TesseraCore

/// Ports go/internal/importers/importers_test.go: same fixtures, same behavior.
final class ImportersTests: XCTestCase {
    private let sampleSecret = "JBSWY3DPEHPK3PXP"

    private func wantSecret() throws -> Data { try Base32.decode(sampleSecret) }

    private let aegisPlain = """
    {
      "version": 1,
      "header": { "slots": null, "params": null },
      "db": {
        "version": 3,
        "entries": [
          { "type": "totp", "name": "john@example.com", "issuer": "GitHub",
            "info": { "secret": "JBSWY3DPEHPK3PXP", "algo": "SHA1", "digits": 6, "period": 30 } },
          { "type": "hotp", "name": "ops", "issuer": "Bank",
            "info": { "secret": "JBSWY3DPEHPK3PXP", "algo": "SHA256", "digits": 8, "counter": 5 } }
        ]
      }
    }
    """

    private let aegisEncrypted = #"{ "version": 1, "header": { "slots": [{}], "params": {} }, "db": "BASE64CIPHERTEXT==" }"#

    private let twofasPlain = """
    {
      "schemaVersion": 4,
      "services": [
        { "name": "GitHub", "secret": "JBSWY3DPEHPK3PXP",
          "otp": { "account": "john@example.com", "issuer": "GitHub", "digits": 6, "period": 30, "algorithm": "SHA1", "tokenType": "TOTP" } }
      ]
    }
    """

    private let twofasEncrypted = #"{ "schemaVersion": 4, "services": [], "servicesEncrypted": "deadbeef:cafe:1" }"#

    private let raivoPlain = """
    [
      { "issuer": "GitHub", "account": "john@example.com", "secret": "JBSWY3DPEHPK3PXP",
        "algorithm": "SHA1", "digits": "6", "kind": "TOTP", "timer": "30", "counter": "0" }
    ]
    """

    private func parse(_ s: String) throws -> (accounts: [Account], source: String)? {
        try Importers.parse(Data(s.utf8))
    }

    func testParseAegisPlain() throws {
        let result = try parse(aegisPlain)
        let unwrapped = try XCTUnwrap(result)
        XCTAssertEqual(unwrapped.source, "Aegis")
        XCTAssertEqual(unwrapped.accounts.count, 2)
        let a = unwrapped.accounts[0]
        XCTAssertEqual(a.type, .totp)
        XCTAssertEqual(a.issuer, "GitHub")
        XCTAssertEqual(a.account, "john@example.com")
        XCTAssertEqual(a.secret, try wantSecret())
        let b = unwrapped.accounts[1]
        XCTAssertEqual(b.type, .hotp)
        XCTAssertEqual(b.counter, 5)
        XCTAssertEqual(b.digits, 8)
        XCTAssertEqual(b.algorithm, "SHA256")
    }

    func testParse2FASPlain() throws {
        let unwrapped = try XCTUnwrap(try parse(twofasPlain))
        XCTAssertEqual(unwrapped.source, "2FAS")
        XCTAssertEqual(unwrapped.accounts.count, 1)
        let a = unwrapped.accounts[0]
        XCTAssertEqual(a.issuer, "GitHub")
        XCTAssertEqual(a.account, "john@example.com")
        XCTAssertEqual(a.period, 30)
        XCTAssertEqual(a.secret, try wantSecret())
    }

    func testParseRaivoPlain() throws {
        let unwrapped = try XCTUnwrap(try parse(raivoPlain))
        XCTAssertEqual(unwrapped.source, "Raivo")
        XCTAssertEqual(unwrapped.accounts.count, 1)
        XCTAssertEqual(unwrapped.accounts[0].period, 30)
        XCTAssertEqual(unwrapped.accounts[0].digits, 6)
    }

    func testEncryptedDetected() {
        for (name, data, source) in [
            ("aegis", aegisEncrypted, "Aegis"),
            ("2fas", twofasEncrypted, "2FAS"),
        ] {
            do {
                _ = try parse(data)
                XCTFail("\(name): expected encryption error, got success")
            } catch let e as Importers.ImporterError {
                guard case .encrypted = e else {
                    XCTFail("\(name): expected .encrypted, got \(e)")
                    continue
                }
                _ = source
            } catch {
                XCTFail("\(name): unexpected error \(error)")
            }
        }
    }

    func testUnsupportedTypeRejected() {
        let data = """
        {
          "db": { "entries": [
            { "type": "yandex", "name": "x", "issuer": "Yandex",
              "info": { "secret": "JBSWY3DPEHPK3PXP", "algo": "SHA1", "digits": 6, "period": 30 } }
          ] }
        }
        """
        XCTAssertThrowsError(try parse(data))
    }

    func testUnsupportedAlgoRejected() {
        let data = """
        {
          "db": { "entries": [
            { "type": "totp", "name": "x", "issuer": "Old",
              "info": { "secret": "JBSWY3DPEHPK3PXP", "algo": "MD5", "digits": 6, "period": 30 } }
          ] }
        }
        """
        XCTAssertThrowsError(try parse(data))
    }

    func testSteamImportForcesFiveDigits() throws {
        let data = """
        {
          "db": { "entries": [
            { "type": "steam", "name": "gabe", "issuer": "Steam",
              "info": { "secret": "JBSWY3DPEHPK3PXP", "algo": "SHA1", "digits": 5, "period": 30 } }
          ] }
        }
        """
        let unwrapped = try XCTUnwrap(try parse(data))
        XCTAssertEqual(unwrapped.accounts.count, 1)
        XCTAssertEqual(unwrapped.accounts[0].type, .steam)
        XCTAssertEqual(unwrapped.accounts[0].digits, 5)
    }

    func testRaivoErrorsSurface() {
        let cases = [
            "missing secret": #"[{ "issuer": "GitHub", "account": "x", "secret": "", "algorithm": "SHA1", "digits": "6", "kind": "TOTP", "timer": "30", "counter": "0" }]"#,
            "bad digits": #"[{ "issuer": "GitHub", "account": "x", "secret": "JBSWY3DPEHPK3PXP", "algorithm": "SHA1", "digits": "six", "kind": "TOTP", "timer": "30", "counter": "0" }]"#,
        ]
        for (name, data) in cases {
            XCTAssertThrowsError(try parse(data), name)
        }
    }

    func testUnrecognizedFallsThrough() throws {
        for data in [
            "otpauth://totp/GitHub:me?secret=JBSWY3DPEHPK3PXP",
            "",
            "   ",
            #"{"foo":"bar"}"#,
        ] {
            XCTAssertNil(try parse(data), "expected nil for \(data)")
        }
    }
}
