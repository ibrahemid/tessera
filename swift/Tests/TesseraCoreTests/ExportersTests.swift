import XCTest
import Foundation
@testable import TesseraCore

final class ExportersTests: XCTestCase {

    private func account(_ id: String, issuer: String, account: String, type: OTPType = .totp,
                         note: String = "") -> Account {
        Account(id: id, type: type, issuer: issuer, account: account,
                secret: Data("12345678901234567890".utf8), note: note)
    }

    /// Every format rendered from the `exports` accounts gives the Go file
    /// names and bytes, transfer links and skip list.
    func testExportVectorsMatchGo() throws {
        let e = try VectorNode.root().get("exports")
        let accounts = try CanonicalJSON.decode(e.get("canonical_json").data)
        let batchID = try XCTUnwrap(Int32(exactly: e.get("batch_id").int()))
        let formats = try e.get("formats").items
        XCTAssertEqual(try formats.map { try $0.get("format").text }, Exporters.Format.allCases.map(\.name))
        for f in formats {
            let name = try f.get("format").text
            let format = try XCTUnwrap(Exporters.Format(rawValue: name), name)
            let r = try Exporters.render(format, accounts, batchID: batchID)
            XCTAssertEqual(r.format, format, name)
            switch r.output {
            case .files(let files):
                XCTAssertNil(f["codes"], name)
                let want = try f.get("files").items
                XCTAssertEqual(files.map { Data($0.name.utf8) }, try want.map { try $0.get("name").data }, name)
                XCTAssertEqual(files.map(\.data), try want.map { try $0.get("content").data }, name)
            case .transferCodes(let codes):
                XCTAssertNil(f["files"], name)
                let want = try f.get("codes").items
                XCTAssertEqual(codes.map { Data($0.name.utf8) }, try want.map { try $0.get("name").data }, name)
                XCTAssertEqual(codes.map { Data($0.uri.utf8) }, try want.map { try $0.get("uri").data }, name)
            }
            let skipped = try f.get("skipped").items
            XCTAssertEqual(r.skipped.map { Data($0.label.utf8) }, try skipped.map { try $0.get("label").data }, name)
            XCTAssertEqual(r.skipped.map(\.kind.rawValue), try skipped.map { try $0.get("kind").text }, name)
            XCTAssertEqual(r.skipped.map { Data($0.reason.utf8) }, try skipped.map { try $0.get("reason").data }, name)
        }
    }

    func testMigrationEmptySecretVectorMatchesGo() throws {
        let e = try VectorNode.root().get("exports")
        let v = try e.get("migration_empty_secret")
        let accounts = try CanonicalJSON.decode(v.get("canonical_json").data)
        let batchID = try XCTUnwrap(Int32(exactly: e.get("batch_id").int()))
        let r = try Exporters.render(.googleMigration, accounts, batchID: batchID)
        guard case .transferCodes(let codes) = r.output else { return XCTFail("no transfer codes") }
        let want = try v.get("codes").items
        XCTAssertFalse(want.isEmpty)
        XCTAssertEqual(codes.map { Data($0.name.utf8) }, try want.map { try $0.get("name").data })
        XCTAssertEqual(codes.map { Data($0.uri.utf8) }, try want.map { try $0.get("uri").data })
        let skipped = try v.get("skipped").items
        XCTAssertEqual(r.skipped.map { Data($0.label.utf8) }, try skipped.map { try $0.get("label").data })
        XCTAssertEqual(r.skipped.map(\.kind.rawValue), try skipped.map { try $0.get("kind").text })
        XCTAssertEqual(r.skipped.map { Data($0.reason.utf8) }, try skipped.map { try $0.get("reason").data })
        XCTAssertTrue(r.skipped.contains { $0.kind == .emptySecret })
    }

    func testFormatLookupTrimsAndLowercases() {
        XCTAssertEqual(Exporters.Format.lookup(" Apple-CSV\n"), .appleCSV)
        XCTAssertEqual(Exporters.Format.lookup("2FAS"), .twoFAS)
        XCTAssertNil(Exporters.Format.lookup("csv"))
        XCTAssertNil(Exporters.Format.lookup(""))
        for f in Exporters.Format.allCases {
            XCTAssertEqual(Exporters.Format.lookup(f.name), f)
            XCTAssertEqual(f.isMultiFile, f == .googleMigration, f.name)
        }
    }

