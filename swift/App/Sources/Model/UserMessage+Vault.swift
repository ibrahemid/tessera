import Foundation

extension UserMessage {
    enum RestoreSource: Equatable { case backup, rollback }

    static let notAVault = "That file isn’t a Tessera vault."
    static let couldNotOpenVault = "Couldn’t open that vault."
    static let cancelButton = "Cancel"
    static let vaultSwitchBody =
        "The app will use this file from now on. To copy accounts out of a backup instead, use Restore from backup."
    static let vaultSwitchConfirm = "Use this vault"
    static let vaultFilePanelMessage =
        "Choose the tess vault file. The CLI keeps it at ~/.local/share/tessera/vault.json."
    static let vaultFolderPanelPrompt = "Allow access"
    static let restoreDamagedConfirm = "Restore"

    static func nowUsing(_ path: String) -> String { "Now using \(path)" }

    static func vaultSwitchTitle(_ fileName: String) -> String { "Use “\(fileName)” as your vault?" }

    static func vaultFolderPanelMessage(fileName: String, folder: String) -> String {
        "Choose \(folder) to let Tessera save \(fileName) with the same lock and backup files as the tess CLI."
    }

    static func wrongVaultFolder(fileName: String, folder: String) -> String {
        "Choose the folder that holds \(fileName): \(folder)."
    }

    static func folderAccessFailed(folder: String) -> String {
        "Couldn’t keep access to \(folder). Try again."
    }

    static func restoreNeedsFolderAccess(fileName: String, folder: String) -> String {
        "To restore \(fileName), allow access to \(folder)."
    }

    static func restoreDamagedTitle(vaultName: String) -> String { "Restore \(vaultName)?" }

    static func restoreDamagedBody(source: RestoreSource, vaultName: String) -> String {
        let copy = source == .backup ? "the previous version" : "the copy from before the key change"
        return "Tessera replaces it with \(copy) and keeps the damaged file as \(vaultName).damaged."
    }

    static func text(for e: VaultWriteError) -> String {
        switch e {
        case .keyChanged:
            return "This vault’s key changed in another app. Lock Tessera and unlock it again."
        case .missingFile(let url):
            return "Couldn’t find \(url.lastPathComponent). Choose the vault again in Settings."
        case .alreadyExists(let url):
            return "\(url.lastPathComponent) already exists."
        case .unparseable(let url):
            return "\(url.lastPathComponent) is damaged or from a newer Tessera. Restore from a backup."
        case .noWraps:
            return "Couldn’t save a vault with no way to unlock it."
        case .concurrentChange(let url):
            return "\(url.lastPathComponent) changed in another app while saving. Try again."
        case .lock(let lockError):
            return text(for: lockError)
        case .io(_, let url, let code):
            return "Couldn’t access \(url.lastPathComponent): \(reason(code))."
        }
    }

    static func text(for e: VaultEditError) -> String {
        switch e {
        case .vaultClosed: return "The vault is locked, so this change wasn’t saved."
        }
    }

    static func text(for e: VaultLockError) -> String {
        switch e {
        case .timedOut:
            return "Another app is saving this vault. Try again."
        case .symlink(let url):
            return "\(url.lastPathComponent) is a link. Remove it, then try again."
        case .notRegularFile(let url):
            return "\(url.lastPathComponent) isn’t a regular file. Remove it, then try again."
        case .wrongOwner(let url):
            return "\(url.lastPathComponent) belongs to another user. Remove it, then try again."
        case .wrongMode(let url, let mode):
            return "\(url.lastPathComponent) has permissions \(String(mode, radix: 8)) instead of 600. Remove it, then try again."
        case .io(let url, let code):
            return "Couldn’t access \(url.lastPathComponent): \(reason(code))."
        }
    }

    private static func reason(_ code: Int32) -> String {
        String(cString: strerror(code)).lowercased()
    }
}
