import Foundation
import LocalAuthentication
import os
import TesseraCore

/// Moves a vault between key states. Every flow verifies the new path in
/// memory before it commits, re-reads and opens the committed file through
/// the new path before it drops anything old, and keeps a rollback copy until
/// then, so a stop at any step leaves the vault openable.
struct KeyLifecycle: Sendable {
    private static let passphraseType = "passphrase"
    private static let secureEnclaveType = "secure-enclave"

    let transaction: any VaultTransaction
    let keyring: SEKeyring
    let rollback: MigrationRollbackStore
    let argon2: any Argon2idProvider
    let checkpoint: any MigrationCheckpoint
    /// False once the flow's result would no longer be wanted (the vault locked since it began).
    let isCurrent: @Sendable () -> Bool

    init(transaction: any VaultTransaction, keyring: SEKeyring, rollback: MigrationRollbackStore,
         argon2: any Argon2idProvider, checkpoint: any MigrationCheckpoint = NoCheckpoint(),
         isCurrent: @escaping @Sendable () -> Bool = { true }) {
        self.transaction = transaction
        self.keyring = keyring
        self.rollback = rollback
        self.argon2 = argon2
        self.checkpoint = checkpoint
        self.isCurrent = isCurrent
    }

    private var vaultPath: String { SEKeyRegistry.normalizedPath(transaction.location.fileURL.path) }

    // MARK: - Transitions

    /// Replaces the legacy wraps from this Mac. Rotates to a fresh DEK in the
    /// same write when every other wrap can be re-created (no passphrase wrap
    /// it cannot re-create and no wrap of another Mac or unknown type, or the
    /// passphrase is in hand); otherwise keeps the DEK and marks the new key
    /// pending.
    func migrateLegacy(opened: OpenedVault, heldDEK: Data, passphrase: String? = nil, appKey: AppKeyWrap? = nil,
                       requireBiometrics: Bool, context: LAContext?) async throws -> MigrationOutcome {
        let envelope = opened.snapshot.envelope
        let local = envelope.secureEnclaveWraps(.legacy).filter(keyring.isLegacyWrapFromThisMac)
        guard !local.isEmpty else { throw SEKeyError.noWrap }
        var openedSalt: String?
        if let passphrase { openedSalt = try passphraseSalt(envelope, passphrase: passphrase, heldDEK: heldDEK) }
        let keepsOthers = envelope.wraps.contains { wrap in
            if Self.isPassphrase(wrap) { return wrap.salt != appKey?.salt }
            return !local.contains { $0.isSameWrap(as: wrap) }
        }
        if passphrase == nil && keepsOthers {
            return try await migrateKeepingDEK(opened: opened, heldDEK: heldDEK, local: local,
                                               requireBiometrics: requireBiometrics, context: context)
        }
        return try await rotateToFreshKey(opened: opened, heldDEK: heldDEK, replacingSalt: openedSalt,
                                          newPassphrase: passphrase, appKey: appKey, localLegacy: local,
                                          requireBiometrics: requireBiometrics, legacyGuard: local, context: context)
    }

    /// Finishes a pending rotation with the recovery passphrase, which keeps its value.
    func completePendingRotation(opened: OpenedVault, heldDEK: Data, passphrase: String, appKey: AppKeyWrap? = nil,
                                 requireBiometrics: Bool, context: LAContext?) async throws -> MigrationOutcome {
        let salt = try passphraseSalt(opened.snapshot.envelope, passphrase: passphrase, heldDEK: heldDEK)
        return try await rotateToFreshKey(opened: opened, heldDEK: heldDEK, replacingSalt: salt,
                                          newPassphrase: passphrase, appKey: appKey,
                                          localLegacy: localLegacyWraps(opened.snapshot.envelope),
                                          requireBiometrics: requireBiometrics, legacyGuard: [], context: context)
    }

