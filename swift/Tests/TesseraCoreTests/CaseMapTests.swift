import XCTest
import Foundation
@testable import TesseraCore

/// The generated case tables against spec/case_mapping.json, which Go writes
/// from `unicode.ToLower` and `unicode.ToUpper`.
final class CaseMapTests: XCTestCase {
    private struct Mapping: Decodable {
        let unicode_version: String
        let lower: [[UInt32]]
        let upper: [[UInt32]]
    }

    private func mapping() throws -> Mapping {
        try JSONDecoder().decode(Mapping.self, from: Data(contentsOf: VectorNode.specFile("case_mapping.json")))
    }

    private func table(_ pairs: [[UInt32]]) -> [UInt32: UInt32] {
        var out: [UInt32: UInt32] = [:]
        for p in pairs { out[p[0]] = p[1] }
        return out
    }

    func testTablesNameTheGoUnicodeVersion() throws {
        XCTAssertEqual(CaseMap.unicodeVersion, try mapping().unicode_version)
        XCTAssertEqual(CaseMap.unicodeVersion, "17.0.0")
    }

    func testEveryScalarMapsAsGoDoes() throws {
        let m = try mapping()
        let lower = table(m.lower), upper = table(m.upper)
        XCTAssertEqual(lower.count, m.lower.count)
        XCTAssertEqual(upper.count, m.upper.count)
        var lowerMismatch: [UInt32] = [], upperMismatch: [UInt32] = []
        for v in UInt32(0)...0x10FFFF where v < 0xD800 || v > 0xDFFF {
            if CaseMap.lower(v) != lower[v] ?? v { lowerMismatch.append(v) }
            if CaseMap.upper(v) != upper[v] ?? v { upperMismatch.append(v) }
        }
        XCTAssertEqual(lowerMismatch.prefix(10).map { String($0, radix: 16) }, [])
        XCTAssertEqual(upperMismatch.prefix(10).map { String($0, radix: 16) }, [])
    }

    func testValuesOutsideTheScalarRangeMapToThemselves() {
        for v: UInt32 in [0xD800, 0xDFFF, 0x110000, .max] {
            XCTAssertEqual(CaseMap.lower(v), v)
            XCTAssertEqual(CaseMap.upper(v), v)
        }
    }

    func testGoTextUsesTheTables() {
        XCTAssertEqual(GoText.toLower("\u{212A}\u{130}ΣA"), "k\u{69}σa")
        XCTAssertEqual(GoText.toUpper("\u{17F}ǆß"), "SǄß")
    }
}
