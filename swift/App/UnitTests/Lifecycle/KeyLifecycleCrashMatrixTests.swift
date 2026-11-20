import Foundation
import TesseraCore
import XCTest
@testable import Tessera

/// Every transition, stopped after every step and by every write and
/// read-back fault, then relaunched over the same state: the vault opens at
/// every point, the run reaches its target, no legacy wrap or unreferenced
/// key is left, and the accounts never change.
final class KeyLifecycleCrashMatrixTests: XCTestCase {
    func testLegacyToCurrentWithFolderAccess() async throws { try await runMatrix(Scenarios.migrate, .folder) }
    func testLegacyToCurrentWithFileAccess() async throws { try await runMatrix(Scenarios.migrate, .fileOnly) }

    func testLegacyToCurrentWithPassphraseWithFolderAccess() async throws {
        try await runMatrix(Scenarios.migrateWithPassphrase, .folder)
    }

    func testLegacyToCurrentWithPassphraseWithFileAccess() async throws {
        try await runMatrix(Scenarios.migrateWithPassphrase, .fileOnly)
    }

    func testLegacyToPendingWithFolderAccess() async throws { try await runMatrix(Scenarios.migrateKeepingDEK, .folder) }
    func testLegacyToPendingWithFileAccess() async throws { try await runMatrix(Scenarios.migrateKeepingDEK, .fileOnly) }

    func testPendingToCurrentWithFolderAccess() async throws { try await runMatrix(Scenarios.completePending, .folder) }
    func testPendingToCurrentWithFileAccess() async throws { try await runMatrix(Scenarios.completePending, .fileOnly) }

    func testRotateWithFolderAccess() async throws { try await runMatrix(Scenarios.rotate, .folder) }
    func testRotateWithFileAccess() async throws { try await runMatrix(Scenarios.rotate, .fileOnly) }

    func testRotateWithoutEnclaveWithFolderAccess() async throws {
        try await runMatrix(Scenarios.rotateWithoutEnclave, .folder)
    }

    func testRotateWithoutEnclaveWithFileAccess() async throws {
        try await runMatrix(Scenarios.rotateWithoutEnclave, .fileOnly)
    }

    func testReplaceWithFolderAccess() async throws {
        try await runMatrix(Scenarios.replace(preferenceKey: await Self.preferenceKey()), .folder)
    }

    func testReplaceWithFileAccess() async throws {
        try await runMatrix(Scenarios.replace(preferenceKey: await Self.preferenceKey()), .fileOnly)
    }

    func testAddWithFolderAccess() async throws { try await runMatrix(Scenarios.add, .folder) }
    func testAddWithFileAccess() async throws { try await runMatrix(Scenarios.add, .fileOnly) }

    func testCreateWithFolderAccess() async throws { try await runMatrix(Scenarios.create, .folder) }
    func testCreateWithFileAccess() async throws { try await runMatrix(Scenarios.create, .fileOnly) }

    private static func preferenceKey() async -> String {
        await MainActor.run { AppModel.biometricsPrefKey }
    }

    private func runMatrix(_ scenario: LifecycleScenario, _ access: VaultLocation.Access) async throws {
        let matrix = LifecycleMatrix(scenario: scenario, access: access, defaults: try makeThrowawayDefaults(),
                                     directory: try makeTemporaryDirectory("tessera-matrix"))
        let faults = try await matrix.faults()
        XCTAssertTrue(faults.contains(.crash(after: .committed)), scenario.name)
        for fault in faults {
            let found = try await matrix.findings(for: fault)
            XCTAssertEqual(found, [], "\(scenario.name), \(access), \(fault)")
        }
    }
}

private enum Phrase {
    static let recovery = "correct horse battery staple"
    static let next = "a different recovery passphrase"
    static let appKey = "device wrap passphrase"
}

private enum Scenarios {
    static let migrate = LifecycleScenario(
        name: "legacy to current",
        prepare: { rig in try rig.writeLegacyVault() },
        run: { lifecycle, _, start in
            let start = try started(start)
            _ = try await lifecycle.migrateLegacy(opened: start.opened, heldDEK: start.dek, requireBiometrics: false,
                                                  context: nil)
        },
        isDone: { rig, state, original in isRotated(rig, state, from: original) })

