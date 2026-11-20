import XCTest
import Foundation
@testable import TesseraCore

/// Mirrors the Go `internal/otpauth` query-encoding tests (spec/otpauth.md
/// § query encoding): a space is emitted as `%20`, a literal plus as `%2B`, and
/// a `+` arriving in a query value is read as a space.
final class OTPAuthTests: XCTestCase {

    private func account(issuer: String, account name: String) -> Account {
        Account(id: "x", type: .totp, issuer: issuer, account: name,
                secret: Data("12345678901234567890".utf8),
                algorithm: "SHA1", digits: 6, period: 30)
    }

    private func query(_ uri: String) -> String {
        String(uri[uri.range(of: "?")!.upperBound...])
    }

    func testFormatEncodesSpaceAsPercent20() {
        let uri = OTPAuth.format(account(issuer: "Acme Corp", account: "john@example.com"))
        XCTAssertTrue(query(uri).contains("issuer=Acme%20Corp"), uri)
        XCTAssertFalse(query(uri).contains("+"), "no bare '+' in the query: \(uri)")
        XCTAssertTrue(uri.hasPrefix("otpauth://totp/Acme%20Corp:john@example.com?"), uri)
    }

    func testFormatEncodesLiteralPlus() throws {
        let a = account(issuer: "C++ Forum", account: "dev+x@example.com")
        let uri = OTPAuth.format(a)
        XCTAssertTrue(query(uri).contains("issuer=C%2B%2B%20Forum"), uri)
        let back = try OTPAuth.parse(uri)
        XCTAssertEqual(back.issuer, a.issuer)
        XCTAssertEqual(back.account, a.account)
    }

    /// Other emitters (and Tessera's own older exports) still write a space as
    /// '+'; the parser must read it as a space, and %2B must survive as a plus.
    func testParseAcceptsPlusAsSpace() throws {
        let cases: [(uri: String, issuer: String, account: String)] = [
            ("otpauth://totp/Acme%20Corp:john@example.com?secret=JBSWY3DPEHPK3PXP&issuer=Acme+Corp",
             "Acme Corp", "john@example.com"),
            ("otpauth://totp/john@example.com?secret=JBSWY3DPEHPK3PXP&issuer=Acme+Corp",
             "Acme Corp", "john@example.com"),
            ("otpauth://totp/C++%20Forum:dev%2Bx@example.com?secret=JBSWY3DPEHPK3PXP&issuer=C%2B%2B%20Forum",
             "C++ Forum", "dev+x@example.com"),
        ]
        for c in cases {
            let got = try OTPAuth.parse(c.uri)
            XCTAssertEqual(got.issuer, c.issuer, c.uri)
            XCTAssertEqual(got.account, c.account, c.uri)
        }
    }

    /// Verbatim `tess export` output for a spaced issuer (Go `otpauth.Format`,
    /// params sorted): it must parse here with the label and issuer param
    /// agreeing.
    func testParsesGoEmittedSpacedIssuer() throws {
        let uri = "otpauth://totp/Acme%20Corp:john@example.com?issuer=Acme%20Corp&secret=GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
        let got = try OTPAuth.parse(uri)
        XCTAssertEqual(got.issuer, "Acme Corp")
        XCTAssertEqual(got.account, "john@example.com")
        XCTAssertEqual(got.secret, Data("12345678901234567890".utf8))
    }

    func testLabelRoundTrip() throws {
        let cases: [(issuer: String, account: String)] = [
            ("ACME Co", "john@example.com"),
            ("Acme: Prod", "ops"),
            ("Acme", "a:b"),
            ("25% off", "sale%20"),
            ("a%41b", "c%2541d"),
            ("Café Ω", "рабочий@пример.рф"),
            ("slash/issuer", "slash/account"),
            ("", "no-issuer@example.com"),
            ("plus+issuer", "plus+account"),
        ]
        for c in cases {
            let a = account(issuer: c.issuer, account: c.account)
            let uri = OTPAuth.format(a)
            let back = try OTPAuth.parse(uri)
            XCTAssertEqual(back.issuer, c.issuer, uri)
            XCTAssertEqual(back.account, c.account, uri)
        }
    }

    // MARK: - shared vectors

