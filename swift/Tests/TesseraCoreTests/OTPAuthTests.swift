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
}
