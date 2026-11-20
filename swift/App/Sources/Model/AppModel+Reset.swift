import Foundation
import TesseraCore

extension AppModel {
    // MARK: Reset

    /// Start over. For the built-in vault this deletes the Keychain key and the
    /// vault file. For an external vault shared with the CLI, it only detaches
    /// (drops the bookmark); the app never deletes a file the CLI owns.
    func resetTessera() {
        pendingSwitchMessage = nil
        if store.isExternal {
            store.clearExternal()
        } else {
            backupLedger.forget(vaultAt: store.vaultURL.path)
            AppKey.delete()
            try? FileManager.default.removeItem(at: store.vaultURL)
        }
        envelope = nil; dek = nil; appKey = nil; accounts = []
        needsReset = false; needsPassphrase = false; needsRecovery = false
        passphraseIsRecovery = false
        vaultUnreachable = false; isLocked = true
        vaultExists = store.exists; errorMessage = nil; isOpening = true
        Task { await open(reason: "Unlock your Tessera vault") }
    }
}