    static let migrateWithPassphrase = LifecycleScenario(
        name: "legacy to current with the passphrase",
        passphrases: [Phrase.recovery],
        prepare: { rig in try rig.writeLegacyVault(passphrase: Phrase.recovery) },
        run: { lifecycle, _, start in
            let start = try started(start)
            _ = try await lifecycle.migrateLegacy(opened: start.opened, heldDEK: start.dek, passphrase: Phrase.recovery,
                                                  requireBiometrics: false, context: nil)
        },
        isDone: { rig, state, original in
            isRotated(rig, state, from: original) && rig.opens(state.envelope, passphrase: Phrase.recovery, to: state.dek)
        })

    static let migrateKeepingDEK = LifecycleScenario(
        name: "legacy to pending",
        passphrases: [Phrase.recovery],
        prepare: { rig in try rig.writeLegacyVault(passphrase: Phrase.recovery) },
        run: { lifecycle, _, start in
            let start = try started(start)
            _ = try await lifecycle.migrateLegacy(opened: start.opened, heldDEK: start.dek, requireBiometrics: false,
                                                  context: nil)
        },
        isDone: { rig, state, original in
            guard state.envelope.secureEnclaveWraps(.legacy).isEmpty, let key = rig.currentKey(state.envelope) else {
                return false
            }
            return key.item.isRotationPending && key.dek == original
                && rig.opens(state.envelope, passphrase: Phrase.recovery, to: key.dek)
        })

    static let completePending = LifecycleScenario(
        name: "pending to current",
        passphrases: [Phrase.recovery],
        prepare: { rig in
            try rig.writeLegacyVault(passphrase: Phrase.recovery)
            let start = try await rig.openExisting(passphrases: [Phrase.recovery])
            let outcome = try await rig.lifecycle().migrateLegacy(opened: start.opened, heldDEK: start.dek,
                                                                  requireBiometrics: false, context: nil)
            guard outcome.state == .rotationPending else { throw LifecycleRigError.doesNotOpen }
        },
        run: { lifecycle, _, start in
            let start = try started(start)
            _ = try await lifecycle.completePendingRotation(opened: start.opened, heldDEK: start.dek,
                                                            passphrase: Phrase.recovery, requireBiometrics: false,
                                                            context: nil)
        },
        isDone: { rig, state, original in
            isRotated(rig, state, from: original) && rig.opens(state.envelope, passphrase: Phrase.recovery, to: state.dek)
        })

    static let rotate = LifecycleScenario(
        name: "rotate",
        passphrases: [Phrase.recovery, Phrase.next],
        prepare: { rig in
            try rig.writeLegacyVault(passphrase: Phrase.recovery)
            let start = try await rig.openExisting(passphrases: [Phrase.recovery])
            _ = try await rig.lifecycle().migrateLegacy(opened: start.opened, heldDEK: start.dek,
                                                        passphrase: Phrase.recovery, requireBiometrics: false,
                                                        context: nil)
        },
        run: { lifecycle, _, start in
            let start = try started(start)
            _ = try await lifecycle.rotate(opened: start.opened, heldDEK: start.dek, newPassphrase: Phrase.next,
                                           requireBiometrics: false, context: nil)
        },
        isDone: { rig, state, original in
            isRotated(rig, state, from: original) && rig.opens(state.envelope, passphrase: Phrase.next, to: state.dek)
                && !rig.opens(state.envelope, passphrase: Phrase.recovery, to: state.dek)
        })

