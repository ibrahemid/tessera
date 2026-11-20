import Foundation
import os
import TesseraCore
@testable import Tessera

enum LifecycleRigError: Error, Equatable {
    case missingFile, doesNotOpen
}

/// A vault opened on this Mac and the DEK that opened it.
struct OpenState: Sendable {
    let opened: OpenedVault
    let dek: Data

    var envelope: Envelope { opened.snapshot.envelope }
}

/// The state that outlives a simulated launch: the vault file and its
/// `.bak`, the keychain items, the defaults and the rollback directory. Each
/// `lifecycle` call stands for a new launch over the same state.
final class LifecycleRig: @unchecked Sendable {
    let disk = FakeVaultDisk()
    let items = FaultyKeyItemStore()
    let defaults: UserDefaults
    let location: VaultLocation
    let rollbackDirectory: URL
    let isEnclaveAvailable: Bool
    let argon2 = TestArgon2()

    /// Creates `directory` so that the vault path resolves the same before and after the rollback copy lands in it.
    init(directory: URL, access: VaultLocation.Access, defaults: UserDefaults, isEnclaveAvailable: Bool = true) throws {
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        let resolved = directory.resolvingSymlinksInPath()
        location = VaultLocation(fileURL: resolved.appendingPathComponent("vault.json"), access: access)
        rollbackDirectory = resolved.appendingPathComponent("rollback")
        self.defaults = defaults
        self.isEnclaveAvailable = isEnclaveAvailable
        defaults.removeObject(forKey: SEKeyRegistry.defaultsKey)
        defaults.removeObject(forKey: MigrationRollbackStore.vaultPathKey)
    }

    var vaultPath: String { SEKeyRegistry.normalizedPath(location.fileURL.path) }
    var registry: SEKeyRegistry { SEKeyRegistry(defaults: defaults) }
    var enclave: SoftwareEnclaveKeyFactory {
        SoftwareEnclaveKeyFactory(deviceTag: KeyFixture.thisMac, isAvailable: isEnclaveAvailable)
    }
    var keyring: SEKeyring { SEKeyring(items: items, enclave: enclave, registry: registry) }
    var rollback: MigrationRollbackStore { MigrationRollbackStore(directory: rollbackDirectory, defaults: defaults) }
    var file: FakeVaultFile { FakeVaultFile(location: location, disk: disk) }

    func lifecycle(checkpoint: any MigrationCheckpoint = NoCheckpoint(),
                   isCurrent: @escaping @Sendable () -> Bool = { true }) -> KeyLifecycle {
        KeyLifecycle(transaction: file, keyring: keyring, rollback: rollback, argon2: argon2,
                     checkpoint: checkpoint, isCurrent: isCurrent)
    }

    func write(_ envelope: Envelope) throws {
        disk.setVault(try envelope.encoded())
    }

    /// Writes a vault whose Secure Enclave wrap is this Mac's legacy wrap,
    /// after a passphrase wrap when `passphrase` is set. Returns the DEK.
    @discardableResult
    func writeLegacyVault(passphrase: String? = nil) throws -> Data {
        var envelope: Envelope
        let dek: Data
        if let passphrase {
            envelope = try Envelope.create(accounts: KeyFixture.accounts, passphrase: passphrase, argon2: argon2)
            dek = try envelope.recoverDEK(passphrase: passphrase, argon2: argon2)
        } else {
            let made = try Envelope.createUnwrapped(accounts: KeyFixture.accounts)
            envelope = made.0
            dek = made.1
        }
        try envelope.addSecureEnclaveWrap(try KeyFixture.legacyWrap(dek: dek))
        try write(envelope)
        return dek
    }

    /// Writes a vault with one passphrase wrap. Returns the DEK.
    @discardableResult
    func writePassphraseVault(_ passphrase: String) throws -> Data {
        let envelope = try Envelope.create(accounts: KeyFixture.accounts, passphrase: passphrase, argon2: argon2)
        try write(envelope)
        return try envelope.recoverDEK(passphrase: passphrase, argon2: argon2)
    }

    /// Opens the file as a launch would: this Mac's key first, then each
    /// passphrase. Nil when there is no file.
    func open(passphrases: [String]) async throws -> OpenState? {
        let snapshot: VaultSnapshot
        do {
            snapshot = try await file.read()
        } catch VaultWriteError.missingFile {
            return nil
        }
        for dek in deks(snapshot.envelope, keyring: keyring, passphrases: passphrases) {
            if let accounts = try? snapshot.envelope.open(dek: dek) {
                return OpenState(opened: OpenedVault(snapshot: snapshot, accounts: accounts), dek: dek)
            }
        }
        throw LifecycleRigError.doesNotOpen
    }

    func openExisting(passphrases: [String] = []) async throws -> OpenState {
        guard let state = try await open(passphrases: passphrases) else { throw LifecycleRigError.missingFile }
        return state
    }