    /// Rotates to a fresh DEK and key, with `newPassphrase` in place of the
    /// first recovery passphrase wrap. Without a Secure Enclave no key is made.
    func rotate(opened: OpenedVault, heldDEK: Data, newPassphrase: String, appKey: AppKeyWrap? = nil,
                requireBiometrics: Bool, context: LAContext?) async throws -> MigrationOutcome {
        let envelope = opened.snapshot.envelope
        let recovery = envelope.wraps.first { Self.isPassphrase($0) && $0.salt != appKey?.salt }
        return try await rotateToFreshKey(opened: opened, heldDEK: heldDEK, replacingSalt: recovery?.salt,
                                          newPassphrase: newPassphrase, appKey: appKey,
                                          localLegacy: localLegacyWraps(envelope),
                                          requireBiometrics: requireBiometrics, legacyGuard: [], context: context)
    }

    /// Puts a key made with `requireBiometrics` in place of this Mac's key,
    /// keeping the DEK. The key item records the setting.
    func replaceKey(opened: OpenedVault, heldDEK: Data, requireBiometrics: Bool,
                    context: LAContext?) async throws -> MigrationOutcome {
        let envelope = opened.snapshot.envelope
        let old = try keyring.localKeys(in: envelope)
        guard let first = old.first else { throw SEKeyError.noWrap }
        let isPending = first.item.isRotationPending
        let oldWraps = old.map(\.wrap)
        let id = SEKeyID.random()

        let newWrap = try beforeCommit(newKey: id) {
            try saveRollback(opened.snapshot)
            let key = try provision(id, requireBiometrics: requireBiometrics, isRotationPending: isPending,
                                    context: context)
            let wrap = try keyring.makeWrap(dek: heldDEK, key: key)
            var candidate = envelope
            try Self.replace(oldWraps, with: wrap, in: &candidate)
            try verifyPayload(candidate, dek: heldDEK, accounts: opened.accounts)
            try checkpoint.reached(.wrapVerified)
            return wrap
        }

        let (committed, preCommit) = try await commit(heldDEK: heldDEK, newKey: id, marker: newWrap, dek: heldDEK,
                                                      fallback: opened.snapshot.bytes) { fresh in
            var next = fresh.snapshot.envelope
            try Self.replace(oldWraps, with: newWrap, in: &next)
            return VaultCommit(envelope: next, accounts: fresh.accounts, dek: heldDEK, backupPolicy: .copyCurrent)
        }
        try checkpoint.reached(.committed)
        try await verifyReadBack(committed, preCommit: preCommit, newKey: id) { snapshot in
            try expectCurrent(snapshot, id: id, dek: heldDEK, isPending: isPending, context: context)
        }

        var latest = committed
        if transaction.location.hasDirectoryAccess {
            do {
                latest = try await transaction.commit(heldDEK: heldDEK) { fresh in
                    VaultCommit(envelope: fresh.snapshot.envelope, accounts: fresh.accounts, dek: heldDEK,
                                backupPolicy: .copyCurrent)
                }
            } catch where !checkpoint.isInterruption(error) {}
        }
        try checkpoint.reached(.backupHandled)
        try deleteOldKeys(old.map(\.id))
        try discardRollback()
        return MigrationOutcome(committed: latest, state: isPending ? .rotationPending : .current,
                                removedWraps: [], appKeyWrapSalt: nil)
    }

