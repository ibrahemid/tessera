import XCTest
import Foundation
@testable import TesseraCore

/// Ports go/internal/importers/archive_test.go.
final class ImportersZipTests: XCTestCase {

    private func fixture(_ name: String) throws -> Data {
        try Data(contentsOf: importerTestdataDir().appendingPathComponent(name))
    }

    private func failure(_ data: Data, file: StaticString = #filePath, line: UInt = #line) -> Importers.ImporterError? {
        do {
            _ = try Importers.parse(data)
            XCTFail("expected a failure", file: file, line: line)
        } catch let e as Importers.ImporterError {
            return e
        } catch {
            XCTFail("unexpected \(error)", file: file, line: line)
        }
        return nil
    }

    private struct StoredEntry {
        var name: String
        var data: [UInt8]
        var method: UInt16 = 0
        var crc: UInt32?
        var uncompressedSize: UInt32?
    }

    /// A zip with stored entries and no data descriptors, as Go's `CreateRaw` writes one.
    private func storedZip(_ entries: [StoredEntry]) -> Data {
        func u16(_ v: UInt16) -> [UInt8] { [UInt8(v & 0xFF), UInt8(v >> 8)] }
        func u32(_ v: UInt32) -> [UInt8] { u16(UInt16(v & 0xFFFF)) + u16(UInt16(v >> 16)) }
        var out: [UInt8] = []
        var directory: [UInt8] = []
        for e in entries {
            let name = Array(e.name.utf8)
            let crc = e.crc ?? ZipArchive.crc32(e.data)
            let size = UInt32(e.data.count)
            let fields = u16(e.method) + u16(0) + u16(0) + u32(crc) + u32(size) + u32(e.uncompressedSize ?? size)
                + u16(UInt16(name.count)) + u16(0)
            let offset = UInt32(out.count)
            out += u32(0x0403_4B50) + u16(20) + u16(0) + fields + name + e.data
            directory += u32(0x0201_4B50) + u16(20) + u16(20) + u16(0) + fields
                + u16(0) + u16(0) + u16(0) + u32(0) + u32(offset) + name
        }
        let directoryOffset = UInt32(out.count)
        out += directory
        out += u32(0x0605_4B50) + u16(0) + u16(0) + u16(UInt16(entries.count)) + u16(UInt16(entries.count))
            + u32(UInt32(directory.count)) + u32(directoryOffset) + u16(0)
        return Data(out)
    }

    func testDamagedArchivesFailLikeTheGoImporter() throws {
        let cases: [(String, Importers.MalformedKind, String)] = [
            ("1password-bomb.1pux", .archive, "1password: read export.data: zip: not a valid zip file"),
            ("1password-damaged.1pux", .archive, "1password: read export.data: zip: checksum error"),
            ("1password-truncated.1pux", .archive, "1password: open archive: zip: not a valid zip file"),
            ("1password-oversized.1pux", .tooLarge, "1password: export.data in the archive is larger than 64 MiB"),
        ]
        for (file, kind, message) in cases {
            guard let e = failure(try fixture(file)) else { continue }
            XCTAssertEqual(e, .malformed(kind, message), file)
        }
    }

    func testStoredAndDeflatedArchivesReadTheSameAccounts() throws {
        let deflated = try XCTUnwrap(try Importers.parse(fixture("1password.1pux")))
        let stored = try XCTUnwrap(try Importers.parse(fixture("1password-stored.1pux")))
        let document = try XCTUnwrap(try Importers.parse(fixture("1password-export.data.json")))
        XCTAssertEqual(deflated.source, "1Password")
        XCTAssertEqual(deflated.accounts.count, 2)
        XCTAssertEqual(deflated.accounts, stored.accounts)
        XCTAssertEqual(deflated.accounts, document.accounts)
    }

    func testChecksEntryHeaders() throws {
        let data = [UInt8](try fixture("1password-export.data.json"))
        let crc = ZipArchive.crc32(data)
        let cases: [(String, StoredEntry, String)] = [
            ("short body", StoredEntry(name: "export.data", data: data, uncompressedSize: UInt32(data.count) + 5),
             "1password: read export.data: unexpected EOF"),
            ("wrong checksum", StoredEntry(name: "export.data", data: data, crc: crc ^ 1),
             "1password: read export.data: zip: checksum error"),
            ("unknown method", StoredEntry(name: "export.data", data: data, method: 12),
             "1password: open export.data: zip: unsupported compression algorithm"),
        ]
        for (name, entry, message) in cases {
            guard let e = failure(storedZip([entry])) else { continue }
            XCTAssertEqual(e, .malformed(.archive, message), name)
        }
    }

    func testReadsOnlyTheRootExportData() throws {
        let traversal = failure(try fixture("1password-traversal.1pux"))
        XCTAssertEqual(traversal, .malformed(.archive, "archive holds no export.data; a 1Password .1pux export does"))

        let zip = storedZip([
            StoredEntry(name: "../export.data", data: Array(#"{"accounts":[]}"#.utf8)),
            StoredEntry(name: "files/export.data", data: Array("not json".utf8)),
            StoredEntry(name: "export.data", data: [UInt8](try fixture("1password-export.data.json"))),
        ])
        let found = try XCTUnwrap(try Importers.parse(zip))
        XCTAssertEqual(found.accounts.count, 2)
    }

    func testCRC32MatchesTheIEEEPolynomial() {
        XCTAssertEqual(ZipArchive.crc32([]), 0)
        XCTAssertEqual(ZipArchive.crc32(Array("123456789".utf8)), 0xCBF4_3926)
    }
}
