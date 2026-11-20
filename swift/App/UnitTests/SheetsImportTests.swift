import XCTest
import AppKit
import ImageIO
import UniformTypeIdentifiers
import TesseraCore
import TesseraArgon2
@testable import Tessera

final class SheetsImportScannerTests: XCTestCase {
    private var dir: URL!

    override func setUpWithError() throws {
        try super.setUpWithError()
        dir = FileManager.default.temporaryDirectory.appendingPathComponent("sheets-import-\(UUID().uuidString)")
        try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
    }

    override func tearDownWithError() throws {
        try? FileManager.default.removeItem(at: dir)
        try super.tearDownWithError()
    }

    private let uri = "otpauth://totp/GitHub:octocat?secret=JBSWY3DPEHPK3PXP&issuer=GitHub"

    private func write(_ name: String, _ data: Data) throws -> URL {
        let url = dir.appendingPathComponent(name)
        try data.write(to: url)
        return url
    }

    private func writePNG(_ name: String, _ image: CGImage) throws -> URL {
        let url = dir.appendingPathComponent(name)
        let dest = try XCTUnwrap(CGImageDestinationCreateWithURL(url as CFURL, UTType.png.identifier as CFString, 1, nil))
        CGImageDestinationAddImage(dest, image, nil)
        XCTAssertTrue(CGImageDestinationFinalize(dest))
        return url
    }

    /// A white canvas with `content` drawn in the middle, so a QR code has a quiet zone.
    private func canvas(side: Int, content: CGImage? = nil) throws -> CGImage {
        let ctx = try XCTUnwrap(CGContext(data: nil, width: side, height: side, bitsPerComponent: 8, bytesPerRow: 0,
                                          space: CGColorSpaceCreateDeviceRGB(),
                                          bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue))
        ctx.setFillColor(NSColor.white.cgColor)
        ctx.fill(CGRect(x: 0, y: 0, width: side, height: side))
        if let content {
            let x = (side - content.width) / 2, y = (side - content.height) / 2
            ctx.draw(content, in: CGRect(x: x, y: y, width: content.width, height: content.height))
        }
        return try XCTUnwrap(ctx.makeImage())
    }

    func testTextFileWithLinkImports() throws {
        let url = try write("codes.txt", Data((uri + "\n").utf8))
        let scan = ImportScanner.scan([url])
        XCTAssertEqual(scan.accounts.map(\.issuer), ["GitHub"])
        XCTAssertTrue(scan.failures.isEmpty)
        XCTAssertTrue(scan.backups.isEmpty)
    }

    func testEmptyFileIsAFailure() throws {
        let url = try write("empty.txt", Data())
        let scan = ImportScanner.scan([url])
        XCTAssertTrue(scan.accounts.isEmpty)
        XCTAssertEqual(scan.failures.map(\.source), ["empty.txt"])
        XCTAssertEqual(scan.failures.first?.kind, .item)
    }

    func testBadLineIsReportedWithFileAndLine() throws {
        let url = try write("mixed.txt", Data("\(uri)\notpauth://totp/Bad?secret=1\n".utf8))
        let scan = ImportScanner.scan([url])
        XCTAssertEqual(scan.accounts.count, 1)
        XCTAssertEqual(scan.failures.map(\.source), ["mixed.txt line 2"])
    }

    func testTesseraBackupIsRecognised() throws {
        let sample = Account(id: "1", type: .totp, issuer: "GitHub", account: "a", secret: Data("12345678901234567890".utf8))
        let data = try Envelope.create(accounts: [sample], passphrase: "correct-horse", argon2: Argon2Reference()).encoded()
        let url = try write("tessera-backup.json", data)
        let scan = ImportScanner.scan([url])
        XCTAssertEqual(scan.backups, [url])
        XCTAssertTrue(scan.accounts.isEmpty)
        XCTAssertTrue(scan.failures.isEmpty)
    }

    func testQRImageImports() throws {
        let qr = try XCTUnwrap(QRImage.generate(from: uri, scale: 8)?.cgImage(forProposedRect: nil, context: nil, hints: nil))
        let url = try writePNG("qr.png", try canvas(side: qr.width + 160, content: qr))
        let scan = ImportScanner.scan([url])
        XCTAssertEqual(scan.accounts.map(\.account), ["octocat"])
        XCTAssertTrue(scan.failures.isEmpty)
    }

    func testImageWithoutQRIsANoQRFailure() throws {
        let url = try writePNG("blank.png", try canvas(side: 120))
        let scan = ImportScanner.scan([url])
        XCTAssertTrue(scan.accounts.isEmpty)
        XCTAssertEqual(scan.failures.map(\.kind), [.noQR])
    }

    func testProgressReportsEachFileAndBatchContinuesPastFailures() throws {
        let missing = dir.appendingPathComponent("missing.txt")
        let good = try write("codes.txt", Data(uri.utf8))
        let empty = try write("empty.txt", Data())
        let counts = Counts()
        let scan = ImportScanner.scan([missing, good, empty]) { counts.append($0) }
        XCTAssertEqual(counts.values, [1, 2, 3])
        XCTAssertEqual(scan.accounts.count, 1)
        XCTAssertEqual(scan.failures.map(\.source), ["missing.txt", "empty.txt"])
    }
}

private final class Counts: @unchecked Sendable {
    private let lock = NSLock()
    private var stored: [Int] = []
    func append(_ n: Int) { lock.withLock { stored.append(n) } }
    var values: [Int] { lock.withLock { stored } }
}

final class SheetsImportSummaryTests: XCTestCase {
    private func failure(_ kind: ImportItemFailure.Kind = .item) -> ImportItemFailure {
        ImportItemFailure(source: "a.png", display: "", reason: "No QR code found.", kind: kind)
    }

    func testCleanAddNeedsNoReview() {
        XCTAssertFalse(ImportSummary(added: 2, duplicates: 0, failures: []).needsReview)
        XCTAssertFalse(ImportSummary(added: 0, duplicates: 2, failures: []).needsReview)
    }

    func testAnyFailureNeedsReview() {
        XCTAssertTrue(ImportSummary(added: 3, duplicates: 0, failures: [failure()]).needsReview)
    }

    func testNothingImportedNeedsReview() {
        XCTAssertTrue(ImportSummary(added: 0, duplicates: 0, failures: []).needsReview)
    }

    func testLineCountsImagesWithoutQR() {
        let s = ImportSummary(added: 1, duplicates: 1, failures: [failure(.noQR), failure(.noQR), failure()])
        XCTAssertEqual(s.line, "Added 1 · 1 duplicate skipped · 2 images had no QR code · 1 not imported")
    }
}

final class SheetsImportProgressTests: XCTestCase {
    @MainActor
    func testProgressText() {
        XCTAssertEqual(ImportProgressView.text(for: ImportProgress(done: 0, total: 1)), "Reading 1 file…")
        XCTAssertEqual(ImportProgressView.text(for: ImportProgress(done: 0, total: 3)), "Reading 1 of 3 files…")
        XCTAssertEqual(ImportProgressView.text(for: ImportProgress(done: 3, total: 3)), "Reading 3 of 3 files…")
    }
}