    /// Adds a wrap for a new key on this Mac to a vault that has none, keeping the DEK.
    func addSEWrap(opened: OpenedVault, heldDEK: Data, requireBiometrics: Bool,
                   context: LAContext?) async throws -> MigrationOutcome {
        let envelope = opened.snapshot.envelope
        let id = SEKeyID.random()
        let newWrap = try beforeCommit(newKey: id) {
            try saveRollback(opened.snapshot)
            let key = try provision(id, requireBiometrics: requireBiometrics, isRotationPending: false,
                                    context: context)
            let wrap = try keyring.makeWrap(dek: heldDEK, key: key)
            var candidate = envelope
            try candidate.addSecureEnclaveWrap(wrap)
            try verifyPayload(candidate, dek: heldDEK, accounts: opened.accounts)
            try checkpoint.reached(.wrapVerified)
            return wrap
        }

        let (committed, preCommit) = try await commit(heldDEK: heldDEK, newKey: id, marker: newWrap, dek: heldDEK,
                                                      fallback: opened.snapshot.bytes) { fresh in
            var next = fresh.snapshot.envelope
            try next.addSecureEnclaveWrap(newWrap)
            return VaultCommit(envelope: next, accounts: fresh.accounts, dek: heldDEK, backupPolicy: .copyCurrent)
        }
        try checkpoint.reached(.committed)
        try await verifyReadBack(committed, preCommit: preCommit, newKey: id) { snapshot in
            try expectCurrent(snapshot, id: id, dek: heldDEK, isPending: false, context: context)
        }
        try discardRollback()
        return MigrationOutcome(committed: committed, state: .current, removedWraps: [], appKeyWrapSalt: nil)
    }

    /// Creates a vault whose only wrap is for a new key on this Mac. When the
    /// read-back fails, the file goes while it still holds the bytes written,
    /// and the key with it.
    func createVault(accounts: [Account], requireBiometrics: Bool, context: LAContext?) async throws -> MigrationOutcome {
        let id = SEKeyID.random()
        let (envelope, dek, stored) = try beforeCommit(newKey: id) {
            let key = try provision(id, requireBiometrics: requireBiometrics, isRotationPending: false,
                                    context: context)
            var (envelope, dek) = try Envelope.createUnwrapped(accounts: accounts)
            let stored = try Self.inPayloadOrder(accounts)
            try envelope.addSecureEnclaveWrap(try keyring.makeWrap(dek: dek, key: key))
            try verifyPayload(envelope, dek: dek, accounts: stored)
            try checkpoint.reached(.wrapVerified)
            return (envelope, dek, stored)
        }
        guard isCurrent() else {
            removeKey(id)
            throw CancellationError()
        }
        let committed: CommittedVault
        do {
            committed = try await transaction.create(VaultCommit(envelope: envelope, accounts: stored, dek: dek,
                                                                 backupPolicy: .keep))
        } catch {
            if checkpoint.isInterruption(error) { throw error }
            do {
                let snapshot = try await transaction.read()
                if !envelope.wraps.allSatisfy(snapshot.envelope.containsWrap) { removeKey(id) }
            } catch VaultWriteError.missingFile {
                removeKey(id)
            } catch {}
            throw error
        }
        try checkpoint.reached(.committed)
        do {
            let snapshot = try await transaction.read()
            try expectCurrent(snapshot, id: id, dek: dek, isPending: false, context: context)
            let opened = try snapshot.envelope.open(dek: dek)
            if snapshot.bytes == committed.bytes, opened != stored {
                throw MigrationError.verificationFailed(.readBackVerified)
            }
        } catch {
            if checkpoint.isInterruption(error) { throw error }
            try await discardCreatedVault(committed, newKey: id)
        }
        try checkpoint.reached(.readBackVerified)
        return MigrationOutcome(committed: committed, state: .current, removedWraps: [], appKeyWrapSalt: nil)
    }

    /// Runs after every verified open, before any transition: drops this
    /// vault's rollback copy, a backup that holds a legacy wrap or does not
    /// open under `dek` (with folder access; an unreadable backup stays), and
    /// the keys registered to this vault that its file no longer references.
    @discardableResult
    func reconcile(opened: OpenedVault, dek: Data) async -> ReconcileReport {
        var report = ReconcileReport()
        if rollback.pending(for: vaultPath) != nil, (try? rollback.discard(for: vaultPath)) != nil {
            report.discardedRollback = true
        }
        if transaction.location.hasDirectoryAccess, let backup = try? await transaction.readBackup() {
            let holdsLegacy = !backup.envelope.secureEnclaveWraps(.legacy).isEmpty
            let opensUnderDEK = (try? backup.envelope.open(dek: dek)) != nil
            if holdsLegacy || !opensUnderDEK, (try? await transaction.deleteBackup()) != nil {
                report.deletedBackup = true
            }
        }
        report.sweptKeys = keyring.sweep(vaultPath: vaultPath,
                                         referenced: Set(opened.snapshot.envelope.secureEnclaveKeyIDs))
        return report
    }

