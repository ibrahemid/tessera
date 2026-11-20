import Foundation
import os
import TesseraCore
import XCTest
@testable import Tessera

final class KeyLifecycleTests: XCTestCase {
    private let recovery = "correct horse battery staple"
    private let next = "a different recovery passphrase"
    private let appKey = "device wrap passphrase"
    private var rig: LifecycleRig!

    override func setUpWithError() throws {
        rig = try makeRig(.folder)
    }

    private func makeRig(_ access: VaultLocation.Access, isEnclaveAvailable: Bool = true) throws -> LifecycleRig {
        try LifecycleRig(directory: try makeTemporaryDirectory("tessera-lifecycle"), access: access,
                         defaults: try makeThrowawayDefaults(), isEnclaveAvailable: isEnclaveAvailable)
    }

    // MARK: - Foreign wraps

    func testPendingMigrationKeepsForeignWrapsAndRotationReportsThem() async throws {
        var envelope = try Envelope.create(accounts: KeyFixture.accounts, passphrase: recovery, argon2: rig.argon2)
        let dek = try envelope.recoverDEK(passphrase: recovery, argon2: rig.argon2)
        let otherMac = try KeyFixture.legacyWrap(dek: dek, device: KeyFixture.otherMac)
        let foreign = try KeyFixture.foreignCurrentWrap()
        let unknown = KeyFixture.unknownWrap()
        try envelope.addSecureEnclaveWrap(try KeyFixture.legacyWrap(dek: dek))
        try envelope.addSecureEnclaveWrap(otherMac)
        try envelope.addSecureEnclaveWrap(foreign)
        envelope.wraps.append(unknown)
        try rig.write(envelope)

        let start = try await rig.openExisting()
        let pending = try await rig.lifecycle().migrateLegacy(opened: start.opened, heldDEK: start.dek,
                                                              requireBiometrics: false, context: nil)
        XCTAssertEqual(pending.state, .rotationPending)
        let kept = pending.committed.envelope
        XCTAssertEqual(kept.wraps.count, 5)
        for wrap in [envelope.wraps[0], otherMac, foreign, unknown] { XCTAssertTrue(kept.containsWrap(wrap)) }
        XCTAssertTrue(kept.secureEnclaveWraps(.legacy).allSatisfy { $0.isSameWrap(as: otherMac) })
        let key = try XCTUnwrap(rig.currentKey(kept))
        XCTAssertTrue(key.item.isRotationPending)
        XCTAssertEqual(key.dek, dek)

        let middle = try await rig.openExisting()
        let rotated = try await rig.lifecycle().completePendingRotation(opened: middle.opened, heldDEK: middle.dek,
                                                                        passphrase: recovery,
                                                                        requireBiometrics: false, context: nil)
        XCTAssertEqual(rotated.state, .current)
        XCTAssertEqual(rotated.removedWraps, [.secureEnclave, .secureEnclave, .unknown("hardware-key")])
        XCTAssertEqual(rotated.committed.envelope.wraps.count, 2)
        XCTAssertEqual(rig.items.accounts.count, 1)
        XCTAssertNotEqual(rotated.committed.dek, dek)
    }

    func testPassphraselessVaultKeepsAnotherMacsWrap() async throws {
        var (envelope, dek) = try Envelope.createUnwrapped(accounts: KeyFixture.accounts)
        let otherMac = try KeyFixture.legacyWrap(dek: dek, device: KeyFixture.otherMac)
        try envelope.addSecureEnclaveWrap(try KeyFixture.legacyWrap(dek: dek))
        try envelope.addSecureEnclaveWrap(otherMac)
        try rig.write(envelope)

        let start = try await rig.openExisting()
        let outcome = try await rig.lifecycle().migrateLegacy(opened: start.opened, heldDEK: start.dek,
                                                              requireBiometrics: false, context: nil)
        XCTAssertEqual(outcome.state, .rotationPending)
        XCTAssertEqual(outcome.committed.envelope.wraps.count, 2)
        XCTAssertTrue(outcome.committed.envelope.containsWrap(otherMac))
        XCTAssertEqual(rig.currentKey(outcome.committed.envelope)?.dek, dek)
    }

