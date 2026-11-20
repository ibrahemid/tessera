import Darwin
import XCTest
@testable import Tessera

final class VaultFileLockTests: XCTestCase {
    private func vaultURL() throws -> URL {
        try makeScratchDirectory().appendingPathComponent("vault.json")
    }

    private func assertLockFails(_ vault: URL, with expected: VaultLockError,
                                 file: StaticString = #filePath, line: UInt = #line) {
        XCTAssertThrowsError(try VaultFileLock.acquire(for: vault, timeout: .milliseconds(60)),
                             file: file, line: line) { error in
            XCTAssertEqual(error as? VaultLockError, expected, file: file, line: line)
        }
    }

    func testAHeldLockTimesOut() throws {
        let vault = try vaultURL()
        let held = try VaultFileLock.acquire(for: vault)
        defer { held.release() }

        let start = ContinuousClock.now
        assertLockFails(vault, with: .timedOut(VaultSidecar.lockURL(for: vault)))
        XCTAssertGreaterThanOrEqual(ContinuousClock.now - start, .milliseconds(60))
    }

    func testTheLockFileIsPrivateAndKeptAfterRelease() throws {
        let vault = try vaultURL()
        let lockURL = VaultSidecar.lockURL(for: vault)
        let lock = try VaultFileLock.acquire(for: vault)
        XCTAssertEqual(lock.url, lockURL)
        XCTAssertEqual(VaultFiles.mode(of: lockURL), 0o600)

        lock.release()

        XCTAssertTrue(VaultFiles.exists(lockURL))
        let again = try VaultFileLock.acquire(for: vault, timeout: .milliseconds(60))
        again.release()
        XCTAssertEqual(VaultFiles.mode(of: lockURL), 0o600)
    }

    func testReleasingTwiceIsHarmless() throws {
        let vault = try vaultURL()
        let lock = try VaultFileLock.acquire(for: vault)
        lock.release()
        lock.release()
        try VaultFileLock.acquire(for: vault, timeout: .milliseconds(60)).release()
    }

    func testAMissingFolderIsCreatedPrivately() throws {
        let folder = try makeScratchDirectory().appendingPathComponent("share/tessera", isDirectory: true)
        let vault = folder.appendingPathComponent("vault.json")

        try VaultFileLock.acquire(for: vault).release()

        XCTAssertEqual(VaultFiles.mode(of: folder), 0o700)
        XCTAssertEqual(VaultFiles.mode(of: folder.deletingLastPathComponent()), 0o700)
        XCTAssertEqual(VaultFiles.mode(of: VaultSidecar.lockURL(for: vault)), 0o600)
    }

    func testALooseLockFileIsRefused() throws {
        let vault = try vaultURL()
        let lockURL = VaultSidecar.lockURL(for: vault)
        try VaultFiles.write(Data(), to: lockURL, mode: 0o644)

        assertLockFails(vault, with: .wrongMode(lockURL, mode: 0o644))
        XCTAssertEqual(VaultFiles.mode(of: lockURL), 0o644)
    }

    func testALinkedLockFileIsRefused() throws {
        let vault = try vaultURL()
        let lockURL = VaultSidecar.lockURL(for: vault)
        let elsewhere = vault.deletingLastPathComponent().appendingPathComponent("elsewhere")
        try VaultFiles.write(Data(), to: elsewhere, mode: 0o600)
        try VaultFiles.symlink(lockURL, to: elsewhere)

        assertLockFails(vault, with: .symlink(lockURL))
    }

    func testADanglingLinkIsRefused() throws {
        let vault = try vaultURL()
        let lockURL = VaultSidecar.lockURL(for: vault)
        let missing = vault.deletingLastPathComponent().appendingPathComponent("missing")
        try VaultFiles.symlink(lockURL, to: missing)

        assertLockFails(vault, with: .symlink(lockURL))
        XCTAssertFalse(VaultFiles.exists(missing))
    }

    func testAFolderInPlaceOfTheLockIsRefused() throws {
        let vault = try vaultURL()
        let lockURL = VaultSidecar.lockURL(for: vault)
        try FileManager.default.createDirectory(at: lockURL, withIntermediateDirectories: false)

        assertLockFails(vault, with: .notRegularFile(VaultSidecar.lockURL(for: vault)))
    }
}
