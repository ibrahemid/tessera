import XCTest
import Foundation
@testable import TesseraCore

/// Times, counters, algorithms and account types the code generator must
/// refuse with a typed error instead of trapping, pinned to the Go vectors.
final class OTPRangeTests: XCTestCase {
    private let rfcSecret = Data("12345678901234567890".utf8)

    private func message(_ body: () throws -> Void) -> String {
        do {
            try body()
            return "no error"
        } catch {
            return "\(error)"
        }
    }

    private func account(_ type: OTPType, algorithm: String = "SHA1", counter: Int64 = 0) -> Account {
        Account(id: "a", type: type, issuer: "", account: "x", secret: rfcSecret, algorithm: algorithm, counter: counter)
    }

    func testTimeRangeVectors() throws {
        let v = try VectorNode.root().get("otp_time_range")
        let secret = try XCTUnwrap(Data(base64Encoded: v.get("secret_b64").text))
        let alg = try OTP.Algorithm.parse(v.get("algorithm").text)
        let digits = Int(try v.get("digits").int()), period = Int(try v.get("period").int())
        for c in try v.get("accepted").items {
            let time = Date(timeIntervalSince1970: TimeInterval(try c.get("time").int()))
            XCTAssertEqual(try OTP.totp(secret: secret, time: time, period: period, digits: digits, algorithm: alg),
                           try c.get("totp").text)
            XCTAssertEqual(try OTP.steam(secret: secret, time: time), try c.get("steam").text)
        }
        let want = try v.get("message").text
        XCTAssertEqual(try v.get("error").text, "time_range")
        for c in try v.get("refused").items {
            let time = Date(timeIntervalSince1970: TimeInterval(try c.int()))
            XCTAssertThrowsError(try OTP.totp(secret: secret, time: time, period: period, digits: digits, algorithm: alg)) {
                XCTAssertEqual($0 as? OTP.TimeRangeError, OTP.TimeRangeError())
                XCTAssertEqual("\($0)", want)
            }
            XCTAssertThrowsError(try OTP.steam(secret: secret, time: time)) {
                XCTAssertEqual($0 as? OTP.TimeRangeError, OTP.TimeRangeError())
            }
        }
    }

    func testTimesNoInt64HoldsAreRefusedWithoutTrapping() {
        let times: [TimeInterval] = [9_223_372_036_854_775_808, 1e30, .infinity, -.infinity, .nan, -0.5, -1, -30,
                                     TimeInterval(Int64.min), -1e30]
        for t in times {
            let time = Date(timeIntervalSince1970: t)
            for type in [OTPType.totp, .steam] {
                XCTAssertThrowsError(try OTP.code(for: account(type), at: time), "\(type) \(t)") {
                    XCTAssertTrue($0 is OTP.TimeRangeError, "\(type) \(t)")
                }
            }
        }
    }

    func testLargestRepresentableTimeStillGivesACode() throws {
        let time = Date(timeIntervalSince1970: 9_223_372_036_854_774_784)
        XCTAssertEqual(try OTP.code(for: account(.totp), at: time).count, 6)
        XCTAssertEqual(try OTP.code(for: account(.steam), at: time).count, 5)
    }

    func testUnknownTypeVectors() throws {
        let v = try VectorNode.root().get("unknown_type")
        let accounts = try CanonicalJSON.decode(v.get("payload").data)
        XCTAssertEqual(CanonicalJSON.encode(accounts), try v.get("expected_canonical_json").data)
        let cases = try v.get("accounts").items
        XCTAssertEqual(accounts.count, cases.count)
        for (a, c) in zip(accounts, cases) {
            XCTAssertEqual(Array(a.id.utf8), Array(try c.get("id").text.utf8))
            XCTAssertEqual(a.type, .unrecognized(try c.get("type").text))
            XCTAssertFalse(a.type.isRecognized)
            XCTAssertEqual(message { try a.validate() }, try c.get("validate_error").text)
            XCTAssertEqual(try c.get("code_error").text, "unknown_type")
            XCTAssertThrowsError(try OTP.code(for: a, at: Date(timeIntervalSince1970: 0))) {
                XCTAssertEqual($0 as? OTP.UnknownTypeError, OTP.UnknownTypeError(id: a.id, type: a.type.rawValue))
                XCTAssertEqual("\($0)", try? c.get("code_message").text)
            }
        }
    }

    func testHOTPCodeRefusedVectors() throws {
        let h = try VectorNode.root().get("hotp_counter_range")
        let accept = try XCTUnwrap(h.get("payload_accept").items.first)
        let base = try XCTUnwrap(CanonicalJSON.decode(accept.get("payload").data).first)
        let cases = try h.get("code_refused").get("cases").items
        XCTAssertFalse(cases.isEmpty)
        for c in cases {
            var a = base
            a.counter = try c.get("counter").int()
            XCTAssertEqual(try c.get("error").text, "counter_range")
            XCTAssertEqual(message { _ = try OTP.code(for: a, at: Date()) }, try c.get("message").text)
        }
    }

    func testAlgorithmParseTrimsLikeGo() throws {
        XCTAssertEqual(try OTP.Algorithm.parse(" sha256 "), .sha256)
        XCTAssertEqual(try OTP.Algorithm.parse("\tSHA512\n"), .sha512)
        XCTAssertEqual(try OTP.Algorithm.parse("  "), .sha1)
        XCTAssertEqual(message { _ = try OTP.Algorithm.parse(" md5 ") }, "otp: unsupported algorithm \" md5 \"")
    }

    func testSteamIgnoresTheAlgorithm() throws {
        let time = Date(timeIntervalSince1970: 1_700_000_000)
        let want = try OTP.steam(secret: rfcSecret, time: time)
        XCTAssertEqual(try OTP.code(for: account(.steam, algorithm: "MD5"), at: time), want)
        XCTAssertEqual(try OTP.code(for: account(.steam, algorithm: "SHA512"), at: time), want)
        XCTAssertThrowsError(try OTP.code(for: account(.totp, algorithm: "MD5"), at: time))
    }
}
