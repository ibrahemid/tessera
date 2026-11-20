import Darwin
import os
import XCTest
import TesseraCore
@testable import Tessera

final class VaultWriterTests: XCTestCase {
    private let github = VaultFixture.account("g", issuer: "GitHub", account: "me@example.com")
    private let aws = VaultFixture.account("a", issuer: "AWS", account: "root")
    private let allAccess: [VaultLocation.Access] = [.container, .folder, .fileOnly]

    private func makeLocation(_ access: VaultLocation.Access) throws -> VaultLocation {
        VaultLocation(fileURL: try makeScratchDirectory().appendingPathComponent("vault.json"), access: access)
    }

    private func makeQueue() -> VaultIOQueue {
        VaultIOQueue(label: "tests.vault-io.\(UUID().uuidString)")
    }

    private func makeVault(at location: VaultLocation, accounts: [Account] = []) async throws -> CommittedVault {
        let (envelope, dek) = try VaultFiles.makeEnvelope(accounts: accounts)
        return try await VaultWriter(location: location, queue: makeQueue())
            .create(VaultCommit(envelope: envelope, accounts: accounts, dek: dek, backupPolicy: .keep))
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

    func testCommitRoundTripsThroughTheFile() async throws {
        for access in allAccess {
            let location = try makeLocation(access)
            let created = try await makeVault(at: location)
            let writer = VaultWriter(location: location, queue: makeQueue())

            let committed = try await writer.commit(heldDEK: created.dek, VaultFiles.appending(github, dek: created.dek))

            XCTAssertEqual(committed.accounts, [github])
            XCTAssertEqual(committed.dek, created.dek)
            XCTAssertEqual(committed.bytes, try committed.envelope.encoded())
            XCTAssertEqual(try Data(contentsOf: location.fileURL), committed.bytes)
            XCTAssertEqual(VaultFiles.mode(of: location.fileURL), 0o600)
            let snapshot = try await writer.read()
            XCTAssertEqual(snapshot.bytes, committed.bytes)
            XCTAssertEqual(try snapshot.envelope.open(dek: created.dek), [github])
            XCTAssertNotNil(snapshot.modifiedAt)
            XCTAssertEqual(snapshot.modifiedAt, committed.modifiedAt)
            XCTAssertEqual(VaultFiles.tempNames(in: location.fileURL.deletingLastPathComponent()), [])
        }
    }

    func testCommitStartsFromTheFileNotFromAnEarlierRead() async throws {
        for access in allAccess {
            let location = try makeLocation(access)
            let created = try await makeVault(at: location)
            let writer = VaultWriter(location: location, queue: makeQueue())
            let outside = try created.envelope.reseal(accounts: [aws], dek: created.dek)
            try await writer.restore(verbatim: try outside.encoded())

            let committed = try await writer.commit(heldDEK: created.dek, VaultFiles.appending(github, dek: created.dek))

            XCTAssertEqual(committed.accounts, [aws, github])
            let after = try await writer.read()
            XCTAssertEqual(try after.envelope.open(dek: created.dek), [aws, github])
        }
    }

    func testRestoreWritesTheBytesVerbatim() async throws {
        for access in allAccess {
            let location = try makeLocation(access)
            let created = try await makeVault(at: location)
            let bytes = try created.envelope.reseal(accounts: [aws], dek: created.dek).encoded()

            try await VaultWriter(location: location, queue: makeQueue()).restore(verbatim: bytes)

            XCTAssertEqual(try Data(contentsOf: location.fileURL), bytes)
            XCTAssertEqual(VaultFiles.mode(of: location.fileURL), 0o600)
        }
    }

    func testCreateRefusesAnExistingFile() async throws {
        for access in allAccess {
            let location = try makeLocation(access)
            let created = try await makeVault(at: location)
            XCTAssertEqual(VaultFiles.mode(of: location.fileURL), 0o600)

            await assertWriteError(.alreadyExists(location.fileURL)) {
                _ = try await VaultWriter(location: location, queue: self.makeQueue())
                    .create(VaultCommit(envelope: created.envelope, accounts: [], dek: created.dek, backupPolicy: .keep))
            }
            XCTAssertEqual(try Data(contentsOf: location.fileURL), created.bytes)
        }
    }

    func testCreateMakesAMissingFolderPrivately() async throws {
        let folder = try makeScratchDirectory().appendingPathComponent("share/tessera", isDirectory: true)
        let location = VaultLocation(fileURL: folder.appendingPathComponent("vault.json"), access: .folder)

        let created = try await makeVault(at: location)

        XCTAssertEqual(try Data(contentsOf: location.fileURL), created.bytes)
        XCTAssertEqual(VaultFiles.mode(of: folder), 0o700)
        XCTAssertEqual(VaultFiles.mode(of: location.fileURL), 0o600)
        XCTAssertEqual(VaultFiles.mode(of: VaultSidecar.lockURL(for: location.fileURL)), 0o600)
    }

    func testAnotherKeyIsReportedAsKeyChanged() async throws {
        for access in allAccess {
            let location = try makeLocation(access)
            let created = try await makeVault(at: location)
            let other = Data(repeating: 7, count: 32)

            await assertWriteError(.keyChanged) {
                _ = try await VaultWriter(location: location, queue: self.makeQueue())
                    .commit(heldDEK: other, VaultFiles.appending(self.github, dek: other))
            }
            XCTAssertEqual(try Data(contentsOf: location.fileURL), created.bytes)
        }
    }

    func testARotatedKeyWritesNothing() async throws {
        for access in allAccess {
            let location = try makeLocation(access)
            let created = try await makeVault(at: location)
            let (rotated, _) = try VaultFiles.makeEnvelope(accounts: [aws])
            let rotatedBytes = try rotated.encoded()
            let writer = VaultWriter(location: location, queue: makeQueue())
            try await writer.restore(verbatim: rotatedBytes)
            let runs = CallCounter()

            await assertWriteError(.keyChanged) {
                _ = try await writer.commit(heldDEK: created.dek,
                                            VaultFiles.appending(self.github, dek: created.dek, runs: runs))
            }

            XCTAssertEqual(runs.value, 0)
            XCTAssertEqual(try Data(contentsOf: location.fileURL), rotatedBytes)
            XCTAssertFalse(VaultFiles.exists(VaultSidecar.backupURL(for: location.fileURL)))
        }
    }

    func testMissingFileIsReported() async throws {
        for access in allAccess {
            let location = try makeLocation(access)
            await assertWriteError(.missingFile(location.fileURL)) {
                _ = try await VaultWriter(location: location, queue: self.makeQueue()).read()
            }
        }
    }

    func testUnparseableFileIsReported() async throws {
        for access in allAccess {
            let location = try makeLocation(access)
            try VaultFiles.write(Data("{}".utf8), to: location.fileURL, mode: 0o600)
            await assertWriteError(.unparseable(location.fileURL)) {
                _ = try await VaultWriter(location: location, queue: self.makeQueue()).read()
            }
        }
    }

    func testFolderCommitKeepsAPrivateCopyOfThePreviousFile() async throws {
        let location = try makeLocation(.folder)
        let created = try await makeVault(at: location)
        let writer = VaultWriter(location: location, queue: makeQueue())
        let backupURL = VaultSidecar.backupURL(for: location.fileURL)

        let first = try await writer.commit(heldDEK: created.dek, VaultFiles.appending(github, dek: created.dek))

        XCTAssertEqual(try Data(contentsOf: backupURL), created.bytes)
        XCTAssertEqual(VaultFiles.mode(of: backupURL), 0o600)
        let backup = try await writer.readBackup()
        XCTAssertEqual(backup?.bytes, created.bytes)

        _ = try await writer.commit(heldDEK: created.dek, VaultFiles.appending(aws, dek: created.dek, policy: .keep))
        XCTAssertEqual(try Data(contentsOf: backupURL), created.bytes)

        _ = try await writer.commit(heldDEK: created.dek, VaultFiles.appending(aws, dek: created.dek))
        XCTAssertNotEqual(try Data(contentsOf: backupURL), first.bytes)
        let previous = try await writer.readBackup()
        XCTAssertEqual(try previous?.envelope.open(dek: created.dek), [aws, github])

        try await writer.deleteBackup()
        XCTAssertFalse(VaultFiles.exists(backupURL))
        let none = try await writer.readBackup()
        XCTAssertNil(none)
        try await writer.deleteBackup()
    }

    func testAnUnparseableBackupIsReported() async throws {
        let location = try makeLocation(.folder)
        _ = try await makeVault(at: location)
        let backupURL = VaultSidecar.backupURL(for: location.fileURL)
        try VaultFiles.write(Data("{}".utf8), to: backupURL, mode: 0o600)

        await assertWriteError(.unparseable(backupURL)) {
            _ = try await VaultWriter(location: location, queue: self.makeQueue()).readBackup()
        }
    }

    func testFileOnlyAccessHasNoBackupOrLock() async throws {
        let location = try makeLocation(.fileOnly)
        let created = try await makeVault(at: location)
        let writer = VaultWriter(location: location, queue: makeQueue())
        _ = try await writer.commit(heldDEK: created.dek, VaultFiles.appending(github, dek: created.dek))

        let backup = try await writer.readBackup()
        XCTAssertNil(backup)
        try await writer.deleteBackup()
        XCTAssertFalse(VaultFiles.exists(VaultSidecar.backupURL(for: location.fileURL)))
        XCTAssertFalse(VaultFiles.exists(VaultSidecar.lockURL(for: location.fileURL)))
    }

    func testASecondWriterWaitsForTheLockAndBothEditsSurvive() async throws {
        let location = try makeLocation(.folder)
        let created = try await makeVault(at: location)
        let dek = created.dek
        let vault = location.fileURL
        let second = VaultWriter(location: location, queue: makeQueue())
        let appendAWS = VaultFiles.appending(aws, dek: dek)
        let secondCommit = OSAllocatedUnfairLock<Task<CommittedVault, any Error>?>(initialState: nil)
        let lockWasHeld = CallCounter()
        let fileUnchangedWhileHeld = CallCounter()
        let createdBytes = created.bytes
        let first = VaultWriter(location: location, queue: makeQueue(), beforeReplace: {
            do throws(VaultLockError) {
                try VaultFileLock.acquire(for: vault, timeout: .milliseconds(50)).release()
            } catch {
                if case .timedOut = error { lockWasHeld.increment() }
            }
            let task = Task.detached { try await second.commit(heldDEK: dek, appendAWS) }
            secondCommit.withLock { $0 = task }
            Thread.sleep(forTimeInterval: 0.2)
            if (try? Data(contentsOf: vault)) == createdBytes { fileUnchangedWhileHeld.increment() }
        })

        let committed = try await first.commit(heldDEK: dek, VaultFiles.appending(github, dek: dek))
        let task = try XCTUnwrap(secondCommit.withLock { $0 })
        let later = try await task.value

        XCTAssertEqual(lockWasHeld.value, 1)
        XCTAssertEqual(fileUnchangedWhileHeld.value, 1)
        XCTAssertEqual(committed.accounts, [github])
        XCTAssertEqual(later.accounts, [github, aws])
        XCTAssertEqual(try Data(contentsOf: VaultSidecar.backupURL(for: vault)), committed.bytes)
        let final = try await second.read()
        XCTAssertEqual(try final.envelope.open(dek: dek), [aws, github])
    }

    func testFileOnlyCommitReappliesAfterAnOutsideEdit() async throws {
        let location = try makeLocation(.fileOnly)
        let created = try await makeVault(at: location)
        let dek = created.dek
        let vault = location.fileURL
        let outside = try created.envelope.reseal(accounts: [aws], dek: dek).encoded()
        let hooks = CallCounter()
        let runs = CallCounter()
        let writer = VaultWriter(location: location, queue: makeQueue(), beforeReplace: {
            if hooks.increment() == 1 { try outside.write(to: vault) }
        })

        let committed = try await writer.commit(heldDEK: dek, VaultFiles.appending(github, dek: dek, runs: runs))

        XCTAssertEqual(runs.value, 2)
        XCTAssertEqual(committed.accounts, [aws, github])
        let final = try await writer.read()
        XCTAssertEqual(final.bytes, committed.bytes)
        XCTAssertEqual(try final.envelope.open(dek: dek), [aws, github])
    }

    func testFileOnlyCommitGivesUpWhenTheFileKeepsChanging() async throws {
        let location = try makeLocation(.fileOnly)
        let created = try await makeVault(at: location)
        let dek = created.dek
        let vault = location.fileURL
        let envelope = created.envelope
        let accounts = [aws]
        let runs = CallCounter()
        let writer = VaultWriter(location: location, queue: makeQueue(), beforeReplace: {
            try envelope.reseal(accounts: accounts, dek: dek).encoded().write(to: vault)
        })

        await assertWriteError(.concurrentChange(vault)) {
            _ = try await writer.commit(heldDEK: dek, VaultFiles.appending(self.github, dek: dek, runs: runs))
        }

        XCTAssertEqual(runs.value, VaultWriter.maxTransformRuns)
        let final = try await writer.read()
        XCTAssertEqual(try final.envelope.open(dek: dek), [aws])
    }

    func testALinkedVaultReplacesTheTargetAndKeepsTheLink() async throws {
        for access in [VaultLocation.Access.folder, .fileOnly] {
            let root = try makeScratchDirectory()
            let realDir = root.appendingPathComponent("real", isDirectory: true)
            let linkDir = root.appendingPathComponent("links", isDirectory: true)
            try FileManager.default.createDirectory(at: realDir, withIntermediateDirectories: false)
            try FileManager.default.createDirectory(at: linkDir, withIntermediateDirectories: false)
            let target = realDir.appendingPathComponent("vault.json")
            let link = linkDir.appendingPathComponent("vault.json")
            let created = try await makeVault(at: VaultLocation(fileURL: target, access: .folder))
            try VaultFiles.symlink(link, to: target)
            let writer = VaultWriter(location: VaultLocation(fileURL: link, access: access), queue: makeQueue())

            let committed = try await writer.commit(heldDEK: created.dek, VaultFiles.appending(github, dek: created.dek))

            XCTAssertTrue(DurableFile.isSymlink(link))
            XCTAssertEqual(try Data(contentsOf: target), committed.bytes)
            XCTAssertEqual(VaultFiles.mode(of: target), 0o600)
            XCTAssertEqual(try FileManager.default.contentsOfDirectory(atPath: linkDir.path), ["vault.json"])
            if access == .folder {
                XCTAssertEqual(try Data(contentsOf: VaultSidecar.backupURL(for: target)), created.bytes)
                XCTAssertTrue(VaultFiles.exists(VaultSidecar.lockURL(for: target)))
            }
            XCTAssertEqual(VaultFiles.tempNames(in: realDir), [])
        }
    }

    func testCreateThroughALinkRefusesItsTarget() async throws {
        let dir = try makeScratchDirectory()
        let target = dir.appendingPathComponent("real.json")
        let link = dir.appendingPathComponent("vault.json")
        let created = try await makeVault(at: VaultLocation(fileURL: target, access: .folder))
        try VaultFiles.symlink(link, to: target)

        await assertWriteError(.alreadyExists(target)) {
            _ = try await VaultWriter(location: VaultLocation(fileURL: link, access: .folder), queue: self.makeQueue())
                .create(VaultCommit(envelope: created.envelope, accounts: [], dek: created.dek, backupPolicy: .keep))
        }
        XCTAssertEqual(try Data(contentsOf: target), created.bytes)
    }

    func testAVaultWithNoWaysToUnlockIsNotWritten() async throws {
        let location = try makeLocation(.folder)
        let (envelope, dek) = try Envelope.createUnwrapped(accounts: [])

        await assertWriteError(.noWraps) {
            _ = try await VaultWriter(location: location, queue: self.makeQueue())
                .create(VaultCommit(envelope: envelope, accounts: [], dek: dek, backupPolicy: .keep))
        }
        XCTAssertFalse(VaultFiles.exists(location.fileURL))
    }

    func testSidecarNamesFollowTheVaultPath() {
        let vault = URL(fileURLWithPath: "/tmp/x/vault.json")
        XCTAssertEqual(VaultSidecar.backupURL(for: vault).path, "/tmp/x/vault.json.bak")
        XCTAssertEqual(VaultSidecar.lockURL(for: vault).path, "/tmp/x/vault.json.lock")
        XCTAssertFalse(VaultLocation(fileURL: vault, access: .fileOnly).hasDirectoryAccess)
        XCTAssertTrue(VaultLocation(fileURL: vault, access: .folder).hasDirectoryAccess)
        XCTAssertTrue(VaultLocation(fileURL: vault, access: .container).hasDirectoryAccess)
    }

    func testLiveDependenciesWriteThroughTheVaultWriter() throws {
        let location = try makeLocation(.folder)
        let transaction = AppDependencies.live().makeTransaction(location)
        XCTAssertTrue(transaction is VaultWriter)
        XCTAssertEqual(transaction.location, location)
    }
}