    private func vectors() throws -> [String: Any] {
        var dir = URL(fileURLWithPath: #filePath)
        for _ in 0..<8 {
            dir.deleteLastPathComponent()
            let candidate = dir.appendingPathComponent("spec/testvectors.json")
            if FileManager.default.fileExists(atPath: candidate.path) {
                return try JSONSerialization.jsonObject(with: Data(contentsOf: candidate)) as! [String: Any]
            }
        }
        throw XCTSkip("spec/testvectors.json not found")
    }

    private func assertAccount(_ a: Account, _ c: [String: Any], _ name: String,
                               file: StaticString = #filePath, line: UInt = #line) {
        XCTAssertEqual(a.type.rawValue, c["type"] as? String, "\(name): type", file: file, line: line)
        XCTAssertEqual(Array(a.issuer.utf8), Array((c["issuer"] as! String).utf8), "\(name): issuer", file: file, line: line)
        XCTAssertEqual(Array(a.account.utf8), Array((c["account"] as! String).utf8), "\(name): account", file: file, line: line)
        XCTAssertEqual(Base32.encodeNoPad(a.secret), c["secret_b32"] as? String, "\(name): secret", file: file, line: line)
        if let algorithm = c["algorithm"] as? String {
            XCTAssertEqual(a.algorithm, algorithm, "\(name): algorithm", file: file, line: line)
        }
        if let digits = c["digits"] as? NSNumber {
            XCTAssertEqual(a.digits, digits.intValue, "\(name): digits", file: file, line: line)
        }
        if let period = c["period"] as? NSNumber {
            XCTAssertEqual(a.period, period.intValue, "\(name): period", file: file, line: line)
        }
        if let counter = c["counter"] as? NSNumber {
            XCTAssertEqual(a.counter, counter.int64Value, "\(name): counter", file: file, line: line)
        }
    }

    func testStrictValidVectors() throws {
        let set = try vectors()["otpauth_strict"] as! [String: Any]
        let cases = set["valid"] as! [[String: Any]]
        XCTAssertFalse(cases.isEmpty, "otpauth_strict.valid is empty")
        for c in cases {
            let name = c["name"] as! String
            let a = try OTPAuth.parse(c["uri"] as! String)
            assertAccount(a, c, name)
        }
    }

    func testStrictInvalidVectors() throws {
        let set = try vectors()["otpauth_strict"] as! [String: Any]
        let cases = set["invalid"] as! [[String: Any]]
        XCTAssertFalse(cases.isEmpty, "otpauth_strict.invalid is empty")
        for c in cases {
            let name = c["name"] as! String
            XCTAssertThrowsError(try OTPAuth.parse(c["uri"] as! String), name) { error in
                guard case AccountError.invalid(let m) = error else {
                    XCTFail("\(name): \(error) is not AccountError.invalid")
                    return
                }
                XCTAssertTrue(m.hasPrefix("otpauth: "), "\(name): \(m)")
            }
        }
    }

    func testParseVectors() throws {
        let set = try vectors()["otpauth_uri"] as! [String: Any]
        let cases = set["parse"] as! [[String: Any]]
        XCTAssertFalse(cases.isEmpty, "otpauth_uri.parse is empty")
        for c in cases {
            let uri = c["uri"] as! String
            assertAccount(try OTPAuth.parse(uri), c, uri)
        }
    }

    /// Each row's text is what Go `otpauth.Parse` returns for the same input; an
    /// empty text means Go accepts it.
    func testErrorTextMatchesGo() {
        let cases: [(String, String)] = [
            ("otpauth://totp/Ac%ZZme:alice?secret=JBSWY3DPEHPK3PXP", "otpauth: invalid URI"),
            ("otpauth://totp/ACME:alice%2?secret=JBSWY3DPEHPK3PXP", "otpauth: invalid URI"),
            ("otpauth://totp/ACME:alice?secret=JBSWY3DPEHPK3PXP&issuer=%ZZ", "otpauth: invalid percent-encoding in query"),
            ("otpauth://totp/ACME:alice?secret=%G1", "otpauth: invalid percent-encoding in query"),
            ("otpauth://totp/ACME:alice?secret=JBSWY3DPEHPK3PXP&x-note=%ZZ", "otpauth: invalid percent-encoding in query"),
            ("otpauth://hotp/Bank:ops?secret=JBSWY3DPEHPK3PXP&counter=9223372036854775807", "otpauth: invalid counter \"9223372036854775\"… (19 bytes)"),
            ("otpauth://hotp/Bank:ops?secret=JBSWY3DPEHPK3PXP&counter=9223372036854775808", "otpauth: invalid counter \"9223372036854775\"… (19 bytes)"),
            ("otpauth://hotp/Bank:ops?secret=JBSWY3DPEHPK3PXP&counter=-1", "otpauth: invalid counter \"-1\""),
            ("otpauth://hotp/Bank:ops?secret=JBSWY3DPEHPK3PXP&counter=1.5", "otpauth: invalid counter \"1.5\""),
            ("otpauth://hotp/Bank:ops?secret=JBSWY3DPEHPK3PXP&counter=0x10", "otpauth: invalid counter \"0x10\""),
            ("otpauth://hotp/Bank:ops?secret=JBSWY3DPEHPK3PXP", "otpauth: hotp requires counter"),
            ("otpauth://totp/A?issuer=B", "otpauth: missing secret"),
            ("otpauth://totp/A:b?secret=JBSWY3DPEHPK3PXP&issuer=B", "otpauth: issuer mismatch (label \"A\", param \"B\")"),
            ("otpauth://totp/A?secret=JBSWY3DPEHPK3PXP&digits=9", "otpauth: invalid digits \"9\""),
            ("otpauth://totp/A?secret=JBSWY3DPEHPK3PXP&algorithm=MD5", "otpauth: unsupported algorithm \"MD5\""),
            ("otpauth://totp/A?secret=JBSWY3DPEHPK3PXP&period=0", "otpauth: invalid period \"0\""),
            ("otpauth://x/A?secret=JBSWY3DPEHPK3PXP", "otpauth: unsupported type \"x\""),
            ("http://totp/A?secret=JBSWY3DPEHPK3PXP", "otpauth: not an otpauth uri (scheme \"http\")"),
            ("otpauth://totp/A?secret=MZ", "otpauth: bad secret: base32x: invalid base32 input"),
            ("otpauth://totp/A?secret=JBSWY3DPEHPK3PXP&period=abc", "otpauth: invalid period \"abc\""),
            ("otpauth://totp?secret=JBSWY3DPEHPK3PXP", ""),
            ("otpauth://totp/A?secret=JBSWY3DPEHPK3PXP&digits=", ""),
            ("otpauth://totp/%ZZ", "otpauth: invalid URI"),
            ("otpauth://totp/A?secret=JBSWY3DPEHPK3PXP&counter=1", ""),
        ]
        for (uri, want) in cases {
            do {
                _ = try OTPAuth.parse(uri)
                XCTAssertEqual(want, "", "\(uri): accepted")
            } catch AccountError.invalid(let m) {
                XCTAssertEqual(m, want, uri)
            } catch {
                XCTFail("\(uri): \(error) is not AccountError.invalid")
            }
        }
    }

    /// Go `otpauth.Format` output for the same accounts: parameters sorted by
    /// key, defaults omitted, label parts path-escaped.
    func testFormatMatchesGo() {
        let secret = Data("Hello!".utf8) + Data([0xde, 0xad, 0xbe, 0xef])
        let cases: [(Account, String)] = [
            (Account(id: "", type: .totp, issuer: "Example", account: "alice@example.com", secret: secret),
             "otpauth://totp/Example:alice@example.com?issuer=Example&secret=JBSWY3DPEHPK3PXP"),
            (Account(id: "", type: .hotp, issuer: "Bank", account: "ops", secret: secret,
                     algorithm: "SHA256", digits: 8, period: 0, counter: 5),
             "otpauth://hotp/Bank:ops?algorithm=SHA256&counter=5&digits=8&issuer=Bank&secret=JBSWY3DPEHPK3PXP"),
            (Account(id: "", type: .steam, issuer: "Steam", account: "gabe", secret: secret, digits: 5),
             "otpauth://steam/Steam:gabe?digits=5&issuer=Steam&secret=JBSWY3DPEHPK3PXP"),
            (Account(id: "", type: .totp, issuer: "", account: "solo", secret: secret,
                     algorithm: "SHA512", digits: 8, period: 60),
             "otpauth://totp/solo?algorithm=SHA512&digits=8&period=60&secret=JBSWY3DPEHPK3PXP"),
            (Account(id: "", type: .totp, issuer: "Acme: Prod & Co", account: "a/b?c#d e+f%", secret: secret),
             "otpauth://totp/Acme%3A%20Prod%20&%20Co:a%2Fb%3Fc%23d%20e+f%25?issuer=Acme%3A%20Prod%20%26%20Co&secret=JBSWY3DPEHPK3PXP"),
            (Account(id: "", type: .totp, issuer: "Café", account: "", secret: secret),
             "otpauth://totp/Caf%C3%A9:?issuer=Caf%C3%A9&secret=JBSWY3DPEHPK3PXP"),
        ]
        for (a, want) in cases {
            XCTAssertEqual(OTPAuth.format(a), want)
        }
    }

    func testStrictValidVectorsRoundTripThroughFormat() throws {
        let set = try vectors()["otpauth_strict"] as! [String: Any]
        for c in set["valid"] as! [[String: Any]] {
            let a = try OTPAuth.parse(c["uri"] as! String)
            let back = try OTPAuth.parse(OTPAuth.format(a))
            assertAccount(back, c, c["name"] as! String)
        }
    }
}