    // MARK: - Aborts

    func testRotationAbortsWhenTheLegacyWrapVanished() async throws {
        let dek = try rig.writeLegacyVault()
        try await assertAbortsWhenTheLegacyWrapVanishes(dek: dek)
    }

    func testPendingMigrationAbortsWhenTheLegacyWrapVanished() async throws {
        let dek = try rig.writeLegacyVault(passphrase: recovery)
        try await assertAbortsWhenTheLegacyWrapVanishes(dek: dek)
    }

    private func assertAbortsWhenTheLegacyWrapVanishes(dek: Data, file: StaticString = #filePath,
                                                       line: UInt = #line) async throws {
        let rig = try XCTUnwrap(self.rig)
        let start = try await rig.openExisting()
        let legacy = try XCTUnwrap(start.envelope.secureEnclaveWraps(.legacy).first)
        var tampered = start.envelope
        try tampered.addPassphraseWrap(dek: dek, passphrase: next, argon2: rig.argon2)
        XCTAssertTrue(try tampered.removeWrap(legacy))
        let tamperedBytes = try tampered.encoded()
        let commits = rig.disk.commits
        let checkpoint = CrashingCheckpoint { step in
            if step == .wrapVerified { rig.disk.setVault(tamperedBytes) }
        }

        do {
            _ = try await rig.lifecycle(checkpoint: checkpoint).migrateLegacy(opened: start.opened, heldDEK: start.dek,
                                                                              requireBiometrics: false, context: nil)
            XCTFail("the migration went on", file: file, line: line)
        } catch {
            XCTAssertEqual(error as? MigrationError, .legacyWrapChanged, file: file, line: line)
        }
        XCTAssertEqual(rig.disk.vault, tamperedBytes, file: file, line: line)
        XCTAssertEqual(rig.disk.commits, commits, file: file, line: line)
        XCTAssertEqual(rig.items.accounts, [], file: file, line: line)
        XCTAssertEqual(rig.registry.ids(for: rig.vaultPath), [], file: file, line: line)
    }

    func testVerificationFailureRestoresTheFileByteForByte() async throws {
        let rig = try XCTUnwrap(self.rig)
        try rig.writeLegacyVault()
        let before = try XCTUnwrap(rig.disk.vault)
        let start = try await rig.openExisting()
        let checkpoint = CrashingCheckpoint { step in
            if step == .committed { rig.items.setFaults { $0.readData = Data("other".utf8) } }
        }

        do {
            _ = try await rig.lifecycle(checkpoint: checkpoint).migrateLegacy(opened: start.opened, heldDEK: start.dek,
                                                                              requireBiometrics: false, context: nil)
            XCTFail("the migration went on")
        } catch {
            XCTAssertEqual(error as? MigrationError, .verificationFailed(.readBackVerified))
        }
        XCTAssertEqual(rig.disk.vault, before)
        XCTAssertEqual(rig.items.accounts, [])
        XCTAssertEqual(rig.registry.ids(for: rig.vaultPath), [])
    }

    func testRestoreFailureKeepsTheKeyAndTheRollbackCopy() async throws {
        try rig.writeLegacyVault()
        let before = try XCTUnwrap(rig.disk.vault)
        let start = try await rig.openExisting()
        rig.disk.injectReadFaultAfterNextCommit(.fail)
        rig.disk.injectRestoreFault(.fail)

        do {
            _ = try await rig.lifecycle().migrateLegacy(opened: start.opened, heldDEK: start.dek,
                                                        requireBiometrics: false, context: nil)
            XCTFail("the migration went on")
        } catch {
            XCTAssertEqual(error as? MigrationError, .restoreFailed)
        }
        let pending = try XCTUnwrap(rig.rollback.pending())
        XCTAssertEqual(pending.vaultPath, rig.vaultPath)
        XCTAssertEqual(try Data(contentsOf: pending.url), before)
        XCTAssertEqual(rig.items.accounts.count, 1)
        let reopened = try await rig.openExisting()
        XCTAssertNotNil(rig.currentKey(reopened.envelope))
    }

