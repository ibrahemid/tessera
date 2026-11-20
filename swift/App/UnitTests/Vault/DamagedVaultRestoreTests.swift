import Darwin
import XCTest
import TesseraCore
@testable import Tessera

final class DamagedVaultRestoreTests: XCTestCase {
    private let damaged = Data("{\"version\":99".utf8)

    private struct Fixture {
        let dir: URL
        let vault: URL
        let backup: URL
        let backupBytes: Data
    }

    private func makeFixture(damagedVault: Bool = true) throws -> Fixture {
        let dir = try makeScratchDirectory()
        let vault = dir.appendingPathComponent("vault.json")
        let backup = VaultSidecar.backupURL(for: vault)
        let (envelope, _) = try VaultFiles.makeEnvelope()
        let backupBytes = try envelope.encoded()
        try VaultFiles.write(backupBytes, to: backup, mode: 0o600)
        if damagedVault { try VaultFiles.write(damaged, to: vault, mode: 0o644) }
        return Fixture(dir: dir, vault: vault, backup: backup, backupBytes: backupBytes)
    }

    private func restorer(_ vault: URL, access: VaultLocation.Access = .folder) -> DamagedVaultRestore {
        DamagedVaultRestore(location: VaultLocation(fileURL: vault, access: access),
                            queue: VaultIOQueue(label: "tests.restore.\(UUID().uuidString)"))
    }

    private func assertWriteError(_ expected: VaultWriteError, file: StaticString = #filePath, line: UInt = #line,
                                  _ body: () async throws -> Void) async {
        do {
            try await body()
            XCTFail("expected \(expected)", file: file, line: line)
        } catch {
            XCTAssertEqual(error as? VaultWriteError, expected, file: file, line: line)
        }
    }

    func testRestoreKeepsTheDamagedFileAndWritesTheBackup() async throws {
        let fixture = try makeFixture()
        let asked = CallCounter()

        let restored = try await restorer(fixture.vault).run(from: fixture.backup) { asked.increment(); return true }

        XCTAssertTrue(restored)
        XCTAssertEqual(asked.value, 1)
        XCTAssertEqual(try Data(contentsOf: fixture.vault), fixture.backupBytes)
        XCTAssertEqual(VaultFiles.mode(of: fixture.vault), 0o600)
        let kept = DamagedVaultRestore.damagedURL(for: fixture.vault)
        XCTAssertEqual(kept.lastPathComponent, "vault.json.damaged")
        XCTAssertEqual(try Data(contentsOf: kept), damaged)
        XCTAssertEqual(VaultFiles.mode(of: kept), 0o600)
        XCTAssertEqual(try Data(contentsOf: fixture.backup), fixture.backupBytes)
        XCTAssertTrue(VaultFiles.exists(VaultSidecar.lockURL(for: fixture.vault)))
        XCTAssertEqual(VaultFiles.tempNames(in: fixture.dir), [])
    }

    func testDecliningChangesNothing() async throws {
        let fixture = try makeFixture()

        let restored = try await restorer(fixture.vault).run(from: fixture.backup) { false }

        XCTAssertFalse(restored)
        XCTAssertEqual(try Data(contentsOf: fixture.vault), damaged)
        XCTAssertEqual(VaultFiles.mode(of: fixture.vault), 0o644)
        XCTAssertFalse(VaultFiles.exists(DamagedVaultRestore.damagedURL(for: fixture.vault)))
    }

    func testAnUnparseableSourceIsRefusedBeforeAsking() async throws {
        let fixture = try makeFixture()
        let source = fixture.dir.appendingPathComponent("other.json")
        try VaultFiles.write(Data("{}".utf8), to: source, mode: 0o600)
        let asked = CallCounter()

        await assertWriteError(.unparseable(source)) {
            _ = try await self.restorer(fixture.vault).run(from: source) { asked.increment(); return true }
        }

        XCTAssertEqual(asked.value, 0)
        XCTAssertEqual(try Data(contentsOf: fixture.vault), damaged)
        XCTAssertFalse(VaultFiles.exists(DamagedVaultRestore.damagedURL(for: fixture.vault)))
    }

    func testFileOnlyAccessIsRefusedBeforeAsking() async throws {
        let fixture = try makeFixture()
        let asked = CallCounter()

        await assertWriteError(.io(operation: "restore", url: fixture.vault, errno: EACCES)) {
            _ = try await self.restorer(fixture.vault, access: .fileOnly)
                .run(from: fixture.backup) { asked.increment(); return true }
        }

        XCTAssertEqual(asked.value, 0)
        XCTAssertEqual(try Data(contentsOf: fixture.vault), damaged)
    }

    func testAMissingVaultIsRestoredWithoutADamagedCopy() async throws {
        let fixture = try makeFixture(damagedVault: false)

        let restored = try await restorer(fixture.vault).run(from: fixture.backup) { true }

        XCTAssertTrue(restored)
        XCTAssertEqual(try Data(contentsOf: fixture.vault), fixture.backupBytes)
        XCTAssertFalse(VaultFiles.exists(DamagedVaultRestore.damagedURL(for: fixture.vault)))
    }

    func testALinkedVaultIsRestoredAtItsTarget() async throws {
        let fixture = try makeFixture()
        let linkDir = fixture.dir.appendingPathComponent("links", isDirectory: true)
        try FileManager.default.createDirectory(at: linkDir, withIntermediateDirectories: false)
        let link = linkDir.appendingPathComponent("vault.json")
        try VaultFiles.symlink(link, to: fixture.vault)

        let restored = try await restorer(link).run(from: fixture.backup) { true }

        XCTAssertTrue(restored)
        XCTAssertTrue(DurableFile.isSymlink(link))
        XCTAssertEqual(try Data(contentsOf: fixture.vault), fixture.backupBytes)
        XCTAssertEqual(try Data(contentsOf: DamagedVaultRestore.damagedURL(for: fixture.vault)), damaged)
        XCTAssertEqual(try FileManager.default.contentsOfDirectory(atPath: linkDir.path), ["vault.json"])
    }
}
