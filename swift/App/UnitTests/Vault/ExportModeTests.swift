import AppKit
import XCTest
import TesseraCore
import TesseraArgon2
@testable import Tessera

final class ExportModeTests: XCTestCase {
    private let accounts = [VaultFixture.account("a", issuer: "Alpha", account: "me@example.com")]
    private let password = "correct-horse"

    private func exportLinks(to url: URL) throws { try LinkExport.write(accounts, to: url) }

    private func exportBackup(to url: URL) throws {
        try BackupExport.write(accounts, password: password, argon2: Argon2Reference(), to: url)
    }

    private func exportQR(to url: URL) throws {
        let image = try XCTUnwrap(QRImage.generate(from: OTPAuth.format(accounts[0])))
        try QRImage.writePNG(image, to: url)
    }

    func testLinkExportIsOwnerOnly() throws {
        let url = try makeScratchDirectory().appendingPathComponent("links.txt")
        try exportLinks(to: url)
        XCTAssertEqual(VaultFiles.mode(of: url), 0o600)
        XCTAssertEqual(try String(contentsOf: url, encoding: .utf8), LinkExport.body(for: accounts))
    }

    func testBackupExportIsOwnerOnlyAndRestorable() throws {
        let url = try makeScratchDirectory().appendingPathComponent("backup.json")
        try exportBackup(to: url)
        XCTAssertEqual(VaultFiles.mode(of: url), 0o600)
        let restored = try Envelope.decode(Data(contentsOf: url)).open(passphrase: password, argon2: Argon2Reference())
        XCTAssertEqual(restored.map(\.id), ["a"])
    }

    func testQRImageIsOwnerOnly() throws {
        let url = try makeScratchDirectory().appendingPathComponent("qr.png")
        try exportQR(to: url)
        XCTAssertEqual(VaultFiles.mode(of: url), 0o600)
        XCTAssertNotNil(NSImage(contentsOf: url))
    }

    func testEveryExportTightensAnExistingWorldReadableFile() throws {
        let dir = try makeScratchDirectory()
        let exports: [(String, (URL) throws -> Void)] = [
            ("links.txt", exportLinks), ("backup.json", exportBackup), ("qr.png", exportQR),
        ]
        for (name, export) in exports {
            let url = dir.appendingPathComponent(name)
            try VaultFiles.write(Data("old".utf8), to: url, mode: 0o644)
            try export(url)
            XCTAssertEqual(VaultFiles.mode(of: url), 0o600, name)
            XCTAssertNotEqual(try Data(contentsOf: url), Data("old".utf8), name)
        }
    }

    func testExportDoesNotFollowASymlink() throws {
        let dir = try makeScratchDirectory()
        let target = dir.appendingPathComponent("target.txt")
        try VaultFiles.write(Data("keep".utf8), to: target, mode: 0o644)
        let link = dir.appendingPathComponent("links.txt")
        try VaultFiles.symlink(link, to: target)

        XCTAssertThrowsError(try exportLinks(to: link))
        XCTAssertEqual(try Data(contentsOf: target), Data("keep".utf8))
        XCTAssertEqual(VaultFiles.mode(of: target), 0o644)
    }
}
