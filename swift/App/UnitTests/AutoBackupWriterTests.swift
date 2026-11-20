import Darwin
import Foundation
import os
import TesseraCore
import XCTest
@testable import Tessera

private final class RemovalLog: Sendable {
    private let state = OSAllocatedUnfairLock(initialState: [String]())

    var paths: [String] { state.withLock { $0 } }

    func remover(failing: Set<String> = [], code: Int32 = EACCES, vanished: Set<String> = []) -> AutoBackupWriter.Remove {
        { path in
            self.state.withLock { $0.append(path) }
            let name = URL(fileURLWithPath: path).lastPathComponent
            if failing.contains(name) { return code }
            if vanished.contains(name) { _ = AutoBackupWriter.systemRemove(path) }
            return AutoBackupWriter.systemRemove(path)
        }
    }
}

final class AutoBackupWriterTests: XCTestCase {
    private let passphrase = "pw"

    private func seal(_ issuer: String) throws -> Envelope {
        try Envelope.create(accounts: [VaultFixture.account("a", issuer: issuer)], passphrase: passphrase,
                            argon2: TestArgon2())
    }

    private func issuer(at url: URL) throws -> String {
        let accounts = try Envelope.decode(Data(contentsOf: url)).open(passphrase: passphrase, argon2: TestArgon2())
        return try XCTUnwrap(accounts.first).issuer
    }

    private func touch(_ url: URL) throws {
        try VaultFiles.write(Data("{}".utf8), to: url, mode: 0o600)
    }

    private func names(in dir: URL) -> [String] {
        ((try? FileManager.default.contentsOfDirectory(atPath: dir.path)) ?? []).sorted()
    }

    private func success(_ at: Date) -> Result<BackupResult, AutoBackupError> {
        .success(BackupResult(at: at, fileName: BackupRotation.fileName(for: at), failure: nil))
    }

    func testStartsANewFileAtTheUTCDayBoundary() async throws {
        let dir = try makeScratchDirectory()
        let writer = AutoBackupWriter()
        let late = BackupDates.at(2026, 11, 20, 23, 59, 59, millisecond: 999)
        let next = late.addingTimeInterval(0.001)

        let first = try await writer.write(seal("first"), rotated: false, at: late, folder: dir, vault: nil)
        let second = try await writer.write(seal("second"), rotated: false, at: next, folder: dir, vault: nil)

        XCTAssertEqual(first, success(late))
        XCTAssertEqual(second, success(next))
        let want = ["tessera-backup-20261120.json", "tessera-backup-20261121.json"]
        XCTAssertEqual(names(in: dir), want)
        XCTAssertEqual(try issuer(at: dir.appendingPathComponent(want[0])), "first")
        XCTAssertEqual(try issuer(at: dir.appendingPathComponent(want[1])), "second")
    }

    func testReplacesTheSameDayFile() async throws {
        let dir = try makeScratchDirectory()
        let writer = AutoBackupWriter()
        let morning = BackupDates.at(2026, 11, 20)

        _ = try await writer.write(seal("first"), rotated: false, at: morning, folder: dir, vault: nil)
        let result = try await writer.write(seal("second"), rotated: false, at: morning.addingTimeInterval(23 * 3600),
                                        folder: dir, vault: nil)

        XCTAssertEqual(result, success(morning.addingTimeInterval(23 * 3600)))
        XCTAssertEqual(names(in: dir), ["tessera-backup-20261120.json"])
        let file = dir.appendingPathComponent("tessera-backup-20261120.json")
        XCTAssertEqual(try issuer(at: file), "second")
        XCTAssertEqual(VaultFiles.mode(of: file), 0o600)
        XCTAssertEqual(VaultFiles.tempNames(in: dir), [])
    }

