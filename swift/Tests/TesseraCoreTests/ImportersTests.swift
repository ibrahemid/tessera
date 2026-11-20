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

    /// A recognized export that parses cleanly but holds no OTP entries fails,
    /// naming the app: silent skipping of items without a second factor would
    /// otherwise make it indistinguishable from an empty input.
    func testEmptyRecognizedExportFails() {
        XCTAssertThrowsError(try parse(#"{ "encrypted": false, "items": [] }"#)) { error in
            XCTAssertEqual("\(error)",
                           "Bitwarden Authenticator export has no one-time-password entries; only items with a 2FA code are imported")
        }
    }

    // MARK: - shared fixture table

    /// Runs go/internal/importers/testdata/expected.json, the same table the Go
    /// TestTestdataTable runs. Rows whose "cores" omit "swift" are skipped: the
    /// .1pux zip container is a CLI-only transport.
    func testTestdataTable() throws {
        let entries = try importerFixtures()
        XCTAssertFalse(entries.isEmpty, "expected.json is empty")
        let dir = importerTestdataDir()
        for entry in entries {
            let file = entry["file"] as! String
            let cores = entry["cores"] as? [String] ?? ["go", "swift"]
            if !cores.contains("swift") { continue }
            let data = try Data(contentsOf: dir.appendingPathComponent(file))
            let via = entry["via"] as? String ?? "importers"

            var accounts: [Account] = []
            var source = ""
            var thrown: Error?
            switch via {
            case "importers":
                var found: (accounts: [Account], source: String)?
                do { found = try Importers.parse(data) } catch { thrown = error }
                if thrown == nil {
                    guard let found else {
                        XCTFail("\(file): Importers.parse did not recognize the fixture as an export")
                        continue
                    }
                    accounts = found.accounts
                    source = found.source
                }
            case "detect":
                let text = try XCTUnwrap(String(data: data, encoding: .utf8), "\(file): not UTF-8")
                let (accs, errs) = InputDetect.parseText(text)
                accounts = accs
                if let first = errs.first {
                    thrown = Importers.ImporterError.malformed(first.reason)
                }
            default:
                XCTFail("\(file): unknown via \"\(via)\"")
                continue
            }

            if let reject = entry["reject"] as? String {
                guard let thrown else {
                    XCTFail("\(file): expected the export to be rejected (\(reject)), got \(accounts.count) accounts")
                    continue
                }
                let want = entry["message_contains"] as? String ?? ""
                XCTAssertTrue("\(thrown)".contains(want), "\(file): error \"\(thrown)\" does not contain \"\(want)\"")
                continue
            }
            if let thrown {
                XCTFail("\(file): parse: \(thrown)")
                continue
            }
            if let wantSource = entry["source"] as? String {
                XCTAssertEqual(source, wantSource, "\(file): source")
            }
            try assertAccounts(file, accounts, entry["accounts"] as! [[String: Any]])
        }
    }

    private func assertAccounts(_ file: String, _ got: [Account], _ want: [[String: Any]]) throws {
        guard got.count == want.count else {
            XCTFail("\(file): got \(got.count) accounts, want \(want.count)")
            return
        }
        for (i, w) in want.enumerated() {
            let a = got[i]
            let at = "\(file) account \(i)"
            XCTAssertEqual(a.type.rawValue, w["type"] as! String, "\(at): type")
            XCTAssertEqual(a.issuer, w["issuer"] as! String, "\(at): issuer")
            XCTAssertEqual(a.account, w["account"] as! String, "\(at): account")
            XCTAssertEqual(a.secret, try Base32.decode(w["secret_b32"] as! String), "\(at): secret")
            XCTAssertEqual(a.algorithm, w["algorithm"] as! String, "\(at): algorithm")
            XCTAssertEqual(a.digits, (w["digits"] as! NSNumber).intValue, "\(at): digits")
            XCTAssertEqual(a.period, (w["period"] as! NSNumber).intValue, "\(at): period")
            XCTAssertEqual(a.counter, (w["counter"] as! NSNumber).int64Value, "\(at): counter")
            XCTAssertEqual(Array(a.note.utf8), Array((w["note"] as? String ?? "").utf8), "\(at): note")
        }
    }

    // MARK: - text cleaning and notes

    func testImportersCleanText() throws {
        let cases: [(String, String, String, String)] = [
            ("aegis",
             #"{"version":1,"header":{},"db":{"version":3,"entries":[{"type":"totp","name":"\ufeffjohn\u200d\u0007","issuer":" \u202eGit\u0085Hub\u2069 ","info":{"secret":"JBSWY3DPEHPK3PXP","algo":"SHA1","digits":6,"period":30}}]}}"#,
             "GitHub", "john\u{200d}"),
            ("bitwarden setup key",
             #"{"folders":[],"items":[{"name":"\u202eBank\u0000","login":{"username":" ops\u001b","totp":"JBSWY3DPEHPK3PXP"}}]}"#,
             "Bank", "ops"),
            ("bitwarden steam fallback",
             #"{"folders":[],"items":[{"name":"Valve\u2066","login":{"username":"\ufeff ","totp":"steam://JBSWY3DPEHPK3PXP"}}]}"#,
             "Steam", "Valve"),
            ("apple csv",
             "Title,URL,Username,Password,Notes,OTPAuth\n\"\u{202e}Example\",https://e.com,\"alice\u{7}\",pw,,JBSWY3DPEHPK3PXP\n",
             "Example", "alice"),
        ]
        for (name, data, issuer, account) in cases {
            let found = try XCTUnwrap(try parse(data), name)
            XCTAssertEqual(found.accounts.map { Array($0.issuer.utf8) }, [Array(issuer.utf8)], name)
            XCTAssertEqual(found.accounts.map { Array($0.account.utf8) }, [Array(account.utf8)], name)
        }
    }

    func testImportersMapNote() throws {
        let raw = "line1\r\nline2\rline3\u{1b}[0m\tend\u{202e} "
        let want = "line1\nline2\nline3[0m\tend"
        let jsonRaw = #"line1\r\nline2\rline3\u001b[0m\tend\u202e "#
        let cases: [(String, String)] = [
            ("aegis",
             #"{"version":1,"header":{},"db":{"version":3,"entries":[{"type":"totp","name":"a","issuer":"b","note":"@@N@@","info":{"secret":"JBSWY3DPEHPK3PXP"}},{"type":"totp","name":"c","issuer":"d","info":{"secret":"JBSWY3DPEHPK3PXP"}}]}}"#),
            ("bitwarden",
             #"{"folders":[],"items":[{"name":"a","notes":"@@N@@","login":{"totp":"JBSWY3DPEHPK3PXP"}},{"name":"b","notes":null,"login":{"totp":"JBSWY3DPEHPK3PXP"}}]}"#),
            ("proton",
             #"{"version":1,"entries":[{"note":"@@N@@","content":{"uri":"otpauth://totp/a:b?secret=JBSWY3DPEHPK3PXP","entry_type":"Totp","name":"a"}},{"note":null,"content":{"uri":"otpauth://totp/c:d?secret=JBSWY3DPEHPK3PXP","entry_type":"Totp","name":"c"}}]}"#),
            ("apple csv",
             "Title,URL,Username,Password,Notes,OTPAuth\na,,b,,\"" + raw + "\",JBSWY3DPEHPK3PXP\nc,,d,,,JBSWY3DPEHPK3PXP\n"),
            ("1password csv",
             "Title,Url,Username,Password,OTPAuth,Favorite,Archived,Tags,Notes\na,,b,,JBSWY3DPEHPK3PXP,false,false,,\"" + raw + "\"\nc,,d,,JBSWY3DPEHPK3PXP,false,false,\n"),
            ("1pux export.data",
             #"{"accounts":[{"vaults":[{"items":[{"details":{"notesPlain":"@@N@@","sections":[{"fields":[{"value":{"totp":"JBSWY3DPEHPK3PXP"}}]}]},"overview":{"title":"a"}},{"details":{"sections":[{"fields":[{"value":{"totp":"JBSWY3DPEHPK3PXP"}}]}]},"overview":{"title":"b"}}]}]}]}"#),
        ]
        for (name, template) in cases {
            let data = template.replacingOccurrences(of: "@@N@@", with: jsonRaw)
            let found = try XCTUnwrap(try parse(data), name)
            XCTAssertEqual(found.accounts.map { Array($0.note.utf8) }, [Array(want.utf8), []], name)
        }
    }

    // MARK: - input bounds

    func testParseRejectsOversizedInput() {
        var data = Data(count: Importers.maxInputBytes + 1)
        data[0] = UInt8(ascii: "{")
        XCTAssertThrowsError(try Importers.parse(data)) { error in
            guard case Importers.ImporterError.malformed(let m) = error else {
                XCTFail("\(error) is not ImporterError.malformed")
                return
            }
            XCTAssertEqual(m, "input is larger than 64 MiB")
        }
    }

    func testJSONNestingLimitMatchesGo() {
        func nested(_ depth: Int) -> [UInt8] {
            Array(repeating: UInt8(ascii: "["), count: depth) + Array(repeating: UInt8(ascii: "]"), count: depth)
        }
        XCTAssertNotNil(GoJSON(nested(GoJSON.maxDepth)))
        XCTAssertNil(GoJSON(nested(GoJSON.maxDepth + 1)))
        XCTAssertNil(GoJSON(Array(#"{"a":1}x"#.utf8)))
        XCTAssertNotNil(GoJSON(Array(#" {"a":1,"a":2} "#.utf8)))
    }

    /// A bad entry fails the whole export and names the entry, with the text
    /// Go `importers.Parse` gives.
    func testBadEntryFailsTheExport() {
        let data = #"{"version":1,"header":{},"db":{"version":3,"entries":[{"type":"totp","name":"a","issuer":"b","info":{"secret":"JBSWY3DPEHPK3PXP"}},{"type":"totp","name":"c","issuer":"d","info":{"secret":"!!"}}]}}"#
        XCTAssertThrowsError(try parse(data)) { error in
            XCTAssertEqual("\(error)", #"aegis entry "c": decode secret: base32x: invalid base32 input"#)
        }
    }
}