    func testStaleFlowsStopBeforeWriting() async throws {
        try rig.writeLegacyVault()
        let start = try await rig.openExisting()
        let stale = rig.lifecycle(isCurrent: { false })
        do {
            _ = try await stale.migrateLegacy(opened: start.opened, heldDEK: start.dek, requireBiometrics: false,
                                              context: nil)
            XCTFail("the migration went on")
        } catch {
            XCTAssertTrue(error is CancellationError, "\(error)")
        }
        XCTAssertEqual(rig.disk.commits, 0)
        XCTAssertEqual(rig.items.accounts, [])
        XCTAssertEqual(rig.registry.ids(for: rig.vaultPath), [])

        let empty = try makeRig(.folder)
        do {
            _ = try await empty.lifecycle(isCurrent: { false }).createVault(accounts: KeyFixture.accounts,
                                                                            requireBiometrics: false, context: nil)
            XCTFail("the create went on")
        } catch {
            XCTAssertTrue(error is CancellationError, "\(error)")
        }
        XCTAssertNil(empty.disk.vault)
        XCTAssertEqual(empty.disk.commits, 0)
        XCTAssertEqual(empty.items.accounts, [])
        XCTAssertEqual(empty.registry.ids(for: empty.vaultPath), [])
    }

    // MARK: - Rotation without a Secure Enclave

    func testRotationWithoutEnclaveKeepsTheDeviceWrapAndOneRecoveryWrap() async throws {
        let rig = try makeRig(.folder, isEnclaveAvailable: false)
        let second = "a second recovery passphrase"
        var envelope = try Envelope.create(accounts: KeyFixture.accounts, passphrase: recovery, argon2: rig.argon2)
        let dek = try envelope.recoverDEK(passphrase: recovery, argon2: rig.argon2)
        try envelope.addPassphraseWrap(dek: dek, passphrase: appKey, argon2: rig.argon2)
        try envelope.addPassphraseWrap(dek: dek, passphrase: second, argon2: rig.argon2)
        try rig.write(envelope)

        let start = try await rig.openExisting(passphrases: [appKey])
        let salt = try start.envelope.recoverDEKAndSalt(passphrase: appKey, argon2: rig.argon2).salt
        let outcome = try await rig.lifecycle().rotate(opened: start.opened, heldDEK: start.dek, newPassphrase: next,
                                                       appKey: AppKeyWrap(passphrase: appKey, salt: salt),
                                                       requireBiometrics: false, context: nil)

        let rotated = outcome.committed.envelope
        XCTAssertEqual(rotated.wraps.count, 2)
        XCTAssertEqual(rotated.wraps.map(\.type), ["passphrase", "passphrase"])
        XCTAssertEqual(outcome.removedWraps, [.passphrase])
        XCTAssertNotEqual(outcome.committed.dek, dek)
        XCTAssertEqual(outcome.appKeyWrapSalt, rotated.wraps[0].salt)
        let viaAppKey = try rotated.recoverDEKAndSalt(passphrase: appKey, argon2: rig.argon2)
        XCTAssertEqual(viaAppKey.dek, outcome.committed.dek)
        XCTAssertEqual(viaAppKey.salt, rotated.wraps[0].salt)
        let viaNext = try rotated.recoverDEKAndSalt(passphrase: next, argon2: rig.argon2)
        XCTAssertEqual(viaNext.dek, outcome.committed.dek)
        XCTAssertEqual(viaNext.salt, rotated.wraps[1].salt)
        for old in [recovery, second] {
            XCTAssertThrowsError(try rotated.recoverDEK(passphrase: old, argon2: rig.argon2))
        }
        XCTAssertEqual(rig.items.accounts, [])
    }

    // MARK: - Create

