import Darwin
import XCTest
@testable import Tessera

final class DurableFileTests: XCTestCase {
    private let old = Data("old contents".utf8)
    private let new = Data("new contents".utf8)

    private func assertWriteError(_ expected: VaultWriteError, file: StaticString = #filePath, line: UInt = #line,
                                  _ body: () throws -> Void) {
        XCTAssertThrowsError(try body(), file: file, line: line) { error in
            XCTAssertEqual(error as? VaultWriteError, expected, file: file, line: line)
        }
    }

    func testReplaceWritesANewPrivateFile() throws {
        let dir = try makeScratchDirectory()
        let target = dir.appendingPathComponent("vault.json")

        try DurableFile.replace(new, at: target, hasDirectoryAccess: true)

        XCTAssertEqual(try Data(contentsOf: target), new)
        XCTAssertEqual(VaultFiles.mode(of: target), 0o600)
        XCTAssertEqual(VaultFiles.tempNames(in: dir), [])
    }

    func testReplaceTightensALooseFile() throws {
        let dir = try makeScratchDirectory()
        let target = dir.appendingPathComponent("vault.json")
        try VaultFiles.write(old, to: target, mode: 0o644)

        try DurableFile.replace(new, at: target, hasDirectoryAccess: true)

        XCTAssertEqual(try Data(contentsOf: target), new)
        XCTAssertEqual(VaultFiles.mode(of: target), 0o600)
        XCTAssertEqual(VaultFiles.tempNames(in: dir), [])
    }

    func testAFailedRenameLeavesTheFileAndNoTemp() throws {
        let dir = try makeScratchDirectory()
        let target = dir.appendingPathComponent("vault.json")
        try VaultFiles.write(old, to: target, mode: 0o600)

        assertWriteError(.io(operation: "rename", url: target, errno: EIO)) {
            try DurableFile.replace(new, at: target, hasDirectoryAccess: true, fileProtection: false,
                                    rename: { _, _ in EIO })
        }

        XCTAssertEqual(try Data(contentsOf: target), old)
        XCTAssertEqual(VaultFiles.tempNames(in: dir), [])
    }

    func testFileOnlyReplaceWritesThroughStaging() throws {
        let dir = try makeScratchDirectory()
        let target = dir.appendingPathComponent("vault.json")
        try VaultFiles.write(old, to: target, mode: 0o644)

        try DurableFile.replace(new, at: target, hasDirectoryAccess: false)

        XCTAssertEqual(try Data(contentsOf: target), new)
        XCTAssertEqual(VaultFiles.mode(of: target), 0o600)
        XCTAssertEqual(VaultFiles.tempNames(in: dir), [])
    }

    func testFileOnlyReplaceFallsBackWhenTheRenameIsRefused() throws {
        let dir = try makeScratchDirectory()
        let target = dir.appendingPathComponent("vault.json")
        try VaultFiles.write(old, to: target, mode: 0o644)
        let renames = CallCounter()

        try DurableFile.replace(new, at: target, hasDirectoryAccess: false, fileProtection: false,
                                rename: { _, _ in renames.increment(); return EPERM })

        XCTAssertEqual(renames.value, 1)
        XCTAssertEqual(try Data(contentsOf: target), new)
        XCTAssertEqual(VaultFiles.mode(of: target), 0o600)
        XCTAssertEqual(VaultFiles.tempNames(in: dir), [])
    }

    func testFileOnlyReplaceReportsOtherRenameFailures() throws {
        let dir = try makeScratchDirectory()
        let target = dir.appendingPathComponent("vault.json")
        try VaultFiles.write(old, to: target, mode: 0o600)

        assertWriteError(.io(operation: "rename", url: target, errno: EIO)) {
            try DurableFile.replace(new, at: target, hasDirectoryAccess: false, fileProtection: false,
                                    rename: { _, _ in EIO })
        }

        XCTAssertEqual(try Data(contentsOf: target), old)
    }

    func testProtectedReplaceWritesInsideTheContainer() throws {
        let dir = try makeScratchDirectory()
        let target = dir.appendingPathComponent("vault.json")

        try DurableFile.replace(new, at: target, hasDirectoryAccess: true, fileProtection: true)

        XCTAssertEqual(try Data(contentsOf: target), new)
        XCTAssertEqual(VaultFiles.mode(of: target), 0o600)
    }

