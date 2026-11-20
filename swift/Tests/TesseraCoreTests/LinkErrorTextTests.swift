import XCTest
import Foundation
@testable import TesseraCore

final class LinkErrorTextTests: XCTestCase {

    private func otpauthError(_ uri: String, file: StaticString = #filePath, line: UInt = #line) -> String? {
        do {
            _ = try OTPAuth.parse(uri)
            return nil
        } catch AccountError.invalid(let m) {
            return m
        } catch {
            XCTFail("\(error) is not AccountError.invalid", file: file, line: line)
            return nil
        }
    }

    private func migrationError(_ uri: String, file: StaticString = #filePath, line: UInt = #line) -> String? {
        do {
            _ = try Migration.parse(uri)
            return nil
        } catch AccountError.invalid(let m) {
            return m
        } catch {
            XCTFail("\(error) is not AccountError.invalid", file: file, line: line)
            return nil
        }
    }

    func testQuoteValueMatchesGo() {
        let cases: [([UInt8], String)] = [
            ([], "\"\""),
            (Array("sha3".utf8), "\"sha3\""),
            (Array("0123456789abcdef".utf8), "\"0123456789abcdef\""),
            (Array("0123456789abcdefg".utf8), "\"0123456789abcdef\"… (17 bytes)"),
            (Array((String(repeating: "a", count: 15) + "€x").utf8), "\"aaaaaaaaaaaaaaa\"… (19 bytes)"),
            (Array((String(repeating: "a", count: 13) + "€x").utf8), "\"aaaaaaaaaaaaa€\"… (17 bytes)"),
            ([0x74, 0x6f, 0x74, 0xa9], "\"tot\\xa9\""),
            ([UInt8](repeating: 0xff, count: 20), "\"" + String(repeating: "\\xff", count: 16) + "\"… (20 bytes)"),
            (Array("a\"b\\c\n".utf8), "\"a\\\"b\\\\c\\n\""),
            ([0xef, 0xbf, 0xbd, 0xed, 0xa0, 0x80], "\"\u{FFFD}\\xed\\xa0\\x80\""),
        ]
        for (b, want) in cases {
            XCTAssertEqual(GoText.quoteValue(bytes: b), want, "\(b)")
        }
        XCTAssertEqual(GoText.quote(bytes: [0x61, 0x80, 0x62]), "\"a\\x80b\"")
    }

    func testOTPAuthErrorQuotesNoLongValueAndNoSecret() {
        let second = "KRSXG5CTMVRXEZLU"
        let cases: [(String, String)] = [
            ("otpauth://hotp/A:b?secret=JBSWY3DPEHPK3PXP&counter=0otpauth://hotp/A:c?secret=" + second,
             "otpauth: invalid counter \"0otpauth://hotp/\"… (43 bytes)"),
            ("otpauth://totp/A:b?secret=JBSWY3DPEHPK3PXP&issuer=Aotpauth://totp/A:c?secret=" + second,
             "otpauth: issuer mismatch (label \"A\", param \"Aotpauth://totp/\"… (43 bytes))"),
            ("otpauth://totp/A:b?secret=JBSWY3DPEHPK3PXP&digits=6otpauth://totp/A:c?secret=" + second,
             "otpauth: invalid digits \"6otpauth://totp/\"… (43 bytes)"),
            ("otpauth://totp/A:b?secret=JBSWY3DPEHPK3PXP&period=30otpauth://totp/A:c?secret=" + second,
             "otpauth: invalid period \"30otpauth://totp\"… (44 bytes)"),
            ("otpauth://totp/A:b?secret=JBSWY3DPEHPK3PXP&algorithm=SHA1otpauth://totp/A:c?secret=" + second,
             "otpauth: unsupported algorithm \"SHA1otpauth://to\"… (46 bytes)"),
        ]
        for (uri, want) in cases {
            let m = otpauthError(uri)
            XCTAssertEqual(m, want, uri)
            XCTAssertFalse(m?.contains(second) ?? true, uri)
        }
        XCTAssertEqual(otpauthError("otpauth://hotp/A:b?secret=JBSWY3DPEHPK3PXP&counter=x7"),
                       "otpauth: invalid counter \"x7\"")
    }

