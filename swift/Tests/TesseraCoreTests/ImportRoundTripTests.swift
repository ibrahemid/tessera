import XCTest
import Foundation
@testable import TesseraCore

/// Ports go/internal/importers/roundtrip_test.go: every export format,
/// imported back, keeps the organisation that format holds.
final class ImportRoundTripTests: XCTestCase {

    private struct Carried {
        var folder = false
        var tags = false
        var note = false
        var pinned = false
    }

    private static let carried: [Exporters.Format: Carried] = [
        .aegis: Carried(folder: true, note: true, pinned: true),
        .twoFAS: Carried(folder: true),
        .bitwarden: Carried(folder: true, note: true, pinned: true),
        .proton: Carried(note: true),
        .andOTP: Carried(tags: true),
        .appleCSV: Carried(note: true),
        .googleMigration: Carried(),
    ]

    private static func accounts() -> [Account] {
        let secret = Data("12345678901234567890".utf8)
        return [
            Account(id: "a1", type: .totp, issuer: "GitHub", account: "user@example.com", secret: secret,
                    folder: "Work", tags: ["dev", "oss"], pinned: true, note: "Recovery codes in the safe\nsecond line"),
            Account(id: "b2", type: .totp, issuer: "Example Corp", account: "bob", secret: secret,
                    folder: "Work", tags: ["ops"]),
            Account(id: "c3", type: .totp, issuer: "Bank", account: "ops", secret: secret,
                    folder: "Finance", pinned: true, note: "Desk 12"),
            Account(id: "d4", type: .totp, issuer: "Plain", account: "none", secret: secret),
        ]
    }

    private func reimport(_ rendered: Exporters.Rendered) throws -> [Account] {
        XCTAssertEqual(rendered.skipped, [], rendered.format.rawValue)
        switch rendered.output {
        case .files(let files):
            XCTAssertEqual(files.count, 1, rendered.format.rawValue)
            let file = try XCTUnwrap(files.first, rendered.format.rawValue)
            return try XCTUnwrap(try Importers.parse(file.data), rendered.format.rawValue).accounts
        case .transferCodes(let codes):
            var out: [Account] = []
            for code in codes {
                let parsed = InputDetect.parseText(code.uri)
                XCTAssertTrue(parsed.errors.isEmpty, code.name)
                out += parsed.accounts
            }
            return out
        }
    }

    func testExportImportKeepsOrganisation() throws {
        let accounts = Self.accounts()
        for format in Exporters.Format.allCases {
            let c = try XCTUnwrap(Self.carried[format], "\(format.rawValue) has no organisation round-trip entry")
            let got = try reimport(try Exporters.render(format, accounts, batchID: 1))
            XCTAssertEqual(got.count, accounts.count, format.rawValue)
            for (g, want) in zip(got, accounts) {
                let at = "\(format.rawValue) \(want.id)"
                XCTAssertEqual(g.issuer, want.issuer, at)
                XCTAssertEqual(Array(g.folder.utf8), Array((c.folder ? want.folder : "").utf8), "\(at): folder")
                XCTAssertEqual(g.tags.map { Array($0.utf8) }, (c.tags ? want.tags : []).map { Array($0.utf8) },
                               "\(at): tags")
                XCTAssertEqual(Array(g.note.utf8), Array((c.note ? want.note : "").utf8), "\(at): note")
                XCTAssertEqual(g.pinned, c.pinned && want.pinned, "\(at): pinned")
            }
        }
    }
}