    // MARK: - Rotation

    private func rotateToFreshKey(opened: OpenedVault, heldDEK: Data, replacingSalt: String?,
                                  newPassphrase: String?, appKey: AppKeyWrap?, localLegacy: [VaultWrap],
                                  requireBiometrics: Bool, legacyGuard: [VaultWrap],
                                  context: LAContext?) async throws -> MigrationOutcome {
        let envelope = opened.snapshot.envelope
        if let appKey {
            guard try passphraseSalt(envelope, passphrase: appKey.passphrase, heldDEK: heldDEK) == appKey.salt else {
                throw VaultWriteError.keyChanged
            }
        }
        let useEnclave = keyring.isAvailable
        let old = useEnclave ? try keyring.localKeys(in: envelope) : []

        var recreated = Set<Int>()
        for (i, wrap) in envelope.wraps.enumerated() {
            let isLocalSE = old.contains { $0.wrap.isSameWrap(as: wrap) } || localLegacy.contains { $0.isSameWrap(as: wrap) }
            let isRecreatedPassphrase = Self.isPassphrase(wrap) && wrap.salt != nil
                && (wrap.salt == replacingSalt || wrap.salt == appKey?.salt)
            if isLocalSE || isRecreatedPassphrase { recreated.insert(i) }
        }
        let passphrases = [appKey?.passphrase, newPassphrase].compactMap { $0 }
        let id: SEKeyID? = useEnclave ? .random() : nil

        let (candidate, dek) = try beforeCommit(newKey: id) {
            try saveRollback(opened.snapshot)
            let key = try id.map {
                try provision($0, requireBiometrics: requireBiometrics, isRotationPending: false, context: context)
            }
            var candidate = envelope
            let rotated = try candidate.rotate(accounts: opened.accounts, passphrases: passphrases,
                                               recreating: recreated, argon2: argon2) { dek in
                try key.map { [try keyring.makeWrap(dek: dek, key: $0)] } ?? []
            }
            try verifyRotated(candidate, dek: rotated.dek, accounts: opened.accounts, passphrases: passphrases, key: key)
            try checkpoint.reached(.wrapVerified)
            return (candidate, rotated.dek)
        }
        guard let marker = candidate.wraps.last else { throw EnvelopeError.noWraps }

        let accounts = opened.accounts
        let (committed, preCommit) = try await commit(heldDEK: heldDEK, newKey: id, marker: marker, dek: dek,
                                                      fallback: opened.snapshot.bytes) { fresh in
            let freshEnvelope = fresh.snapshot.envelope
            guard legacyGuard.allSatisfy(freshEnvelope.containsWrap) else { throw MigrationError.legacyWrapChanged }
            guard Self.hasSameWraps(freshEnvelope, envelope) else { throw MigrationError.wrapsChanged }
            let next = fresh.accounts == accounts ? candidate : try candidate.reseal(accounts: fresh.accounts, dek: dek)
            return VaultCommit(envelope: next, accounts: fresh.accounts, dek: dek, backupPolicy: .copyCurrent)
        }
        try checkpoint.reached(.committed)

        try await verifyReadBack(committed, preCommit: preCommit, newKey: id) { snapshot in
            if let id {
                try expectCurrent(snapshot, id: id, dek: dek, isPending: false, context: context)
            } else if let opener = passphrases.first {
                let (opened, salt) = try snapshot.envelope.recoverDEKAndSalt(passphrase: opener, argon2: argon2)
                guard opened == dek, salt == candidate.wraps.first?.salt else {
                    throw MigrationError.verificationFailed(.readBackVerified)
                }
            }
        }
        try await deleteBackup()
        try deleteOldKeys(old.map(\.id))
        try discardRollback()
        let removed = envelope.wraps.indices.filter { !recreated.contains($0) }.map { Self.removedWrap(envelope.wraps[$0]) }
        return MigrationOutcome(committed: committed, state: .current, removedWraps: removed,
                                appKeyWrapSalt: appKey == nil ? nil : candidate.wraps.first?.salt)
    }