    func testWritePrivateTruncatesAndTightens() throws {
        let dir = try makeScratchDirectory()
        let target = dir.appendingPathComponent("notes")
        try VaultFiles.write(Data(repeating: 9, count: 4096), to: target, mode: 0o644)

        try DurableFile.writePrivate(new, to: target)

        XCTAssertEqual(try Data(contentsOf: target), new)
        XCTAssertEqual(VaultFiles.mode(of: target), 0o600)
    }

    func testWritePrivateRefusesALink() throws {
        let dir = try makeScratchDirectory()
        let real = dir.appendingPathComponent("real")
        let link = dir.appendingPathComponent("link")
        try VaultFiles.write(old, to: real, mode: 0o600)
        try VaultFiles.symlink(link, to: real)

        assertWriteError(.io(operation: "open", url: link, errno: ELOOP)) {
            try DurableFile.writePrivate(new, to: link)
        }
        XCTAssertEqual(try Data(contentsOf: real), old)
    }

    func testCreateWritesANewPrivateFile() throws {
        for access in [true, false] {
            let dir = try makeScratchDirectory()
            let target = dir.appendingPathComponent("vault.json")

            try DurableFile.create(new, at: target, hasDirectoryAccess: access)

            XCTAssertEqual(try Data(contentsOf: target), new)
            XCTAssertEqual(VaultFiles.mode(of: target), 0o600)
            XCTAssertEqual(VaultFiles.tempNames(in: dir), [])
        }
    }

    func testCreateRefusesAnExistingFile() throws {
        for access in [true, false] {
            let dir = try makeScratchDirectory()
            let target = dir.appendingPathComponent("vault.json")
            try VaultFiles.write(old, to: target, mode: 0o644)

            assertWriteError(.alreadyExists(target)) {
                try DurableFile.create(new, at: target, hasDirectoryAccess: access)
            }

            XCTAssertEqual(try Data(contentsOf: target), old)
            XCTAssertEqual(VaultFiles.mode(of: target), 0o644)
            XCTAssertEqual(VaultFiles.tempNames(in: dir), [])
        }
    }

    func testCreateRefusesADanglingLink() throws {
        let dir = try makeScratchDirectory()
        let target = dir.appendingPathComponent("vault.json")
        let missing = dir.appendingPathComponent("missing.json")
        try VaultFiles.symlink(target, to: missing)

        assertWriteError(.alreadyExists(target)) {
            try DurableFile.create(new, at: target, hasDirectoryAccess: true)
        }

        XCTAssertFalse(VaultFiles.exists(missing))
        XCTAssertTrue(DurableFile.isSymlink(target))
    }

    func testPreserveKeepsTheBytesPrivately() throws {
        let dir = try makeScratchDirectory()
        let vault = dir.appendingPathComponent("vault.json")
        let kept = dir.appendingPathComponent("vault.json.damaged")
        try VaultFiles.write(old, to: vault, mode: 0o644)

        try DurableFile.preserve(vault, as: kept)

        XCTAssertEqual(try Data(contentsOf: kept), old)
        XCTAssertEqual(VaultFiles.mode(of: kept), 0o600)
        XCTAssertEqual(try Data(contentsOf: vault), old)
        XCTAssertEqual(VaultFiles.tempNames(in: dir), [])
    }

    func testPreserveReplacesAnEarlierCopy() throws {
        let dir = try makeScratchDirectory()
        let vault = dir.appendingPathComponent("vault.json")
        let kept = dir.appendingPathComponent("vault.json.damaged")
        try VaultFiles.write(old, to: vault, mode: 0o600)
        try VaultFiles.write(new, to: kept, mode: 0o644)

        try DurableFile.preserve(vault, as: kept)

        XCTAssertEqual(try Data(contentsOf: kept), old)
        XCTAssertEqual(VaultFiles.mode(of: kept), 0o600)
    }

    func testPreserveReportsAMissingFile() throws {
        let dir = try makeScratchDirectory()
        let vault = dir.appendingPathComponent("vault.json")

        assertWriteError(.missingFile(vault)) {
            try DurableFile.preserve(vault, as: dir.appendingPathComponent("vault.json.damaged"))
        }
        XCTAssertEqual(VaultFiles.tempNames(in: dir), [])
    }