    func testOTPAuthErrorQuotesInvalidBytesAsGoDoes() {
        XCTAssertEqual(otpauthError("otpauth://tot%A9/?secret=JBSWY3DPEHPK3PXP"),
                       "otpauth: unsupported type \"tot\\xa9\"")
        XCTAssertEqual(otpauthError("otpauth://totp/A:b?secret=JBSWY3DPEHPK3PXP&algorithm=SH%FFA"),
                       "otpauth: unsupported algorithm \"SH\\xffA\"")
        XCTAssertEqual(otpauthError("otpauth://totp/A:b?secret=JBSWY3DPEHPK3PXP&digits=%A96"),
                       "otpauth: invalid digits \"\\xa96\"")
    }

    func testOTPAuthQueryKeysAreBytes() throws {
        let a = try OTPAuth.parse("otpauth://totp/A:b?secret=JBSWY3DPEHPK3PXP&issuer=A&%FF=1&period=60")
        XCTAssertEqual(a.period, 60)
        XCTAssertEqual(otpauthError("otpauth://totp/A:b?secret=JBSWY3DPEHPK3PXP&period=%FF"),
                       "otpauth: invalid period \"\\xff\"")
    }

    func testOTPAuthQueryParsesInLinearTime() throws {
        let uri = "otpauth://totp/A:b?" + String(repeating: "&", count: 1_000_000) + "secret=JBSWY3DPEHPK3PXP"
        let start = Date()
        let a = try OTPAuth.parse(uri)
        XCTAssertLessThan(Date().timeIntervalSince(start), 1.0)
        XCTAssertEqual(a.account, "b")
    }

    func testMigrationQueryParsesInLinearTime() throws {
        let uri = "otpauth-migration://offline?" + String(repeating: "&", count: 1_000_000)
            + "data=Ch8KCkhlbGxvId6tvu8SBWFsaWNlGgRBQ01FIAEoATACEAEYASAAKLlg"
        let start = Date()
        let accounts = try Migration.parse(uri)
        XCTAssertLessThan(Date().timeIntervalSince(start), 1.0)
        XCTAssertEqual(accounts.count, 1)
    }

    func testUnexpectedHostErrorQuotesNoPartOfTheLink() {
        let data = "Ci0KCkhlbGxvId6tvu8SDkV4YW1wbGU6YWxpY2Ug"
        let want = "migration: unexpected host; the link must start with otpauth-migration://offline?"
        for uri in ["otpauth-migration://offlinedata=" + data,
                    "otpauth-migration://offline&data=" + data,
                    "otpauth-migration://online?data=" + data] {
            let m = migrationError(uri)
            XCTAssertEqual(m, want, uri)
            XCTAssertFalse(m?.contains(String(data.prefix(8))) ?? true, uri)
        }
    }

    func testMigrationSchemeErrorQuotesABoundedValue() {
        XCTAssertEqual(migrationError("otpauth-migrationx://offline?data=AA%3D%3D"),
                       "migration: not an otpauth-migration uri (scheme \"otpauth-migratio\"… (18 bytes))")
    }

    func testProtobufFailureTextsMatchGo() {
        func uri(_ payload: [UInt8]) -> String {
            "otpauth-migration://offline?data=" + GoURL.escape(Data(payload).base64EncodedString(), .queryComponent)
        }
        let cases: [([UInt8], String)] = [
            ([0x0a, 0x05], "migration: malformed otp_parameters: unexpected EOF"),
            ([0x00], "migration: malformed payload tag: proto: invalid field number"),
            ([0x10] + [UInt8](repeating: 0xff, count: 9) + [0x02],
             "migration: malformed payload field 2: proto: variable length integer overflow"),
            ([0x36], "migration: malformed unknown field 6: proto: cannot parse reserved wire type"),
            ([0x34], "migration: malformed unknown field 6: proto: mismatching end group marker"),
            ([UInt8](repeating: 0x33, count: 20000), "migration: malformed unknown field 6: proto: parse error"),
        ]
        for (payload, want) in cases {
            XCTAssertEqual(migrationError(uri(payload)), want, "\(payload.prefix(4))")
        }
    }
}
