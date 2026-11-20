import XCTest
import Foundation
@testable import TesseraCore

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

/// JSONDecoder, because JSONSerialization drops a leading U+FEFF inside a
/// string value and three of these inputs start with one.
private struct TextCleanVectors: Decodable {
    struct Case: Decodable {
        let name: String
        let input: String
        let output: String

        enum CodingKeys: String, CodingKey {
            case name
            case input = "in"
            case output = "out"
        }
    }

    let text: [Case]
    let note: [Case]

    static func load() throws -> TextCleanVectors {
        struct Root: Decodable {
            let textClean: TextCleanVectors
            enum CodingKeys: String, CodingKey { case textClean = "text_clean" }
        }
        let data = try Data(contentsOf: specDirURL().appendingPathComponent("testvectors.json"))
        return try JSONDecoder().decode(Root.self, from: data).textClean
    }
}

/// spec/testvectors.json `text_clean`, the vectors Go `account.CleanText` and
/// `account.CleanNote` pass. Results are compared as UTF-8 bytes: String
/// equality would accept a canonically equivalent but different result.
final class TextCleanTests: XCTestCase {

    func testTextVectors() throws {
        let cases = try TextCleanVectors.load().text
        XCTAssertFalse(cases.isEmpty, "text_clean.text is empty")
        for c in cases {
            XCTAssertEqual(Array(TextClean.text(c.input).utf8), Array(c.output.utf8), c.name)
        }
    }

    func testNoteVectors() throws {
        let cases = try TextCleanVectors.load().note
        XCTAssertFalse(cases.isEmpty, "text_clean.note is empty")
        for c in cases {
            XCTAssertEqual(Array(TextClean.note(c.input).utf8), Array(c.output.utf8), c.name)
        }
    }

    func testCleaningIsIdempotent() throws {
        let v = try TextCleanVectors.load()
        for c in v.text {
            let once = TextClean.text(c.input)
            XCTAssertEqual(Array(TextClean.text(once).utf8), Array(once.utf8), c.name)
        }
        for c in v.note {
            let once = TextClean.note(c.input)
            XCTAssertEqual(Array(TextClean.note(once).utf8), Array(once.utf8), c.name)
        }
    }

    func testCombiningMarksAreNotNormalized() {
        let decomposed = "e\u{301}"
        XCTAssertEqual(Array(TextClean.text(decomposed).utf8), Array(decomposed.utf8))
        XCTAssertEqual(Array(TextClean.note("\u{202e}" + decomposed).utf8), Array(decomposed.utf8))
    }

    func testTrimSpaceMatchesGoWhiteSpace() {
        XCTAssertEqual(GoText.trimSpace("\u{85}\u{a0}\u{1680}\u{2000}\u{200a}\u{2028}\u{2029}\u{202f}\u{205f}\u{3000}x\u{b}\u{c}"), "x")
        XCTAssertEqual(GoText.trimSpace("\u{200b}x\u{180e}"), "\u{200b}x\u{180e}")
    }
}
