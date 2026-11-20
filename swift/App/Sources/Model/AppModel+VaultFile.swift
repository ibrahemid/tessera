import Foundation
import AppKit
import UniformTypeIdentifiers
import TesseraCore

extension AppModel {
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
            return "That file isn’t a Tessera vault."
        }
        if guardSwitch, !confirmVaultSwitch(to: url) { return nil }
        guard store.setExternal(url) else { return "Couldn’t open that vault." }
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

    /// A vault URL rendered relative to the user's home directory.
    func homeRelative(_ url: URL) -> String {
        url.path.replacingOccurrences(of: NSHomeDirectory(), with: "~")
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