    func testKeepsOnlyPassphraseWraps() async throws {
        let dir = try makeScratchDirectory()
        var envelope = try seal("x")
        let secureEnclave = try VaultWrap.secureEnclave(keyID: Data(repeating: 1, count: 16),
                                                        nonce: Data(repeating: 2, count: XChaCha.nonceSize),
                                                        ct: Data(repeating: 3, count: 48))
        envelope.wraps = [secureEnclave, KeyFixture.unknownWrap()] + envelope.wraps
        let at = BackupDates.at(2026, 11, 20, 9)

        let result = await AutoBackupWriter().write(envelope, rotated: false, at: at, folder: dir, vault: nil)

        XCTAssertEqual(result, success(at))
        let file = dir.appendingPathComponent(BackupRotation.fileName(for: at))
        XCTAssertEqual(try Envelope.decode(Data(contentsOf: file)).wrapTypes, ["passphrase"])
        XCTAssertEqual(envelope.wrapTypes, ["secure-enclave", "hardware-key", "passphrase"])
        XCTAssertEqual(try issuer(at: file), "x")
        XCTAssertEqual(VaultFiles.mode(of: file), 0o600)
    }

    func testWithoutAPassphraseWrapWritesNothing() async throws {
        let dir = try makeScratchDirectory()
        let (envelope, _) = try VaultFiles.makeEnvelope(accounts: [VaultFixture.account("a", issuer: "x")])

        let result = await AutoBackupWriter().write(envelope, rotated: false, at: BackupDates.at(2026, 11, 20),
                                                    folder: dir, vault: nil)

        XCTAssertEqual(result, .failure(.needsPassphrase))
        XCTAssertEqual(names(in: dir), [])
    }

    func testKeepsTheThirtyNewestExactMatches() async throws {
        let dir = try makeScratchDirectory()
        let start = BackupDates.at(2026, 1, 1, 12)
        let dated = (0..<35).map { BackupDates.name(BackupDates.adding(days: $0, to: start)) }
        for name in dated { try touch(dir.appendingPathComponent(name)) }
        let others = ["tessera-backup-2025010.json", "tessera-backup-20250101.json.bak",
                      "Tessera-backup-20250101.json", "tessera-backup-20250101.json~",
                      "tessera-backup-2025010a.json", "notes.txt"]
        for name in others { try touch(dir.appendingPathComponent(name)) }
        let folderNamedLikeABackup = dir.appendingPathComponent("tessera-backup-20240101.json", isDirectory: true)
        try FileManager.default.createDirectory(at: folderNamedLikeABackup, withIntermediateDirectories: false)
        let log = RemovalLog()
        let at = BackupDates.at(2026, 3, 1, 9)

        let result = try await AutoBackupWriter(remove: log.remover()).write(seal("x"), rotated: false, at: at,
                                                                          folder: dir, vault: nil)

        XCTAssertEqual(result, success(at))
        XCTAssertEqual(log.paths, dated[..<6].reversed().map { dir.appendingPathComponent($0).path })
        let kept = names(in: dir).filter { BackupRotation.date(fromName: $0) != nil && $0 != "tessera-backup-20240101.json" }
        XCTAssertEqual(kept.count, BackupRotation.retention)
        XCTAssertEqual(kept.last, "tessera-backup-20260301.json")
        XCTAssertEqual(kept.first, dated[6])
        for name in others + ["tessera-backup-20240101.json"] {
            XCTAssertTrue(VaultFiles.exists(dir.appendingPathComponent(name)), name)
        }
    }

    func testAfterARotationDeletesEveryOtherBackup() async throws {
        let dir = try makeScratchDirectory()
        for name in ["tessera-backup-20261101.json", "tessera-backup-20261119.json", "tessera-backup-20991231.json",
                     "keep-me.json"] {
            try touch(dir.appendingPathComponent(name))
        }
        let at = BackupDates.at(2026, 11, 20, 10)
        let writer = AutoBackupWriter()
        _ = try await writer.write(seal("old"), rotated: false, at: at, folder: dir, vault: nil)
        XCTAssertEqual(names(in: dir).count, 5)
        let log = RemovalLog()

        let result = try await AutoBackupWriter(remove: log.remover()).write(seal("rotated"), rotated: true,
                                                                          at: at.addingTimeInterval(3600),
                                                                          folder: dir, vault: nil)

        XCTAssertEqual(result, success(at.addingTimeInterval(3600)))
        XCTAssertEqual(names(in: dir), ["keep-me.json", "tessera-backup-20261120.json"])
        XCTAssertEqual(log.paths.count, 3)
        XCTAssertEqual(try issuer(at: dir.appendingPathComponent("tessera-backup-20261120.json")), "rotated")
    }

