import XCTest
import Foundation
@testable import TesseraCore

/// Locates the repo's /spec directory by walking up from this source file.
private func specDirURL() -> URL {
    var dir = URL(fileURLWithPath: #filePath)
    for _ in 0..<8 {
        dir.deleteLastPathComponent()
        let candidate = dir.appendingPathComponent("spec/testvectors.json")
        if FileManager.default.fileExists(atPath: candidate.path) {
            return dir.appendingPathComponent("spec")
        }
    }
    fatalError("spec/testvectors.json not found")
}

private func vectors() throws -> [String: Any] {
    let data = try Data(contentsOf: specDirURL().appendingPathComponent("testvectors.json"))
    return try JSONSerialization.jsonObject(with: data) as! [String: Any]
}

/// Mirrors the Go `internal/base32x` test cases. Decoding is lenient about how a
/// key is written down and strict about what it means (spec/otpauth.md
/// § base32): the two cores must accept and refuse exactly the same secrets.
final class Base32Tests: XCTestCase {

    func testRoundTripNoPad() throws {
        // The empty string is excluded: an empty secret is rejected, not decoded
        // to zero bytes (see testRejectsEmpty).
        for s in ["f", "fo", "foo", "foob", "fooba", "foobar"] {
            let encoded = Base32.encodeNoPad(Data(s.utf8))
            let decoded = try Base32.decode(encoded)
            XCTAssertEqual(String(decoding: decoded, as: UTF8.self), s, "round trip \(s) via \(encoded)")
        }
    }

    func testDecodesLenientWriting() throws {
        let cases: [(String, String)] = [
            ("my======", "f"),
            ("MZXW6YTB", "fooba"),
            ("MZ XW 6Y TB", "fooba"),
            ("mz-xw-6y-tb", "fooba"),
            ("MZXW6===", "foo"),
            ("MZXW6", "foo"),
            ("MY", "f"),
        ]
        for (input, want) in cases {
            let got = try Base32.decode(input)
            XCTAssertEqual(String(decoding: got, as: UTF8.self), want, "decode(\(input))")
        }
    }

    func testRejectsEmpty() {
        for input in ["", "   ", "\t\n", "-", "=", "  == -- "] {
            XCTAssertThrowsError(try Base32.decode(input), "decode(\(input)) must be an error")
        }
    }

    /// Group lengths no byte count can produce: a quantum of 8 characters
    /// encodes 5 bytes, so 1, 3 or 6 trailing characters is truncated input.
    func testRejectsBadQuantum() {
        for input in ["M", "MZX", "MZXW6Y", "MZXW6YTBM", "MZXW6YTBMZX"] {
            XCTAssertThrowsError(try Base32.decode(input), "decode(\(input)) must be an error")
        }
    }

    /// Input whose bits past the last whole byte are non-zero decodes without
    /// error under RFC 4648 but does not re-encode to itself, so the stored
    /// secret is not the one the user was handed.
    func testRejectsNonCanonicalTrailingBits() throws {
        // "MY" encodes 'f' (01100110 + 00 padding). "MZ" shares the byte but
        // sets a padding bit, so it re-encodes to "MY", not "MZ".
        XCTAssertThrowsError(try Base32.decode("MZ"))
        XCTAssertEqual(String(decoding: try Base32.decode("MY"), as: UTF8.self), "f")
        // "MZXW6" is the canonical encoding of "foo" (24 bits in 25, one pad
        // bit). "MZXW7" sets that pad bit: same three bytes, different string.
        XCTAssertEqual(String(decoding: try Base32.decode("MZXW6"), as: UTF8.self), "foo")
        XCTAssertThrowsError(try Base32.decode("MZXW7"))
    }

    func testRejectsCharacterOutsideAlphabet() {
        XCTAssertThrowsError(try Base32.decode("MZXW6YT!"))
        XCTAssertThrowsError(try Base32.decode("SGVsbG8gd29ybGQhISE="))
    }

    /// The shared strictness vectors; Go runs the same list.
    func testRejectVectors() throws {
        let b = try vectors()["base32"] as! [String: Any]
        let rejects = b["decode_reject"] as! [[String: Any]]
        XCTAssertFalse(rejects.isEmpty, "spec base32.decode_reject is empty")
        for c in rejects {
            let input = c["input"] as! String
            XCTAssertThrowsError(try Base32.decode(input),
                                 "decode(\(input)) must be an error: \(c["why"] as! String)")
        }
    }
}