    /// Whether the file, or the rollback copy kept for it, opens on this Mac
    /// to `accounts` through a key, a legacy wrap or one of `passphrases`.
    func isOpenable(passphrases: [String], accounts: [Account]) -> Bool {
        let truth = SEKeyring(items: items.base, enclave: enclave, registry: registry)
        var copies: [Data] = []
        if let bytes = disk.vault { copies.append(bytes) }
        if let pending = rollback.pending(), pending.vaultPath == vaultPath,
           let bytes = try? Data(contentsOf: pending.url) {
            copies.append(bytes)
        }
        return copies.contains { bytes in
            guard let envelope = try? Envelope.decode(bytes) else { return false }
            return deks(envelope, keyring: truth, passphrases: passphrases).contains {
                (try? envelope.open(dek: $0)) == accounts
            }
        }
    }

    /// This Mac's current key that opens `envelope`, read without faults.
    func currentKey(_ envelope: Envelope) -> (id: SEKeyID, item: KeyItem, dek: Data)? {
        let truth = SEKeyring(items: items.base, enclave: enclave, registry: registry)
        guard case let .current(dek, id, _, _)? = try? truth.unwrap(envelope, context: nil),
              let item = try? items.base.read(account: id.account) else { return nil }
        return (id, item, dek)
    }

    /// The biometric setting of the key that opens `envelope`.
    func keyRequiresBiometrics(_ envelope: Envelope) -> Bool? {
        currentKey(envelope).flatMap { SoftwareEnclaveKeyFactory.requiresBiometrics($0.item.data) }
    }

    func opens(_ envelope: Envelope, passphrase: String, to dek: Data) -> Bool {
        (try? envelope.recoverDEK(passphrase: passphrase, argon2: argon2)) == dek
    }

    private func deks(_ envelope: Envelope, keyring: SEKeyring, passphrases: [String]) -> [Data] {
        var out: [Data] = []
        if let unwrapped = try? keyring.unwrap(envelope, context: nil) { out.append(unwrapped.dek) }
        out += passphrases.compactMap { try? envelope.recoverDEK(passphrase: $0, argon2: argon2) }
        return out
    }
}

/// A transition under test: how to reach its start state, how to run it, and
/// what its target state is.
struct LifecycleScenario: Sendable {
    let name: String
    /// Every passphrase the person knows for this vault while the transition runs.
    var passphrases: [String] = []
    var isEnclaveAvailable = true
    let prepare: @Sendable (LifecycleRig) async throws -> Void
    let run: @Sendable (KeyLifecycle, LifecycleRig, OpenState?) async throws -> Void
    /// Whether the vault is in the target state, given the DEK it held before the transition.
    let isDone: @Sendable (LifecycleRig, OpenState, Data?) -> Bool
    /// A further expectation for the end state, as a finding when it does not hold.
    var finalCheck: @Sendable (LifecycleRig, OpenState, LifecycleFault) -> String? = { _, _, _ in nil }
}

enum LifecycleFault: Sendable, Equatable, CustomStringConvertible {
    case crash(after: MigrationStep)
    /// `number` counts the transition's own writes, from 1.
    case commit(FakeVaultDisk.CommitFault, number: Int)
    case readBack(FakeVaultDisk.ReadFault, restore: FakeVaultDisk.RestoreFault?)
    /// The keychain returns other bytes from the `committed` step on.
    case keyReadBackMismatch

    var description: String {
        switch self {
        case .crash(let step): return "crash after \(step.rawValue)"
        case .commit(let fault, let number): return "\(fault) on write \(number)"
        case .readBack(let fault, let restore):
            return "read-back \(fault), restore \(restore.map { "\($0)" } ?? "ok")"
        case .keyReadBackMismatch: return "key read-back mismatch"
        }
    }
}

final class Findings: Sendable {
    private let list = OSAllocatedUnfairLock(initialState: [String]())

    func add(_ finding: String) { list.withLock { $0.append(finding) } }

    var all: [String] { list.withLock { $0 } }

    /// `allowsNoFile` holds for a flow that starts without a vault, until its file lands.
    func checkOpenable(_ rig: LifecycleRig, passphrases: [String], allowsNoFile: Bool, at point: String) {
        if allowsNoFile && rig.disk.vault == nil { return }
        if !rig.isOpenable(passphrases: passphrases, accounts: KeyFixture.storedAccounts) {
            add("nothing opens the vault \(point)")
        }
    }
}

/// Runs a scenario once per fault: a stop at that point, a relaunch over the
/// same state that opens and reconciles, and a rerun while the target state
/// is not reached. Each run gets a rig of its own.
struct LifecycleMatrix {
    let scenario: LifecycleScenario
    let access: VaultLocation.Access
    let defaults: UserDefaults
    let directory: URL