    func testUnusableFolderWritesNothing() async throws {
        let dir = try makeScratchDirectory()
        let missing = dir.appendingPathComponent("missing", isDirectory: true)
        let file = dir.appendingPathComponent("file")
        try touch(file)
        let writer = AutoBackupWriter()

        let toMissing = try await writer.write(seal("x"), rotated: false, at: Date(), folder: missing, vault: nil)
        let toFile = try await writer.write(seal("x"), rotated: false, at: Date(), folder: file, vault: nil)

        XCTAssertEqual(toMissing, .failure(.folderUnavailable))
        XCTAssertEqual(toFile, .failure(.folderUnavailable))
        XCTAssertFalse(VaultFiles.exists(missing))
        XCTAssertEqual(names(in: dir), ["file"])
    }

    func testRemovedFolderGivesAFailure() async throws {
        let dir = try makeScratchDirectory()
        let folder = dir.appendingPathComponent("backups", isDirectory: true)
        try FileManager.default.createDirectory(at: folder, withIntermediateDirectories: false)
        let writer = AutoBackupWriter()
        let at = BackupDates.at(2026, 11, 20)
        let first = try await writer.write(seal("x"), rotated: false, at: at, folder: folder, vault: nil)
        XCTAssertEqual(first, success(at))
        try FileManager.default.removeItem(at: folder)

        let result = try await writer.write(seal("y"), rotated: false, at: at, folder: folder, vault: nil)

        XCTAssertEqual(result, .failure(.folderUnavailable))
        XCTAssertFalse(VaultFiles.exists(folder))
    }

    func testRetentionCountsOnlyRealDates() async throws {
        let dir = try makeScratchDirectory()
        let day = BackupDates.at(2026, 11, 20, 12)
        let real = (1...10).map { BackupDates.name(BackupDates.adding(days: -$0, to: day)) }
        for name in real { try touch(dir.appendingPathComponent(name)) }
        for i in 0..<(BackupRotation.retention - 1) {
            try touch(dir.appendingPathComponent(String(format: "tessera-backup-9999%04d.json", Int32(i))))
        }

        let result = try await AutoBackupWriter().write(seal("Now"), rotated: false, at: day, folder: dir, vault: nil)

        XCTAssertEqual(result, success(day))
        XCTAssertEqual(real.filter { !VaultFiles.exists(dir.appendingPathComponent($0)) }, [])
    }

    func testRetentionNeitherCountsNorPrunesUnrankedNames() async throws {
        let dir = try makeScratchDirectory()
        let day = BackupDates.at(2026, 11, 20, 12)
        let unranked = ["tessera-backup-20261121.json", "tessera-backup-20991231.json", "tessera-backup-20260230.json",
                        "tessera-backup-20261300.json", "tessera-backup-00000000.json"]
        for name in unranked { try touch(dir.appendingPathComponent(name)) }
        let real = (1...(BackupRotation.retention + 2)).map { BackupDates.name(BackupDates.adding(days: -$0, to: day)) }
        for name in real { try touch(dir.appendingPathComponent(name)) }
        let log = RemovalLog()

        let result = try await AutoBackupWriter(remove: log.remover()).write(seal("Now"), rotated: false, at: day,
                                                                          folder: dir, vault: nil)

        XCTAssertEqual(result, success(day))
        XCTAssertEqual(log.paths.sorted(), real.suffix(3).map { dir.appendingPathComponent($0).path }.sorted())
        for name in unranked {
            XCTAssertTrue(VaultFiles.exists(dir.appendingPathComponent(name)), name)
        }
    }

    func testRotationCleanupRemovesUnrankedNames() async throws {
        let dir = try makeScratchDirectory()
        let day = BackupDates.at(2026, 11, 20, 12)
        for name in ["tessera-backup-20991231.json", "tessera-backup-20261300.json",
                     BackupDates.name(BackupDates.adding(days: -1, to: day)), "notes.json"] {
            try touch(dir.appendingPathComponent(name))
        }

        let result = try await AutoBackupWriter().write(seal("Now"), rotated: true, at: day, folder: dir, vault: nil)

        XCTAssertEqual(result, success(day))
        XCTAssertEqual(names(in: dir), ["notes.json", BackupDates.name(day)])
    }

