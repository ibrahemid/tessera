import Foundation
import TesseraCore

extension AppModel {
    /// Touch ID can gate the vault only on a Mac with both Secure Enclave and a
    /// usable biometric sensor.
    var biometricsAvailable: Bool { SecureEnclaveWrap.isAvailable && AppKey.biometricsAvailable }

    // MARK: Recovery passphrase

    /// No passphrase wrap: only this Mac's Secure Enclave or Keychain key opens
    /// the vault, so it can't be opened anywhere else or after that key is lost.
    var opensOnThisMacOnly: Bool { !isLocked && !hasRecoveryPassphrase }

    /// Add (or replace) the vault's argon2id passphrase wrap, so the vault opens
    /// with either Touch ID or this passphrase — and so the tess CLI can open it.
    /// Requires an unlocked vault; the DEK is re-wrapped, the payload untouched.
    /// Returns nil on success, or a factual reason to show inline.
    func setRecoveryPassphrase(_ passphrase: String) async -> String? {
        guard let env = envelope, let dek = dek else { return "Unlock the vault first." }
        guard passphrase.count >= PasswordRule.minLength else {
            return PasswordRule.text(for: .tooShort, noun: "passphrase")
        }
        do {
            let a = argon2
            let updated = try await Task.detached(priority: .userInitiated) { () -> Envelope in
                var e = env
                try e.setPassphraseWrap(dek: dek, passphrase: passphrase, argon2: a)
                return e
            }.value
            try store.save(updated)
            self.envelope = updated
            self.lastVaultModified = store.modifiedAt
            return nil
        } catch {
            return UserMessage.textUnlessCancelled(for: error, context: .settings) ?? "Couldn’t save the recovery passphrase."
        }
    }

    // MARK: Touch ID setting

    /// Toggle whether opening the vault requires Touch ID. Rewraps the DEK under a
    /// Secure Enclave key with or without the biometric flag (turning it on
    /// prompts once to confirm). Falls back to the Keychain item on SE-less Macs.
    func setRequireBiometrics(_ on: Bool) async -> String? {
        guard let env = envelope, let dek = dek else {
            return UserMessage.text(for: VaultError.wrongPassphrase, context: .settings)
        }
        do {
            if SecureEnclaveWrap.isAvailable {
                // Rewrap off the main thread: turning Touch ID on prompts here.
                let newEnv = try await Task.detached(priority: .userInitiated) { () -> Envelope in
                    var e = env
                    SecureEnclaveWrap.disable(on: &e)
                    try SecureEnclaveWrap.enable(on: &e, dek: dek, requireBiometrics: on)
                    return e
                }.value
                try store.save(newEnv)
                self.envelope = newEnv
            } else if let key = appKey {
                do { try AppKey.store(key, requireBiometrics: on) }
                catch {
                    // Put the key back under the previous gating; the toggle
                    // must never cost the only unlock path.
                    try? AppKey.store(key, requireBiometrics: requireBiometrics)
                    throw error
                }
            } else {
                throw VaultError.wrongPassphrase
            }
            requireBiometrics = on
            UserDefaults.standard.set(on, forKey: AppModel.biometricsPrefKey)
            return nil
        } catch {
            return UserMessage.textUnlessCancelled(for: error, context: .settings)
        }
    }

    var vaultPathDisplay: String { homeRelative(store.vaultURL) }

    /// The built-in vault path, home-relative, for the "use built-in vault" copy.
    var builtInVaultPathDisplay: String { homeRelative(store.containerVaultURL) }

    /// One-line description of which vault is active.
    var vaultStateLine: String {
        isExternalVault
            ? "Shared vault file. The tess CLI reads and writes this same file."
            : "Built-in vault."
    }

    /// Running against a user-selected external vault (shared with the tess CLI).
    var isExternalVault: Bool { store.isExternal }
}