    static let rotateWithoutEnclave = LifecycleScenario(
        name: "rotate without a Secure Enclave",
        passphrases: [Phrase.recovery, Phrase.next, Phrase.appKey],
        isEnclaveAvailable: false,
        prepare: { rig in
            var envelope = try Envelope.create(accounts: KeyFixture.accounts, passphrase: Phrase.recovery,
                                               argon2: rig.argon2)
            let dek = try envelope.recoverDEK(passphrase: Phrase.recovery, argon2: rig.argon2)
            try envelope.addPassphraseWrap(dek: dek, passphrase: Phrase.appKey, argon2: rig.argon2)
            try rig.write(envelope)
        },
        run: { lifecycle, rig, start in
            let start = try started(start)
            let salt = try start.envelope.recoverDEKAndSalt(passphrase: Phrase.appKey, argon2: rig.argon2).salt
            _ = try await lifecycle.rotate(opened: start.opened, heldDEK: start.dek, newPassphrase: Phrase.next,
                                           appKey: AppKeyWrap(passphrase: Phrase.appKey, salt: salt),
                                           requireBiometrics: false, context: nil)
        },
        isDone: { rig, state, original in
            state.dek != original && state.envelope.wraps.count == 2
                && rig.opens(state.envelope, passphrase: Phrase.appKey, to: state.dek)
                && rig.opens(state.envelope, passphrase: Phrase.next, to: state.dek)
                && !rig.opens(state.envelope, passphrase: Phrase.recovery, to: state.dek)
        })

    /// Turns Touch ID on for a vault whose only path is this Mac's key. A
    /// stop between the file write and the preference write leaves the
    /// preference behind the key.
    static func replace(preferenceKey: String) -> LifecycleScenario {
        LifecycleScenario(
            name: "replace",
            prepare: { rig in
                try rig.writeLegacyVault()
                let start = try await rig.openExisting()
                _ = try await rig.lifecycle().migrateLegacy(opened: start.opened, heldDEK: start.dek,
                                                            requireBiometrics: false, context: nil)
                rig.defaults.set(false, forKey: preferenceKey)
            },
            run: { lifecycle, rig, start in
                let start = try started(start)
                _ = try await lifecycle.replaceKey(opened: start.opened, heldDEK: start.dek, requireBiometrics: true,
                                                   context: nil) { rig.defaults.set($0, forKey: preferenceKey) }
            },
            isDone: { rig, state, original in
                rig.keyRequiresBiometrics(state.envelope) == true && state.dek == original
                    && rig.currentKey(state.envelope)?.item.isRotationPending == false
            },
            finalCheck: { rig, state, fault in
                let preference = rig.defaults.object(forKey: preferenceKey) as? Bool
                let key = rig.keyRequiresBiometrics(state.envelope)
                if fault == .commit(.crashAfterRename, number: 1) {
                    return preference == false && key == true ? nil : "the preference was expected to lag the key"
                }
                return preference == key ? nil
                    : "preference \(String(describing: preference)) differs from the key \(String(describing: key))"
            })
    }

    static let add = LifecycleScenario(
        name: "add",
        passphrases: [Phrase.recovery],
        prepare: { rig in try rig.writePassphraseVault(Phrase.recovery) },
        run: { lifecycle, _, start in
            let start = try started(start)
            _ = try await lifecycle.addSEWrap(opened: start.opened, heldDEK: start.dek, requireBiometrics: false,
                                              context: nil)
        },
        isDone: { rig, state, original in
            guard let key = rig.currentKey(state.envelope) else { return false }
            return !key.item.isRotationPending && key.dek == original
                && rig.opens(state.envelope, passphrase: Phrase.recovery, to: key.dek)
        })

    static let create = LifecycleScenario(
        name: "create",
        prepare: { _ in },
        run: { lifecycle, _, _ in
            _ = try await lifecycle.createVault(accounts: KeyFixture.accounts, requireBiometrics: false, context: nil)
        },
        isDone: { rig, state, _ in
            guard let key = rig.currentKey(state.envelope) else { return false }
            return !key.item.isRotationPending && state.envelope.wraps.count == 1
        })

    private static func started(_ start: OpenState?) throws -> OpenState {
        guard let start else { throw LifecycleRigError.missingFile }
        return start
    }

    /// This Mac's key opens the vault without the pending marker, under a DEK other than `original`.
    private static func isRotated(_ rig: LifecycleRig, _ state: OpenState, from original: Data?) -> Bool {
        guard state.envelope.secureEnclaveWraps(.legacy).isEmpty, let key = rig.currentKey(state.envelope) else {
            return false
        }
        return !key.item.isRotationPending && key.dek == state.dek && key.dek != original
    }
}
