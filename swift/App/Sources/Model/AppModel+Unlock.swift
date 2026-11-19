import Foundation
import AppKit
import LocalAuthentication
import UniformTypeIdentifiers
import TesseraCore

extension AppModel {
    // MARK: Open / unlock

    /// Called once when the window appears: create the vault on first run, or open
    /// the existing one. Silent unless "Require Touch ID" is on.
    func openOnLaunch() async {
        guard !isDemo else { return }
        await open(reason: "Unlock your Tessera vault")
    }

    /// Re-open after a manual lock.
    func unlock() {
        Task { await open(reason: "Unlock your Tessera vault") }
    }

    /// Leave a recovery route and try the Secure Enclave again. The routing is a
    /// judgement about an opaque error, so it must never be a one-way door.
    func retryUnlock() {
        needsRecovery = false
        needsPassphrase = false
        passphraseIsRecovery = false
        errorMessage = nil
        isOpening = true
        Task { await open(reason: "Unlock your Tessera vault") }
    }

    func open(reason: String) async {
        guard isLocked, !needsReset, !needsPassphrase, !needsRecovery, !vaultUnreachable else {
            isOpening = false; return
        }

        // Pre-load routing (pure, unit-tested in VaultResolution): an external vault
        // that's missing/unresolvable never silently falls back to or creates the
        // container.
        switch VaultOpen.preLoad(hasBookmark: store.hasBookmark,
                                 bookmarkResolves: store.isExternal, fileExists: store.exists) {
        case .unreachable:
            vaultUnreachable = true; isOpening = false; return
        case .createContainer:
            do { try await createFreshVault(); errorMessage = nil; isLocked = false }
            catch { errorMessage = UserMessage.textUnlessCancelled(for: error, context: .unlock) }
            lastVaultModified = store.modifiedAt
            isOpening = false; return
        case .loadExisting:
            break
        }

        // On an external shared vault an unreadable file means "relocate", never
        // "delete".
        let env: Envelope
        do { env = try store.load() }
        catch {
            if store.isExternal { vaultUnreachable = true; isOpening = false; return }
            errorMessage = UserMessage.textUnlessCancelled(for: error, context: .unlock); isOpening = false; return
        }

        // No wrap this device can satisfy silently. Route via the pure decision:
        // a passphrase wrap (e.g. a tess CLI vault) is asked for; an external vault
        // with none is relocated (never deleted); a container one offers reset.
        if !SecureEnclaveWrap.hasWrap(env) && !AppKey.exists {
            switch VaultOpen.noSilentWrap(hasPassphraseWrap: env.hasPassphraseWrap, isExternal: store.isExternal) {
            case .passphrase: needsPassphrase = true
            case .unreachable: vaultUnreachable = true
            case .reset: needsReset = true
            }
            isOpening = false; return
        }

        do {
            try await openLoaded(env, reason: reason)
            errorMessage = nil
            isLocked = false
            lastVaultModified = store.modifiedAt
            migrateHandles()
            emitSwitchStatusIfPending()
        } catch {
            routeOpenFailure(error, env: env)
        }
        isOpening = false
    }

    /// Where a failed unlock lands.
    ///
    /// A LocalAuthentication error means *this attempt* failed — a finger that
    /// didn't read, a cancel, a lockout that clears with the device password —
    /// so it stays on the lock screen with its Touch ID button. Anything else
    /// out of the Secure Enclave unwrap means the key blob itself is unusable
    /// (almost always because the enrolled fingerprints changed, which
    /// invalidates a `.biometryCurrentSet` key): route to the passphrase wrap if
    /// the vault has one, otherwise to the recovery screen, instead of dead-
    /// ending on a raw error.
    private func routeOpenFailure(_ error: Error, env: Envelope) {
        if case UnlockFailure.secureEnclave(let underlying) = error {
            if isRetryableAuth(underlying) {
                errorMessage = UserMessage.textUnlessCancelled(for: underlying, context: .unlock)
                return
            }
            errorMessage = nil
            if env.hasPassphraseWrap {
                needsPassphrase = true
                passphraseIsRecovery = true
            } else {
                needsRecovery = true
            }
            return
        }
        // The Keychain app key didn't open this vault: it belongs to another
        // passphrase (a CLI vault). Ask for it instead of dead-ending.
        if case VaultError.wrongPassphrase = error, !SecureEnclaveWrap.hasWrap(env), env.hasPassphraseWrap {
            needsPassphrase = true
            errorMessage = nil
            return
        }
        errorMessage = UserMessage.textUnlessCancelled(for: error, context: .unlock)
    }