    /// A crash after every step a clean run reaches, every write fault on
    /// every write it makes, and every read-back fault.
    func faults() async throws -> [LifecycleFault] {
        let rig = try makeRig()
        try await scenario.prepare(rig)
        let start = try await rig.open(passphrases: scenario.passphrases)
        if let start { await rig.lifecycle().reconcile(opened: start.opened, dek: start.dek) }
        let before = rig.disk.commits
        let recorder = CrashingCheckpoint()
        try await scenario.run(rig.lifecycle(checkpoint: recorder), rig, start)
        let writes = rig.disk.commits - before
        var out = recorder.steps.map { LifecycleFault.crash(after: $0) }
        for number in (0..<writes).map({ $0 + 1 }) {
            out += FakeVaultDisk.CommitFault.allCases.map { LifecycleFault.commit($0, number: number) }
        }
        out += [.readBack(.crash, restore: nil), .readBack(.fail, restore: nil), .readBack(.fail, restore: .fail),
                .readBack(.fail, restore: .crashAfterWrite), .keyReadBackMismatch]
        return out
    }

    func findings(for fault: LifecycleFault) async throws -> [String] {
        let rig = try makeRig()
        try await scenario.prepare(rig)
        let found = Findings()
        let passphrases = scenario.passphrases

        let start = try await rig.open(passphrases: passphrases)
        if let start { await rig.lifecycle().reconcile(opened: start.opened, dek: start.dek) }
        let original = start?.dek
        let startsEmpty = start == nil

        var crashAt: MigrationStep?
        switch fault {
        case .crash(let step):
            crashAt = step
        case .commit(let commitFault, let number):
            rig.disk.injectCommitFault(commitFault, onCommit: rig.disk.commits + number)
        case .readBack(let readFault, let restore):
            rig.disk.injectReadFaultAfterNextCommit(readFault)
            if let restore { rig.disk.injectRestoreFault(restore) }
        case .keyReadBackMismatch:
            break
        }
        let breaksKeyReads = fault == .keyReadBackMismatch
        let checkpoint = CrashingCheckpoint(crashAt: crashAt) { step in
            found.checkOpenable(rig, passphrases: passphrases, allowsNoFile: startsEmpty, at: "after \(step.rawValue)")
            if breaksKeyReads && step == .committed { rig.items.setFaults { $0.readData = Data("other".utf8) } }
        }
        do {
            try await scenario.run(rig.lifecycle(checkpoint: checkpoint), rig, start)
        } catch {}
        found.checkOpenable(rig, passphrases: passphrases, allowsNoFile: startsEmpty, at: "where the run stopped")

        rig.disk.clearFaults()
        rig.items.setFaults { $0 = FaultyKeyItemStore.Faults() }
        guard var state = await relaunch(rig, found, passphrases: passphrases, label: "after the relaunch") else {
            return found.all
        }
        if !(state.map { scenario.isDone(rig, $0, original) } ?? false) {
            do {
                try await scenario.run(rig.lifecycle(), rig, state)
            } catch {
                found.add("the rerun failed: \(error)")
            }
            guard let next = await relaunch(rig, found, passphrases: passphrases, label: "after the rerun") else {
                return found.all
            }
            state = next
        }
        guard let end = state else {
            found.add("no vault file at the end")
            return found.all
        }
        await checkEndState(rig, end, original: original, fault: fault, found: found)
        return found.all
    }

    private func checkEndState(_ rig: LifecycleRig, _ end: OpenState, original: Data?, fault: LifecycleFault,
                               found: Findings) async {
        if !scenario.isDone(rig, end, original) { found.add("the target state was not reached") }
        if !end.envelope.secureEnclaveWraps(.legacy).isEmpty { found.add("a legacy wrap is left in the vault") }
        if let backup = try? await rig.file.readBackup(), !backup.envelope.secureEnclaveWraps(.legacy).isEmpty {
            found.add("a legacy wrap is left in the backup")
        }
        let referenced = Set(end.envelope.secureEnclaveKeyIDs.map(\.account))
        let held = Set(rig.items.accounts)
        if !held.isSubset(of: referenced) { found.add("a key item the vault does not reference is left") }
        if Set(rig.registry.ids(for: rig.vaultPath).map(\.account)) != held {
            found.add("the registry does not match the key items")
        }
        if rig.rollback.pending() != nil { found.add("the rollback copy is left") }
        found.checkOpenable(rig, passphrases: scenario.passphrases, allowsNoFile: false, at: "at the end")
        if let finding = scenario.finalCheck(rig, end, fault) { found.add(finding) }
    }

    /// Opens and reconciles as a launch does. The outer nil means the file
    /// did not open; the inner nil means there is no file.
    private func relaunch(_ rig: LifecycleRig, _ found: Findings, passphrases: [String],
                          label: String) async -> OpenState?? {
        let state: OpenState?
        do {
            state = try await rig.open(passphrases: passphrases)
        } catch {
            found.add("the vault does not open \(label)")
            return nil
        }
        guard let state else { return .some(nil) }
        if state.opened.accounts != KeyFixture.storedAccounts { found.add("the accounts changed \(label)") }
        await rig.lifecycle().reconcile(opened: state.opened, dek: state.dek)
        return state
    }

    private func makeRig() throws -> LifecycleRig {
        let dir = directory.appendingPathComponent(UUID().uuidString)
        return try LifecycleRig(directory: dir, access: access, defaults: defaults,
                            isEnclaveAvailable: scenario.isEnclaveAvailable)
    }
}
