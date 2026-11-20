import XCTest
import Foundation
@testable import TesseraCore

/// Ports go/internal/importers/quote_test.go.
final class ImportersQuoteTests: XCTestCase {

    private static let long = String(repeating: "X", count: 32)

    func testQuoteBounded() {
        let esc = String(UnicodeScalar(0x1B))
        let cases: [(String, String)] = [
            ("", #""""#),
            ("abcdefghijklmnop", #""abcdefghijklmnop""#),
            ("abcdefghijklmnopq", #""abcdefghijklmnop"… (17 bytes)"#),
            ("aaaaaaaaaaaaaaa\u{20AC}", #""aaaaaaaaaaaaaaa"… (18 bytes)"#),
            ("aaaaaaaaaaaaa\u{1F600}b", #""aaaaaaaaaaaaa"… (18 bytes)"#),
            (String(repeating: esc, count: 20), "\"" + String(repeating: #"\x1b"#, count: 16) + "\"… (20 bytes)"),
        ]
        for (input, want) in cases {
            XCTAssertEqual(Importers.quoteBounded(input), want, input)
        }
    }

    func testImporterErrorsBoundQuotedValues() {
        let long = Self.long
        let cases: [(String, String)] = [
            (#"[{"secret":"JBSWY3DPEHPK3PXP","issuer":"i","label":"a","type":""# + long + #""}]"#,
             #"andotp entry "i": unsupported account type "XXXXXXXXXXXXXXXX"… (32 bytes)"#),
            (#"[{"secret":"JBSWY3DPEHPK3PXP","issuer":""# + long + #"","label":"a","type":"TOTP","algorithm":""# + long + #""}]"#,
             #"andotp entry "XXXXXXXXXXXXXXXX"… (32 bytes): unsupported algorithm "XXXXXXXXXXXXXXXX"… (32 bytes)"#),
            (#"[{"issuer":"i","secret":"JBSWY3DPEHPK3PXP","kind":"TOTP","digits":""# + long + #""}]"#,
             #"raivo entry "i": digits: not a number: "XXXXXXXXXXXXXXXX"… (32 bytes)"#),
        ]
        for (input, want) in cases {
            XCTAssertThrowsError(try Importers.parse(Data(input.utf8)), input) { error in
                XCTAssertEqual("\(error)", want, input)
            }
        }
    }

    func testImporterErrorsNeverQuoteTheSecret() {
        let secret = "JBSWY3DPEHPK3PX1ZZZZ"
        let inputs = [
            #"[{"secret":""# + secret + #"","issuer":"i","label":"a","type":"TOTP"}]"#,
            #"[{"issuer":"i","secret":""# + secret + #"","kind":"TOTP"}]"#,
            #"{"version":1,"db":{"version":3,"entries":[{"type":"totp","name":"a","info":{"secret":""# + secret + #""}}]}}"#,
            #"{"services":[{"name":"A","secret":""# + secret + #"","otp":{"tokenType":"TOTP"}}]}"#,
            #"{"items":[{"name":"A","login":{"totp":""# + secret + #""}}]}"#,
            "Title,Url,Username,Password,OTPAuth,Favorite,Archived,Tags,Notes\nA,,,," + secret + ",,,,\n",
        ]
        for input in inputs {
            XCTAssertThrowsError(try Importers.parse(Data(input.utf8)), input) { error in
                XCTAssertFalse("\(error)".contains(secret), "\(error)")
                XCTAssertFalse("\(error)".contains("JBSWY"), "\(error)")
            }
        }
    }
}