    // MARK: - Pending migration

    private func migrateKeepingDEK(opened: OpenedVault, heldDEK: Data, local: [VaultWrap], requireBiometrics: Bool,
                                   context: LAContext?) async throws -> MigrationOutcome {
        let envelope = opened.snapshot.envelope
        let id = SEKeyID.random()
        let newWrap = try beforeCommit(newKey: id) {
            try saveRollback(opened.snapshot)
            let key = try provision(id, requireBiometrics: requireBiometrics, isRotationPending: true, context: context)
            let wrap = try keyring.makeWrap(dek: heldDEK, key: key)
            var candidate = envelope
            try Self.replace(local, with: wrap, in: &candidate)
            try verifyPayload(candidate, dek: heldDEK, accounts: opened.accounts)
            try checkpoint.reached(.wrapVerified)
            return wrap
        }

        let (committed, preCommit) = try await commit(heldDEK: heldDEK, newKey: id, marker: newWrap, dek: heldDEK,
                                                      fallback: opened.snapshot.bytes) { fresh in
            var next = fresh.snapshot.envelope
            guard local.allSatisfy(next.containsWrap) else { throw MigrationError.legacyWrapChanged }
            try Self.replace(local, with: newWrap, in: &next)
            return VaultCommit(envelope: next, accounts: fresh.accounts, dek: heldDEK, backupPolicy: .copyCurrent)
        }
        try checkpoint.reached(.committed)
        try await verifyReadBack(committed, preCommit: preCommit, newKey: id) { snapshot in
            try expectCurrent(snapshot, id: id, dek: heldDEK, isPending: true, context: context)
        }
        try await deleteBackup()
        try discardRollback()
        return MigrationOutcome(committed: committed, state: .rotationPending, removedWraps: [], appKeyWrapSalt: nil)
    }

    // MARK: - Steps

    private func saveRollback(_ snapshot: VaultSnapshot) throws {
        try rollback.save(snapshot.bytes, for: vaultPath)
        try checkpoint.reached(.rollbackSaved)
    }

    private func provision(_ id: SEKeyID, requireBiometrics: Bool, isRotationPending: Bool,
                           context: LAContext?) throws -> ProvisionedSEKey {
        try keyring.createKey(id: id, for: vaultPath, requireBiometrics: requireBiometrics,
                              isRotationPending: isRotationPending, context: context) { stage in
            switch stage {
            case .registered: try checkpoint.reached(.registryWritten)
            case .itemWritten: try checkpoint.reached(.keyItemWritten)
            case .readBack: try checkpoint.reached(.keyReadBack)
            }
        }
    }

    /// Runs the steps before the commit. A failure other than an interruption
    /// removes the new key; the file has not been touched.
    private func beforeCommit<T>(newKey id: SEKeyID?, _ body: () throws -> T) throws -> T {
        do {
            return try body()
        } catch {
            if !checkpoint.isInterruption(error) { removeKey(id) }
            throw error
        }
    }

    /// Commits, and returns the bytes the transform last saw. When the commit
    /// reports a failure, the file is read again: if `marker` is in it the
    /// commit landed and the flow goes on; if the file cannot be read the new
    /// key is kept, since the file may reference it.
    private func commit(heldDEK: Data, newKey id: SEKeyID?, marker: VaultWrap, dek: Data, fallback: Data,
                        _ transform: @escaping @Sendable (OpenedVault) throws -> VaultCommit)
        async throws -> (CommittedVault, Data) {
        guard isCurrent() else {
            removeKey(id)
            throw CancellationError()
        }
        let seen = SeenBytes()
        do {
            let committed = try await transaction.commit(heldDEK: heldDEK) { fresh in
                seen.set(fresh.snapshot.bytes)
                return try transform(fresh)
            }
            return (committed, seen.value ?? fallback)
        } catch {
            if checkpoint.isInterruption(error) { throw error }
            guard let snapshot = try? await transaction.read() else { throw error }
            guard snapshot.envelope.containsWrap(marker) else {
                removeKey(id)
                throw error
            }
            guard let accounts = try? snapshot.envelope.open(dek: dek) else { throw error }
            return (CommittedVault(envelope: snapshot.envelope, accounts: accounts, dek: dek, bytes: snapshot.bytes,
                                   modifiedAt: snapshot.modifiedAt), seen.value ?? fallback)
        }
    }

