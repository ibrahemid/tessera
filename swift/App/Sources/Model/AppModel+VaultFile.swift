import Foundation
import AppKit
import UniformTypeIdentifiers
import TesseraCore

extension AppModel {
    enum FolderGrant: Equatable { case granted, wrongFolder, cancelled, failed }

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
        panel.canChooseFiles = true
        panel.allowsMultipleSelection = false
        panel.showsHiddenFiles = true
        panel.message = UserMessage.vaultFilePanelMessage
        panel.directoryURL = RealHome.nearestExistingAncestor(of: RealHome.cliVaultDirectory())
        guard panel.runModal() == .OK, let url = panel.url else { return nil }
        // Reject anything that isn't a Tessera vault before touching the bookmark,
        // so a stray file never becomes the configured vault.
        guard Self.isVault(at: url) else { return UserMessage.notAVault }
        if guardSwitch, !confirmVaultSwitch(to: url) { return nil }
        guard store.setExternal(url) else { return UserMessage.couldNotOpenVault }
        let grantMessage = folderGrantMessage(requestFolderAccess(for: url), file: url)
        pendingSwitchMessage = UserMessage.nowUsing(homeRelative(url))
        reopenFromStore()
        return grantMessage
    }

    func grantFolderAccess() -> String? {
        guard store.location.access == .fileOnly else { return nil }
        let file = DurableFile.resolvedTarget(store.vaultURL)
        let result = requestFolderAccess(for: file)
        if result == .granted { objectWillChange.send() }
        return folderGrantMessage(result, file: file)
    }

    func restoreDamagedVault(from source: URL) async -> String? {
        guard let info = damagedVault else { return nil }
        if !store.location.hasDirectoryAccess {
            let file = DurableFile.resolvedTarget(store.vaultURL)
            let result = requestFolderAccess(for: file)
            guard result == .granted else {
                return folderGrantMessage(result, file: file)
                    ?? UserMessage.restoreNeedsFolderAccess(fileName: file.lastPathComponent,
                                                            folder: homeRelative(file.deletingLastPathComponent()))
            }
            objectWillChange.send()
        }
        let vaultName = store.vaultURL.lastPathComponent
        let title = UserMessage.restoreDamagedTitle(vaultName: vaultName)
        let body = UserMessage.restoreDamagedBody(source: source == info.rollback ? .rollback : .backup,
                                                  vaultName: vaultName)
        do {
            let restored = try await DamagedVaultRestore(location: store.location).run(from: source) {
                Self.confirmRestore(title: title, body: body)
            }
            guard restored else { return nil }
        } catch {
            return UserMessage.text(for: error, context: .restore)
        }
        reopenFromStore()
        return nil
    }

    func requestFolderAccess(for file: URL) -> FolderGrant {
        let folder = file.deletingLastPathComponent()
        let panel = NSOpenPanel()
        panel.canChooseDirectories = true
        panel.canChooseFiles = false
        panel.allowsMultipleSelection = false
        panel.canCreateDirectories = false
        panel.showsHiddenFiles = true
        panel.directoryURL = folder
        panel.message = UserMessage.vaultFolderPanelMessage(fileName: file.lastPathComponent,
                                                            folder: homeRelative(folder))
        panel.prompt = UserMessage.vaultFolderPanelPrompt
        guard panel.runModal() == .OK, let chosen = panel.url else { return .cancelled }
        guard VaultStore.isParentFolder(chosen, of: file) else { return .wrongFolder }
        return store.setFolder(chosen, fileName: file.lastPathComponent) ? .granted : .failed
    }

    private func folderGrantMessage(_ result: FolderGrant, file: URL) -> String? {
        let folder = homeRelative(file.deletingLastPathComponent())
        switch result {
        case .granted, .cancelled: return nil
        case .wrongFolder: return UserMessage.wrongVaultFolder(fileName: file.lastPathComponent, folder: folder)
        case .failed: return UserMessage.folderAccessFailed(folder: folder)
        }
    }

    private static func isVault(at url: URL) -> Bool {
        guard let data = try? DurableFile.read(url) else { return false }
        return (try? Envelope.decode(data)) != nil
    }

    /// Modal confirmation naming the file before the app repoints at it. Returns
    /// true if the user chose to switch.
    private func confirmVaultSwitch(to url: URL) -> Bool {
        let alert = NSAlert()
        alert.messageText = UserMessage.vaultSwitchTitle(url.lastPathComponent)
        alert.informativeText = UserMessage.vaultSwitchBody
        alert.addButton(withTitle: UserMessage.vaultSwitchConfirm)
        alert.addButton(withTitle: UserMessage.cancelButton)
        return alert.runModal() == .alertFirstButtonReturn
    }

    private static func confirmRestore(title: String, body: String) -> Bool {
        let alert = NSAlert()
        alert.messageText = title
        alert.informativeText = body
        alert.addButton(withTitle: UserMessage.restoreDamagedConfirm)
        alert.addButton(withTitle: UserMessage.cancelButton)
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
        damagedVault = nil
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

    /// A vault URL rendered relative to the user's real home directory.
    func homeRelative(_ url: URL) -> String { RealHome.homeRelative(url) }

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

    var vaultLocation: VaultLocation { store.location }
}