    func testCreateWritesAVaultOpenedByTheNewKey() async throws {
        let outcome = try await rig.lifecycle().createVault(accounts: KeyFixture.accounts, requireBiometrics: true,
                                                            context: nil)
        XCTAssertEqual(outcome.state, .current)
        XCTAssertNotEqual(KeyFixture.accounts, KeyFixture.storedAccounts)
        XCTAssertEqual(outcome.committed.accounts, KeyFixture.storedAccounts)
        let opened = try await rig.openExisting()
        XCTAssertEqual(opened.opened.accounts, KeyFixture.storedAccounts)
        XCTAssertEqual(opened.envelope.wraps.count, 1)
        let key = try XCTUnwrap(rig.currentKey(opened.envelope))
        XCTAssertEqual(rig.keyRequiresBiometrics(opened.envelope), true)
        XCTAssertFalse(key.item.isRotationPending)
        XCTAssertEqual(rig.registry.ids(for: rig.vaultPath), [key.id])
        XCTAssertNil(rig.disk.backup)
    }

    func testCreateOverAnExistingFileLeavesItAndRemovesTheKey() async throws {
        try rig.writePassphraseVault(recovery)
        let before = try XCTUnwrap(rig.disk.vault)
        do {
            _ = try await rig.lifecycle().createVault(accounts: KeyFixture.accounts, requireBiometrics: false,
                                                      context: nil)
            XCTFail("the create went on")
        } catch {
            XCTAssertEqual(error as? VaultWriteError, .alreadyExists(rig.location.fileURL))
        }
        XCTAssertEqual(rig.disk.vault, before)
        XCTAssertEqual(rig.items.accounts, [])
        XCTAssertEqual(rig.registry.ids(for: rig.vaultPath), [])
    }

    // MARK: - Replace

    private func prepareCurrentVault() async throws -> (OpenState, SEKeyID) {
        try rig.writeLegacyVault()
        let legacy = try await rig.openExisting()
        _ = try await rig.lifecycle().migrateLegacy(opened: legacy.opened, heldDEK: legacy.dek,
                                                    requireBiometrics: false, context: nil)
        let start = try await rig.openExisting()
        return (start, try XCTUnwrap(rig.currentKey(start.envelope)).id)
    }

    func testReplaceVerificationFailureRecordsBothSettingsAndRestores() async throws {
        let rig = try XCTUnwrap(self.rig)
        let (start, oldID) = try await prepareCurrentVault()
        let before = try XCTUnwrap(rig.disk.vault)
        let recorded = OSAllocatedUnfairLock(initialState: [Bool]())
        let checkpoint = CrashingCheckpoint { step in
            if step == .committed { rig.items.setFaults { $0.readData = Data("other".utf8) } }
        }

        do {
            _ = try await rig.lifecycle(checkpoint: checkpoint).replaceKey(
                opened: start.opened, heldDEK: start.dek, requireBiometrics: true, context: nil
            ) { value in recorded.withLock { $0.append(value) } }
            XCTFail("the replace went on")
        } catch {
            XCTAssertEqual(error as? MigrationError, .verificationFailed(.readBackVerified))
        }
        XCTAssertEqual(recorded.withLock { $0 }, [true, false])
        XCTAssertEqual(rig.disk.vault, before)
        XCTAssertEqual(rig.items.accounts, [oldID.account])
        XCTAssertEqual(rig.registry.ids(for: rig.vaultPath), [oldID])
    }

    func testReplaceKeepsTheDEKAndRefreshesTheBackup() async throws {
        let (start, oldID) = try await prepareCurrentVault()
        let recorded = OSAllocatedUnfairLock(initialState: [Bool]())
        let outcome = try await rig.lifecycle().replaceKey(opened: start.opened, heldDEK: start.dek,
                                                           requireBiometrics: true, context: nil) { value in
            recorded.withLock { $0.append(value) }
        }
        XCTAssertEqual(outcome.state, .current)
        XCTAssertEqual(recorded.withLock { $0 }, [true])
        let opened = try await rig.openExisting()
        let key = try XCTUnwrap(rig.currentKey(opened.envelope))
        XCTAssertNotEqual(key.id, oldID)
        XCTAssertEqual(key.dek, start.dek)
        XCTAssertEqual(rig.keyRequiresBiometrics(opened.envelope), true)
        XCTAssertEqual(rig.items.accounts, [key.id.account])
        XCTAssertEqual(rig.registry.ids(for: rig.vaultPath), [key.id])
        let backup = try Envelope.decode(try XCTUnwrap(rig.disk.backup))
        let newWrap = try XCTUnwrap(opened.envelope.secureEnclaveWraps(.current).first)
        XCTAssertTrue(backup.containsWrap(newWrap))
        XCTAssertNil(rig.rollback.pending())
    }