    /// Re-reads the committed file and opens it through the new path. On a
    /// failure the bytes from before the commit go back verbatim, only while
    /// the file still holds the committed bytes, and the new key is removed;
    /// if the restore fails, the key and the rollback copy stay. A file
    /// changed since the commit stands, with the rollback copy, and keeps the
    /// new key while it references it.
    private func verifyReadBack(_ committed: CommittedVault, preCommit: Data, newKey id: SEKeyID?,
                                opensThroughNewPath: (VaultSnapshot) throws -> Void) async throws {
        do {
            let snapshot = try await transaction.read()
            try opensThroughNewPath(snapshot)
            let accounts = try snapshot.envelope.open(dek: committed.dek)
            if snapshot.bytes == committed.bytes, accounts != committed.accounts {
                throw MigrationError.verificationFailed(.readBackVerified)
            }
        } catch {
            if checkpoint.isInterruption(error) { throw error }
            let outcome: RestoreOutcome
            do {
                outcome = try await transaction.restore(verbatim: preCommit, ifStill: committed.bytes)
            } catch {
                if checkpoint.isInterruption(error) { throw error }
                throw MigrationError.restoreFailed
            }
            switch outcome {
            case .restored:
                removeKey(id)
                throw MigrationError.verificationFailed(.readBackVerified)
            case .changedSince:
                try await removeKeyUnlessReferenced(id)
                throw MigrationError.changedByAnotherApp
            }
        }
        try checkpoint.reached(.readBackVerified)
    }

    /// Removes a vault this flow created, and its key, while the file still
    /// holds the bytes written. If the removal fails, both stay.
    private func discardCreatedVault(_ committed: CommittedVault, newKey id: SEKeyID) async throws -> Never {
        let outcome: RestoreOutcome
        do {
            outcome = try await transaction.removeCreated(ifStill: committed.bytes)
        } catch {
            if checkpoint.isInterruption(error) { throw error }
            throw MigrationError.restoreFailed
        }
        switch outcome {
        case .restored:
            removeKey(id)
            throw MigrationError.verificationFailed(.readBackVerified)
        case .changedSince:
            try await removeKeyUnlessReferenced(id)
            throw MigrationError.changedByAnotherApp
        }
    }

    /// Removes the new key when the file another app wrote does not reference
    /// it, or is gone. A file that does not read keeps the key for the sweep.
    private func removeKeyUnlessReferenced(_ id: SEKeyID?) async throws {
        guard let id else { return }
        do {
            let snapshot = try await transaction.read()
            if !snapshot.envelope.secureEnclaveKeyIDs.contains(id) { removeKey(id) }
        } catch VaultWriteError.missingFile {
            removeKey(id)
        } catch {
            if checkpoint.isInterruption(error) { throw error }
        }
    }

    private func expectCurrent(_ snapshot: VaultSnapshot, id: SEKeyID, dek: Data, isPending: Bool,
                               context: LAContext?) throws {
        guard case let .current(opened, openedID, openedPending, _, _) = try keyring.unwrap(snapshot.envelope,
                                                                                            context: context),
              openedID == id, opened == dek, openedPending == isPending else {
            throw MigrationError.verificationFailed(.readBackVerified)
        }
    }

    private func deleteBackup() async throws {
        do {
            try await transaction.deleteBackup()
        } catch where !checkpoint.isInterruption(error) {}
        try checkpoint.reached(.backupHandled)
    }