    /// Point Tessera at an existing vault file (typically one created by the tess
    /// CLI), store a security-scoped bookmark, and open it through the normal
    /// unlock flow. Never copies the file — it opens in place so the CLI keeps
    /// working on the same path.
    /// - Parameter guardSwitch: when true (the Settings switch), a valid pick is
    ///   confirmed in a dialog before the app repoints at it — the guard against
    ///   mistaking a backup for the live vault. Onboarding and relocate pass false.
    @discardableResult
    func openExistingVault(guardSwitch: Bool = false) -> String? {
        let panel = NSOpenPanel()
        panel.allowedContentTypes = [.json]
        panel.allowsOtherFileTypes = true
        panel.canChooseDirectories = false
        panel.message = "Choose a vault created by the tess CLI (usually vault.json)."
        let hint = FileManager.default.homeDirectoryForCurrentUser.appendingPathComponent(".local/share/tessera")
        if FileManager.default.fileExists(atPath: hint.path) { panel.directoryURL = hint }
        guard panel.runModal() == .OK, let url = panel.url else { return nil }
        // Reject anything that isn't a Tessera vault before touching the bookmark,
        // so a stray file never becomes the configured vault.
        guard let data = try? Data(contentsOf: url), (try? Envelope.decode(data)) != nil else {
            return "That file isn't a Tessera vault."
        }
        if guardSwitch, !confirmVaultSwitch(to: url) { return nil }
        guard store.setExternal(url) else { return "Couldn't open that vault." }
        pendingSwitchMessage = "Now using \(homeRelative(url))"
        reopenFromStore()
        return nil
    }

    /// Modal confirmation naming the file before the app repoints at it. Returns
    /// true if the user chose to switch.
    private func confirmVaultSwitch(to url: URL) -> Bool {
        let alert = NSAlert()
        alert.messageText = "Use “\(url.lastPathComponent)” as your vault?"
        alert.informativeText = "The app will use this file from now on. To copy accounts out of a backup instead, use Restore from backup."
        alert.addButton(withTitle: "Use this vault")
        alert.addButton(withTitle: "Cancel")
        return alert.runModal() == .alertFirstButtonReturn
    }

    /// Return to the built-in vault. Leaves any external file untouched.
    func useBuiltInVault() {
        pendingSwitchMessage = nil
        store.clearExternal()
        reopenFromStore()
    }

    /// Clear in-memory state and re-run the open flow after the vault source
    /// changed (external ↔ built-in).
    private func reopenFromStore() {
        accounts = []; dek = nil; appKey = nil; envelope = nil
        isLocked = true; errorMessage = nil; notice = nil
        vaultUnreachable = false; needsReset = false; needsPassphrase = false; needsRecovery = false
        passphraseIsRecovery = false
        vaultExists = store.exists
        isOpening = true
        Task { await open(reason: "Unlock your Tessera vault") }
    }

    /// Reload accounts if an external vault (shared with the CLI) changed on disk
    /// while unlocked. The DEK is unchanged by CLI account edits, so decrypt with
    /// the held key; if that fails, keep the current list rather than dropping it.
    func reloadIfExternalChanged() {
        guard !isLocked, store.isExternal, let dek else { return }
        let modified = store.modifiedAt
        guard modified != lastVaultModified else { return }
        guard let env = try? store.load(), let accts = try? env.open(dek: dek) else { return }
        self.envelope = env
        self.accounts = accts
        self.lastVaultModified = modified
    }

    /// Open a vault this Mac has no key for (e.g. created by the tess CLI) with
    /// its passphrase. On success a Secure Enclave wrap is added alongside the
    /// passphrase wrap, so future opens are silent and the CLI keeps working.
    func unlockWithPassphrase(_ passphrase: String) async {
        guard !passphrase.isEmpty else { return }
        errorMessage = nil
        do {
            let env = try store.load()
            let a = argon2
            let req = requireBiometrics
            let (updated, seAdded, dek, accts) = try await Task.detached(priority: .userInitiated) { () -> (Envelope, Bool, Data, [Account]) in
                let dek = try env.recoverDEK(passphrase: passphrase, argon2: a)
                let accts = try env.open(dek: dek)
                var updated = env
                var seAdded = false
                if SecureEnclaveWrap.isAvailable {
                    do { try SecureEnclaveWrap.enable(on: &updated, dek: dek, requireBiometrics: req); seAdded = true }
                    catch {}   // still unlocked this session; next launch asks again
                }
                return (updated, seAdded, dek, accts)
            }.value
            if seAdded { try? store.save(updated) }
            self.envelope = seAdded ? updated : env
            self.dek = dek; self.appKey = nil; self.accounts = accts
            self.lastVaultModified = store.modifiedAt
            needsPassphrase = false
            passphraseIsRecovery = false
            isLocked = false
            migrateHandles()
            emitSwitchStatusIfPending()
        } catch {
            errorMessage = UserMessage.textUnlessCancelled(for: error, context: .unlock)
        }
    }