    func testReadReportsMissingOversizedAndNonRegularFiles() throws {
        let dir = try makeScratchDirectory()
        let target = dir.appendingPathComponent("vault.json")

        assertWriteError(.missingFile(target)) { _ = try DurableFile.read(target) }

        try VaultFiles.write(Data(repeating: 1, count: 10), to: target, mode: 0o600)
        XCTAssertEqual(try DurableFile.read(target, limit: 10).count, 10)
        assertWriteError(.unparseable(target)) { _ = try DurableFile.read(target, limit: 4) }

        assertWriteError(.io(operation: "read", url: dir, errno: EFTYPE)) { _ = try DurableFile.read(dir) }
    }

    func testRemoveIgnoresAMissingFile() throws {
        let dir = try makeScratchDirectory()
        let target = dir.appendingPathComponent("vault.json.bak")
        try VaultFiles.write(old, to: target, mode: 0o600)

        try DurableFile.remove(target)
        XCTAssertFalse(VaultFiles.exists(target))
        try DurableFile.remove(target)
    }

    func testMakePrivateDirectory() throws {
        let dir = try makeScratchDirectory()
        let nested = dir.appendingPathComponent("a/b", isDirectory: true)

        try DurableFile.makePrivateDirectory(nested)
        try DurableFile.makePrivateDirectory(nested)

        XCTAssertEqual(VaultFiles.mode(of: nested), 0o700)
        XCTAssertEqual(VaultFiles.mode(of: nested.deletingLastPathComponent()), 0o700)

        let file = dir.appendingPathComponent("file")
        try VaultFiles.write(old, to: file, mode: 0o600)
        XCTAssertThrowsError(try DurableFile.makePrivateDirectory(file)) { error in
            XCTAssertEqual((error as? POSIXError)?.code, .ENOTDIR)
        }
    }

    func testTempNamesMatchTheCLI() {
        XCTAssertTrue(DurableFile.isTempName(".tess-0.tmp"))
        XCTAssertTrue(DurableFile.isTempName(".tess-4294967295.tmp"))
        XCTAssertFalse(DurableFile.isTempName(".tess-.tmp"))
        XCTAssertFalse(DurableFile.isTempName(".tess-12a.tmp"))
        XCTAssertFalse(DurableFile.isTempName(".tess-\u{0661}.tmp"))
        XCTAssertFalse(DurableFile.isTempName("tess-1.tmp"))
        XCTAssertFalse(DurableFile.isTempName(".tess-1.tmp.bak"))
        XCTAssertFalse(DurableFile.isTempName("vault.json"))
    }

    func testSweepRemovesOnlyStalePrivateTemps() throws {
        let dir = try makeScratchDirectory()
        let now = Date()
        let stale = now.addingTimeInterval(-DurableFile.staleTempAge - 60)
        func make(_ name: String, mode: mode_t, modified: Date) throws -> URL {
            let url = dir.appendingPathComponent(name)
            try VaultFiles.write(old, to: url, mode: mode)
            try FileManager.default.setAttributes([.modificationDate: modified], ofItemAtPath: url.path)
            return url
        }
        let removed = try make(".tess-1.tmp", mode: 0o600, modified: stale)
        let fresh = try make(".tess-2.tmp", mode: 0o600, modified: now)
        let loose = try make(".tess-3.tmp", mode: 0o644, modified: stale)
        let foreign = try make(".tess-x.tmp", mode: 0o600, modified: stale)
        let vault = try make("vault.json", mode: 0o600, modified: stale)
        let link = dir.appendingPathComponent(".tess-4.tmp")
        try VaultFiles.symlink(link, to: vault)

        DurableFile.sweepStaleTemps(in: dir, now: now)

        XCTAssertFalse(VaultFiles.exists(removed))
        for kept in [fresh, loose, foreign, vault, link] {
            XCTAssertTrue(VaultFiles.exists(kept), kept.lastPathComponent)
        }
    }

    func testResolvedTargetFollowsALinkOnce() throws {
        let dir = try makeScratchDirectory()
        let real = dir.appendingPathComponent("real.json")
        let link = dir.appendingPathComponent("vault.json")
        try VaultFiles.write(old, to: real, mode: 0o600)
        try VaultFiles.symlink(link, to: real)

        XCTAssertEqual(DurableFile.resolvedTarget(link).path, real.path)
        XCTAssertEqual(DurableFile.resolvedTarget(real).path, real.path)
        XCTAssertTrue(DurableFile.isSymlink(link))
        XCTAssertFalse(DurableFile.isSymlink(real))
    }
}