    /// Best effort: a key left behind is still registered, so the next sweep deletes it.
    private func deleteOldKeys(_ ids: [SEKeyID]) throws {
        for id in ids { try? keyring.deleteKey(id, for: vaultPath) }
        try checkpoint.reached(.oldKeyDeleted)
    }

    /// Best effort: the next open of this vault discards a copy left behind.
    private func discardRollback() throws {
        try? rollback.discard(for: vaultPath)
        try checkpoint.reached(.rollbackDiscarded)
    }

    private func removeKey(_ id: SEKeyID?) {
        guard let id else { return }
        try? keyring.deleteKey(id, for: vaultPath)
    }

    // MARK: - Checks

    private func passphraseSalt(_ envelope: Envelope, passphrase: String, heldDEK: Data) throws -> String {
        let (dek, salt) = try envelope.recoverDEKAndSalt(passphrase: passphrase, argon2: argon2)
        guard dek == heldDEK else { throw VaultWriteError.keyChanged }
        return salt
    }

    private func localLegacyWraps(_ envelope: Envelope) -> [VaultWrap] {
        envelope.secureEnclaveWraps(.legacy).filter(keyring.isLegacyWrapFromThisMac)
    }

    /// `accounts` in the order the payload stores them.
    private static func inPayloadOrder(_ accounts: [Account]) throws -> [Account] {
        var bytes = CanonicalJSON.encode(accounts)
        defer { bytes.resetBytes(in: 0..<bytes.count) }
        return try CanonicalJSON.decode(bytes)
    }

    private func verifyPayload(_ envelope: Envelope, dek: Data, accounts: [Account]) throws {
        guard try envelope.open(dek: dek) == accounts else { throw MigrationError.verificationFailed(.wrapVerified) }
    }

    private func verifyRotated(_ envelope: Envelope, dek: Data, accounts: [Account], passphrases: [String],
                               key: ProvisionedSEKey?) throws {
        for (index, passphrase) in passphrases.enumerated() {
            let (opened, salt) = try envelope.recoverDEKAndSalt(passphrase: passphrase, argon2: argon2)
            guard opened == dek, salt == envelope.wraps[index].salt else {
                throw MigrationError.verificationFailed(.wrapVerified)
            }
        }
        if let key {
            let wraps = envelope.secureEnclaveWraps(.current).filter { $0.secureEnclaveKeyID == key.id }
            guard wraps.count == 1, try SEWrapCodec.open(wrap: wraps[0], with: key.key) == dek else {
                throw MigrationError.verificationFailed(.wrapVerified)
            }
        }
        try verifyPayload(envelope, dek: dek, accounts: accounts)
    }

    private static func isPassphrase(_ wrap: VaultWrap) -> Bool { wrap.type.utf8.elementsEqual(passphraseType.utf8) }

    private static func removedWrap(_ wrap: VaultWrap) -> RemovedWrap {
        if isPassphrase(wrap) { return .passphrase }
        if wrap.type.utf8.elementsEqual(secureEnclaveType.utf8) { return .secureEnclave }
        return .unknown(wrap.type)
    }

    private static func hasSameWraps(_ a: Envelope, _ b: Envelope) -> Bool {
        a.wraps.count == b.wraps.count && zip(a.wraps, b.wraps).allSatisfy { $0.isSameWrap(as: $1) }
    }

    /// Puts `wrap` in place of the first of `old` and removes the rest.
    private static func replace(_ old: [VaultWrap], with wrap: VaultWrap, in envelope: inout Envelope) throws {
        guard let first = old.first, try envelope.replaceWrap(first, with: wrap) else {
            throw MigrationError.wrapsChanged
        }
        for other in old.dropFirst() where try !envelope.removeWrap(other) { throw MigrationError.wrapsChanged }
    }
}

private final class SeenBytes: Sendable {
    private let state = OSAllocatedUnfairLock<Data?>(initialState: nil)
    func set(_ bytes: Data) { state.withLock { $0 = bytes } }
    var value: Data? { state.withLock { $0 } }
}