    /// Emit the "Now using X" status once, after a vault the user switched to has
    /// successfully unlocked.
    private func emitSwitchStatusIfPending() {
        guard let m = pendingSwitchMessage else { return }
        pendingSwitchMessage = nil
        notice = .info(m)
    }

    /// A vault URL rendered relative to the user's home directory.
    func homeRelative(_ url: URL) -> String {
        url.path.replacingOccurrences(of: NSHomeDirectory(), with: "~")
    }

    /// First run: random DEK wrapped by the Secure Enclave (instant, hardware-
    /// bound, no argon2). On the rare SE-less Mac, fall back to a Keychain key
    /// behind a passphrase wrap.
    private func createFreshVault() async throws {
        if SecureEnclaveWrap.isAvailable {
            let req = requireBiometrics
            let (env, dek) = try await Task.detached(priority: .userInitiated) { () -> (Envelope, Data) in
                let made = try Envelope.createUnwrapped(accounts: [])
                var env = made.0
                try SecureEnclaveWrap.enable(on: &env, dek: made.1, requireBiometrics: req)
                return (env, made.1)
            }.value
            try store.save(env)
            self.envelope = env; self.dek = dek; self.appKey = nil
        } else {
            let a = argon2
            let (env, dek, key) = try await Task.detached(priority: .userInitiated) { () -> (Envelope, Data, Data) in
                let key = try AppKey.create(requireBiometrics: false)
                let pass = key.base64EncodedString()
                let env = try Envelope.create(accounts: [], passphrase: pass, argon2: a)
                let dek = try env.recoverDEK(passphrase: pass, argon2: a)
                return (env, dek, key)
            }.value
            try store.save(env)
            self.envelope = env; self.dek = dek; self.appKey = key
        }
        self.accounts = []; self.vaultExists = true
    }

    /// A failed unlock, tagged with the stage it failed at so the caller can route
    /// an invalidated Secure Enclave key to recovery instead of a raw error.
    private enum UnlockFailure: Error { case secureEnclave(any Error) }

    private func openLoaded(_ env: Envelope, reason: String) async throws {
        if SecureEnclaveWrap.hasWrap(env) {
            let dek: Data
            do {
                dek = try await Task.detached(priority: .userInitiated) { () -> Data in
                    try SecureEnclaveWrap.open(env, reason: reason)  // prompts only if biometric
                }.value
            } catch {
                throw UnlockFailure.secureEnclave(error)
            }
            let accts = try await Task.detached(priority: .userInitiated) { () -> [Account] in
                try env.open(dek: dek)
            }.value
            self.envelope = env; self.dek = dek; self.appKey = nil; self.accounts = accts
        } else {
            let a = argon2
            let (key, dek, accts) = try await Task.detached(priority: .userInitiated) { () -> (Data, Data, [Account]) in
                let key = try AppKey.read(reason: reason)
                let dek = try env.recoverDEK(passphrase: key.base64EncodedString(), argon2: a)
                return (key, dek, try env.open(dek: dek))
            }.value
            self.envelope = env; self.dek = dek; self.appKey = key; self.accounts = accts
        }
    }

    func lock() {
        guard !isLocked else { return }
        accounts = []
        dek = nil
        appKey = nil
        envelope = nil
        isLocked = true
        errorMessage = nil
        notice = nil
        importReport = nil
        codeCache.removeAll()
    }

    /// Migration: accounts written before handles existed gain one deterministically
    /// at unlock and the vault re-seals once (wraps untouched). The `assign` Bool
    /// guards the write, so a second unlock with handles already present is a no-op.
    /// A read-only external vault keeps the assigned handles in memory only; the
    /// same algorithm reproduces them on the next writable unlock.
    private func migrateHandles() {
        guard !isLocked, envelope != nil, dek != nil else { return }
        var next = accounts
        guard Handles.assign(&next) else { return }
        do { try persist(next) }
        catch { accounts = next }   // couldn't write (e.g. read-only): keep in memory
    }

    /// A LocalAuthentication failure: the prompt was cancelled, the finger didn't
    /// read, or biometrics are locked out. Every one of these succeeds on a later
    /// attempt, so they must never be mistaken for a dead key.
    private func isRetryableAuth(_ error: Error) -> Bool {
        if error is LAError { return true }
        return (error as NSError).domain == LAErrorDomain
    }
}
