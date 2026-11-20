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

/// Another app that writes the vault file while a transition runs.
enum SecondWriter: Sendable, Equatable, CustomStringConvertible {
    /// Adds an account under the same DEK and keeps every wrap.
    case editsAccounts
    /// Rotates to a DEK of its own under a passphrase of its own, dropping this Mac's wrap.
    case rotatesDroppingThisMac

    static let passphrase = "the other app's passphrase"
    static let addedAccount = VaultFixture.account("z", issuer: "Other", account: "added")

    var description: String {
        switch self {
        case .editsAccounts: return "another app edits the accounts"
        case .rotatesDroppingThisMac: return "another app rotates without this Mac"
        }
    }
}

/// The state that outlives a simulated launch: the vault file and its
/// `.bak`, the keychain items, the defaults and the rollback directory. Each
/// `lifecycle` call stands for a new launch over the same state.
final class LifecycleRig: @unchecked Sendable {
    private struct OtherWrite {
        let kind: SecondWriter
        let bytes: Data
        let accounts: [Account]
    }

    let disk = FakeVaultDisk()
    let items = FaultyKeyItemStore()
    let defaults: UserDefaults
    let location: VaultLocation
    let rollbackDirectory: URL
    let isEnclaveAvailable: Bool
    let argon2 = TestArgon2()
    private let otherWrite = OSAllocatedUnfairLock<OtherWrite?>(initialState: nil)

    /// Creates `directory` so that the vault path resolves the same before and after the rollback copy lands in it.
    init(directory: URL, access: VaultLocation.Access, defaults: UserDefaults, isEnclaveAvailable: Bool = true) throws {
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        let resolved = directory.resolvingSymlinksInPath()
        location = VaultLocation(fileURL: resolved.appendingPathComponent("vault.json"), access: access)
        rollbackDirectory = resolved.appendingPathComponent("rollback")
        self.defaults = defaults
        self.isEnclaveAvailable = isEnclaveAvailable
        defaults.removeObject(forKey: SEKeyRegistry.defaultsKey)
        defaults.removeObject(forKey: MigrationRollbackStore.copiesKey)
        defaults.removeObject(forKey: MigrationRollbackStore.legacyVaultPathKey)
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
        if let url = rollback.pending(for: vaultPath), let bytes = try? Data(contentsOf: url) {
            copies.append(bytes)
        }
        return copies.contains { opens($0, passphrases: passphrases, to: accounts, keyring: truth) }
    }

    /// Whether the vault file itself opens to `accounts`.
    func fileOpens(passphrases: [String], accounts: [Account]) -> Bool {
        let truth = SEKeyring(items: items.base, enclave: enclave, registry: registry)
        return disk.vault.map { opens($0, passphrases: passphrases, to: accounts, keyring: truth) } ?? false
    }

    /// This Mac's current key that opens `envelope`, read without faults.
    func currentKey(_ envelope: Envelope) -> (id: SEKeyID, item: KeyItem, dek: Data)? {
        let truth = SEKeyring(items: items.base, enclave: enclave, registry: registry)
        guard case let .current(dek, id, _, _, _)? = try? truth.unwrap(envelope, context: nil),
              let item = try? items.base.read(account: id.account) else { return nil }
        return (id, item, dek)
    }

    /// The biometric setting the item of the key that opens `envelope` records.
    func keyRequiresBiometrics(_ envelope: Envelope) -> Bool? {
        currentKey(envelope)?.item.requiresBiometrics
    }

    /// The change another app makes to the file, opening it as `passphrases`
    /// or this Mac's key would. The bytes it writes are recorded.
    func secondWriter(_ kind: SecondWriter, passphrases: [String]) -> @Sendable (Data) -> Data {
        { [self] bytes in
            let truth = SEKeyring(items: items.base, enclave: enclave, registry: registry)
            guard let envelope = try? Envelope.decode(bytes),
                  let opened = deks(envelope, keyring: truth, passphrases: passphrases).lazy
                      .compactMap({ dek in (try? envelope.open(dek: dek)).map { (dek, $0) } }).first else {
                return bytes
            }
            let (dek, accounts) = opened
            let next: Envelope?
            let written: [Account]
            switch kind {
            case .editsAccounts:
                written = (accounts + [SecondWriter.addedAccount]).sorted { $0.id < $1.id }
                next = try? envelope.reseal(accounts: written, dek: dek)
            case .rotatesDroppingThisMac:
                written = accounts
                next = try? Envelope.create(accounts: accounts, passphrase: SecondWriter.passphrase, argon2: argon2)
            }
            guard let out = try? next?.encoded() else { return bytes }
            otherWrite.withLock { $0 = OtherWrite(kind: kind, bytes: out, accounts: written) }
            return out
        }
    }

    /// What another app wrote, when it did.
    var otherWriter: (kind: SecondWriter, bytes: Data)? {
        otherWrite.withLock { $0.map { (kind: $0.kind, bytes: $0.bytes) } }
    }

    /// The accounts the vault holds: the stored set, or the set another app wrote.
    var expectedAccounts: [Account] { otherWrite.withLock { $0?.accounts } ?? KeyFixture.storedAccounts }

    /// `passphrases`, with the one another app wrapped under when it did.
    func knownPassphrases(_ passphrases: [String]) -> [String] {
        let rotated = otherWrite.withLock { $0?.kind == .rotatesDroppingThisMac }
        return rotated ? passphrases + [SecondWriter.passphrase] : passphrases
    }

    func opens(_ envelope: Envelope, passphrase: String, to dek: Data) -> Bool {
        (try? envelope.recoverDEK(passphrase: passphrase, argon2: argon2)) == dek
    }