    func testAppleCSVWithEveryAccountSkippedWritesNoFile() throws {
        let r = try Exporters.render(.appleCSV, [account("1", issuer: "=SUM(A1)", account: "x")])
        XCTAssertEqual(r.output, .files([]))
        XCTAssertEqual(r.skipped.map(\.kind), [.formulaName])
        XCTAssertEqual(r.skipped.first?.label, "=SUM(A1) (x)")
    }

    func testAppleCSVDropsOnlyAFormulaNote() throws {
        let r = try Exporters.render(.appleCSV, [account("1", issuer: "Mail", account: "me", note: "@cmd")])
        XCTAssertEqual(r.skipped, [Exporters.Skipped(label: "the note of Mail (me)",
                                                     reason: Exporters.appleNoteFormulaReason, kind: .formulaNote)])
        guard case .files(let files) = r.output, let file = files.first else {
            return XCTFail("expected one file, got \(r.output)")
        }
        XCTAssertEqual(file.name, "apple-passwords-export.csv")
        let text = String(decoding: file.data, as: UTF8.self)
        XCTAssertFalse(text.contains("@cmd"))
        XCTAssertTrue(text.hasPrefix("Title,URL,Username,Password,Notes,OTPAuth\nMail,,me,,,otpauth://"))
    }

    func testCSVRecordQuotesAsGo() {
        var out: [UInt8] = []
        Exporters.appendCSVRecord(["", "a,b", "q\"", " lead", "\\.", "line\nbreak", "plain"], to: &out)
        XCTAssertEqual(String(decoding: out, as: UTF8.self), ",\"a,b\",\"q\"\"\",\" lead\",\"\\.\",\"line\nbreak\",plain\n")
    }

    func testJSONEscapesHTMLAsGo() {
        XCTAssertEqual(String(decoding: ExportJSON.string("<a&b>\u{2028}").document, as: UTF8.self),
                       "\"\\u003ca\\u0026b\\u003e\\u2028\"\n")
        XCTAssertEqual(String(decoding: ExportJSON.object([("a", .array([])), ("b", .object([]))]).document,
                              as: UTF8.self),
                       "{\n  \"a\": [],\n  \"b\": {}\n}\n")
    }

    func testDerivedIDsAreStableDashedHex() {
        let id = Exporters.exportID("00000000-0000-4000-8000-000000000201")
        XCTAssertEqual(id, Exporters.exportID("00000000-0000-4000-8000-000000000201"))
        XCTAssertEqual(id.split(separator: "-").map(\.count), [8, 4, 4, 4, 12])
        XCTAssertTrue(id.unicodeScalars.allSatisfy { "0123456789abcdef-".unicodeScalars.contains($0) })
        XCTAssertNotEqual(Exporters.folderID("x"), Exporters.exportID("x"))
    }

    func testProtonSkipsHOTPWithoutItsSecret() throws {
        let hotp = account("1", issuer: "Bank", account: "ops", type: .hotp)
        let r = try Exporters.render(.proton, [hotp])
        XCTAssertEqual(r.skipped.map(\.kind), [.unsupportedType])
        XCTAssertEqual(r.skipped.first?.label, "Bank (ops)")
        let secret = Base32.encodeNoPad(hotp.secret)
        XCTAssertFalse(r.skipped.contains { $0.label.contains(secret) || $0.reason.contains(secret) })
    }

    func testTransferCodesShareTheBatchID() throws {
        let accounts = [account("1", issuer: "A", account: "a"), account("2", issuer: "B", account: "b")]
        let a = try Exporters.render(.googleMigration, accounts, batchID: 7)
        let b = try Exporters.render(.googleMigration, accounts, batchID: 7)
        XCTAssertEqual(a, b)
        guard case .transferCodes(let codes) = a.output else { return XCTFail("expected transfer codes") }
        XCTAssertEqual(codes.map(\.name), ["google-migration-1-of-1.png"])
        XCTAssertEqual(codes.first?.uri.hasPrefix("otpauth-migration://offline?data="), true)
    }
}