    // MARK: - Reconcile

    func testReconcileKeepsAnUnreadableBackupAndAnotherVaultsRollbackCopy() async throws {
        try rig.writePassphraseVault(recovery)
        rig.disk.setBackup(Data("not a vault".utf8))
        let other = rig.location.fileURL.deletingLastPathComponent().appendingPathComponent("other.json").path
        try rig.rollback.save(Data("other".utf8), for: other)

        let start = try await rig.openExisting(passphrases: [recovery])
        let report = await rig.lifecycle().reconcile(opened: start.opened, dek: start.dek)
        XCTAssertEqual(report, ReconcileReport())
        XCTAssertEqual(rig.disk.backup, Data("not a vault".utf8))
        XCTAssertEqual(rig.rollback.pending()?.vaultPath, SEKeyRegistry.normalizedPath(other))
    }

    func testReconcileDropsStaleCopiesAndUnreferencedKeys() async throws {
        try rig.writePassphraseVault(recovery)
        let otherDEK = try Envelope.create(accounts: KeyFixture.accounts, passphrase: next, argon2: rig.argon2)
        rig.disk.setBackup(try otherDEK.encoded())
        try rig.rollback.save(try XCTUnwrap(rig.disk.vault), for: rig.vaultPath)
        let orphan = try rig.keyring.createKey(for: rig.vaultPath, requireBiometrics: false, isRotationPending: false,
                                               context: nil)

        let start = try await rig.openExisting(passphrases: [recovery])
        let report = await rig.lifecycle().reconcile(opened: start.opened, dek: start.dek)
        XCTAssertEqual(report, ReconcileReport(discardedRollback: true, deletedBackup: true, sweptKeys: [orphan.id]))
        XCTAssertNil(rig.disk.backup)
        XCTAssertNil(rig.rollback.pending())
        XCTAssertEqual(rig.items.accounts, [])
        XCTAssertEqual(rig.registry.ids(for: rig.vaultPath), [])
    }

    func testReconcileDropsABackupHoldingALegacyWrap() async throws {
        var envelope = try Envelope.create(accounts: KeyFixture.accounts, passphrase: recovery, argon2: rig.argon2)
        let dek = try envelope.recoverDEK(passphrase: recovery, argon2: rig.argon2)
        try rig.write(envelope)
        try envelope.addSecureEnclaveWrap(try KeyFixture.legacyWrap(dek: dek))
        rig.disk.setBackup(try envelope.encoded())

        let start = try await rig.openExisting(passphrases: [recovery])
        let report = await rig.lifecycle().reconcile(opened: start.opened, dek: start.dek)
        XCTAssertEqual(report, ReconcileReport(deletedBackup: true))
        XCTAssertNil(rig.disk.backup)
    }

    func testReconcileWithFileAccessLeavesTheBackup() async throws {
        let rig = try makeRig(.fileOnly)
        try rig.writePassphraseVault(recovery)
        let stale = try Envelope.create(accounts: KeyFixture.accounts, passphrase: next, argon2: rig.argon2).encoded()
        rig.disk.setBackup(stale)

        let start = try await rig.openExisting(passphrases: [recovery])
        let report = await rig.lifecycle().reconcile(opened: start.opened, dek: start.dek)
        XCTAssertEqual(report, ReconcileReport())
        XCTAssertEqual(rig.disk.backup, stale)
    }
}