    private func opens(_ bytes: Data, passphrases: [String], to accounts: [Account], keyring: SEKeyring) -> Bool {
        guard let envelope = try? Envelope.decode(bytes) else { return false }
        return deks(envelope, keyring: keyring, passphrases: passphrases).contains {
            (try? envelope.open(dek: $0)) == accounts
        }
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
    /// A further expectation for the state where the run stopped, as a finding when it does not hold.
    var stopCheck: @Sendable (LifecycleRig, LifecycleFault) -> String? = { _, _ in nil }
}

enum LifecycleFault: Sendable, Equatable, CustomStringConvertible {
    case crash(after: MigrationStep)
    /// `number` counts the transition's own writes, from 1.
    case commit(FakeVaultDisk.CommitFault, number: Int)
    case readBack(FakeVaultDisk.ReadFault, restore: FakeVaultDisk.RestoreFault?)
    /// The keychain returns other bytes from the `committed` step on.
    case keyReadBackMismatch
    /// Another app writes the file between the commit and its read-back, and that read fails when `readFails`.
    case secondWriter(SecondWriter, readFails: Bool)

    var description: String {
        switch self {
        case .crash(let step): return "crash after \(step.rawValue)"
        case .commit(let fault, let number): return "\(fault) on write \(number)"
        case .readBack(let fault, let restore):
            return "read-back \(fault), restore \(restore.map { "\($0)" } ?? "ok")"
        case .keyReadBackMismatch: return "key read-back mismatch"
        case .secondWriter(let kind, let readFails): return "\(kind), read-back \(readFails ? "fails" : "ok")"
        }
    }

    /// Whether the other app's file ends the transition: its rotation leaves nothing to rerun.
    var supersedesTransition: Bool {
        if case .secondWriter(.rotatesDroppingThisMac, _) = self { return true }
        return false
    }
}

final class Findings: Sendable {
    private let list = OSAllocatedUnfairLock(initialState: [String]())

    func add(_ finding: String) { list.withLock { $0.append(finding) } }

    var all: [String] { list.withLock { $0 } }

    /// `allowsNoFile` holds for a flow that starts without a vault, until its file lands.
    func checkOpenable(_ rig: LifecycleRig, passphrases: [String], allowsNoFile: Bool, at point: String) {
        if allowsNoFile && rig.disk.vault == nil { return }
        if !rig.isOpenable(passphrases: rig.knownPassphrases(passphrases), accounts: rig.expectedAccounts) {
            add("nothing opens the vault \(point)")
        }
    }

    /// The other app's file stands: its bytes after a rotation, its accounts after an edit.
    func checkOtherWriterSurvives(_ rig: LifecycleRig, passphrases: [String], at point: String) {
        guard let written = rig.otherWriter else { return }
        switch written.kind {
        case .editsAccounts:
            if !rig.fileOpens(passphrases: passphrases, accounts: rig.expectedAccounts) {
                add("the other app's accounts are gone \(point)")
            }
        case .rotatesDroppingThisMac:
            if rig.disk.vault != written.bytes { add("the other app's file was overwritten \(point)") }
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
    /// every write it makes, every read-back fault, and another app writing
    /// before the read-back.
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
        for kind in [SecondWriter.editsAccounts, .rotatesDroppingThisMac] {
            out += [.secondWriter(kind, readFails: false), .secondWriter(kind, readFails: true)]
        }
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
        case .secondWriter(let kind, let readFails):
            rig.disk.injectWriterAfterNextCommit(rig.secondWriter(kind, passphrases: passphrases))
            if readFails { rig.disk.injectReadFaultAfterNextCommit(.fail) }
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
        if case .secondWriter = fault, rig.otherWriter == nil { found.add("the other app did not write") }
        found.checkOtherWriterSurvives(rig, passphrases: passphrases, at: "where the run stopped")
        if let finding = scenario.stopCheck(rig, fault) { found.add(finding) }

        rig.disk.clearFaults()
        rig.items.setFaults { $0 = FaultyKeyItemStore.Faults() }
        guard var state = await relaunch(rig, found, passphrases: passphrases, label: "after the relaunch") else {
            return found.all
        }
        if !fault.supersedesTransition, !(state.map { scenario.isDone(rig, $0, original) } ?? false) {
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
        if !fault.supersedesTransition, !scenario.isDone(rig, end, original) {
            found.add("the target state was not reached")
        }
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
        if rig.rollback.pending(for: rig.vaultPath) != nil { found.add("the rollback copy is left") }
        found.checkOpenable(rig, passphrases: scenario.passphrases, allowsNoFile: false, at: "at the end")
        found.checkOtherWriterSurvives(rig, passphrases: scenario.passphrases, at: "at the end")
        if !fault.supersedesTransition, let finding = scenario.finalCheck(rig, end, fault) { found.add(finding) }
    }

    /// Opens and reconciles as a launch does. The outer nil means the file
    /// did not open; the inner nil means there is no file.
    private func relaunch(_ rig: LifecycleRig, _ found: Findings, passphrases: [String],
                          label: String) async -> OpenState?? {
        let state: OpenState?
        do {
            state = try await rig.open(passphrases: rig.knownPassphrases(passphrases))
        } catch {
            found.add("the vault does not open \(label)")
            return nil
        }
        guard let state else { return .some(nil) }
        if state.opened.accounts != rig.expectedAccounts { found.add("the accounts changed \(label)") }
        await rig.lifecycle().reconcile(opened: state.opened, dek: state.dek)
        return state
    }

    private func makeRig() throws -> LifecycleRig {
        let dir = directory.appendingPathComponent(UUID().uuidString)
        return try LifecycleRig(directory: dir, access: access, defaults: defaults,
                            isEnclaveAvailable: scenario.isEnclaveAvailable)
    }
}