    func testSparesTheVaultItBacksUp() async throws {
        for rotated in [false, true] {
            let dir = try makeScratchDirectory()
            let day = BackupDates.at(2026, 11, 20, 12)
            let vault = dir.appendingPathComponent(BackupDates.name(BackupDates.at(2026, 8, 20, 12)))
            let envelope = try seal("Restored")
            try VaultFiles.write(try envelope.encoded(), to: vault, mode: 0o600)
            for i in 1...BackupRotation.retention {
                try touch(dir.appendingPathComponent(BackupDates.name(BackupDates.adding(days: -i, to: day))))
            }

            let result = await AutoBackupWriter().write(envelope, rotated: rotated, at: day, folder: dir, vault: vault)

            XCTAssertEqual(result, success(day), "rotated \(rotated)")
            XCTAssertTrue(VaultFiles.exists(vault), "rotated \(rotated)")
            XCTAssertEqual(try issuer(at: vault), "Restored")
            XCTAssertEqual(names(in: dir).count, rotated ? 2 : BackupRotation.retention + 1, "rotated \(rotated)")
        }
    }

    func testSparesTheVaultThroughAnotherName() async throws {
        let dir = try makeScratchDirectory()
        let elsewhere = try makeScratchDirectory()
        let day = BackupDates.at(2026, 11, 20, 12)
        let vault = elsewhere.appendingPathComponent("vault.json")
        let envelope = try seal("Linked")
        try VaultFiles.write(try envelope.encoded(), to: vault, mode: 0o600)
        let alias = dir.appendingPathComponent(BackupDates.name(BackupDates.adding(days: -2, to: day)))
        guard link(vault.path, alias.path) == 0 else { throw XCTSkip("hard links unavailable: \(errno)") }

        let result = await AutoBackupWriter().write(envelope, rotated: true, at: day, folder: dir, vault: vault)

        XCTAssertEqual(result, success(day))
        XCTAssertTrue(VaultFiles.exists(alias))
        XCTAssertEqual(try issuer(at: alias), "Linked")
    }

    func testWritesInOrderLeaveTheLaterEnvelope() async throws {
        let dir = try makeScratchDirectory()
        let writer = AutoBackupWriter()
        let at = BackupDates.at(2026, 11, 20, 8)
        let earlier = try seal("earlier")
        let later = try seal("later")

        let results = await Task {
            [await writer.write(earlier, rotated: false, at: at, folder: dir, vault: nil),
             await writer.write(later, rotated: false, at: at.addingTimeInterval(1), folder: dir, vault: nil)]
        }.value

        XCTAssertEqual(results, [success(at), success(at.addingTimeInterval(1))])
        XCTAssertEqual(names(in: dir), [BackupDates.name(at)])
        XCTAssertEqual(try issuer(at: dir.appendingPathComponent(BackupDates.name(at))), "later")
    }

    func testFailedPruneKeepsTheDayFile() async throws {
        let dir = try makeScratchDirectory()
        let day = BackupDates.at(2026, 11, 20, 12)
        let older = ["tessera-backup-20261101.json", "tessera-backup-20261102.json"]
        for name in older { try touch(dir.appendingPathComponent(name)) }
        let log = RemovalLog()

        let writer = AutoBackupWriter(remove: log.remover(failing: Set(older)))
        let result = try await writer.write(seal("Now"), rotated: true, at: day, folder: dir, vault: nil)

        XCTAssertEqual(result, .failure(.prune(failed: 2)))
        XCTAssertEqual(log.paths.count, 2)
        XCTAssertEqual(try issuer(at: dir.appendingPathComponent(BackupDates.name(day))), "Now")
        XCTAssertEqual(names(in: dir), (older + [BackupDates.name(day)]).sorted())
    }

    func testOlderBackupAlreadyGoneCountsAsRemoved() async throws {
        let dir = try makeScratchDirectory()
        let day = BackupDates.at(2026, 11, 20, 12)
        let older = "tessera-backup-20261101.json"
        try touch(dir.appendingPathComponent(older))

        let writer = AutoBackupWriter(remove: RemovalLog().remover(vanished: [older]))
        let result = try await writer.write(seal("Now"), rotated: true, at: day, folder: dir, vault: nil)

        XCTAssertEqual(result, success(day))
        XCTAssertEqual(names(in: dir), [BackupDates.name(day)])
    }
}
