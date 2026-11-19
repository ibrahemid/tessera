import Foundation

/// When an encrypted backup of each vault file was last exported from this Mac,
/// keyed by vault path. Reset reads it to say whether a backup exists.
struct BackupLedger {
    static let defaultsKey = "tessera.backupExports"
    let defaults: UserDefaults

    init(defaults: UserDefaults = .standard) {
        self.defaults = defaults
    }

    func lastExport(forVaultAt path: String) -> Date? {
        guard let seconds = entries[path] else { return nil }
        return Date(timeIntervalSince1970: seconds)
    }

    func recordExport(forVaultAt path: String, at date: Date = Date()) {
        var e = entries
        e[path] = date.timeIntervalSince1970
        defaults.set(e, forKey: Self.defaultsKey)
    }

    func forget(vaultAt path: String) {
        var e = entries
        guard e.removeValue(forKey: path) != nil else { return }
        defaults.set(e, forKey: Self.defaultsKey)
    }

    private var entries: [String: Double] {
        defaults.dictionary(forKey: Self.defaultsKey) as? [String: Double] ?? [:]
    }
}

/// Wording for the reset confirmation: what reset removes here and whether a
/// backup exists to restore from afterwards.
struct ResetConfirmation: Equatable {
    let title: String
    let action: String
    let message: String

    /// - Parameters:
    ///   - vaultPath: the vault file as the user sees it.
    ///   - accountCount: accounts in the vault, nil while it can't be opened.
    ///   - lastBackup: when a backup was last exported from this vault, if ever.
    init(isExternal: Bool, vaultPath: String, accountCount: Int?, lastBackup: Date?) {
        title = "Reset Tessera?"
        if isExternal {
            action = "Disconnect vault"
            message = "Tessera stops using \(vaultPath). The file isn’t deleted, and the tess CLI can still open it."
            return
        }
        action = "Delete vault"
        var parts: [String] = []
        if let n = accountCount {
            parts.append("Deletes \(n) account\(n == 1 ? "" : "s"), the vault file at \(vaultPath) and its key in this Mac’s Keychain.")
        } else {
            parts.append("Deletes the vault file at \(vaultPath) and its key in this Mac’s Keychain.")
        }
        if let lastBackup {
            parts.append("Last backup exported \(lastBackup.formatted(date: .abbreviated, time: .shortened)). Restore it afterwards from Settings > Backup.")
        } else if accountCount != nil {
            parts.append("No backup of this vault is on record. Export one first from Settings > Backup to keep these accounts.")
        } else {
            parts.append("No backup of this vault is on record. Without one, its accounts can’t be recovered.")
        }
        message = parts.joined(separator: " ")
    }
}

extension AppModel {
    var backupLedger: BackupLedger { BackupLedger() }

    var lastBackupExport: Date? { backupLedger.lastExport(forVaultAt: store.vaultURL.path) }

    var resetConfirmation: ResetConfirmation {
        ResetConfirmation(isExternal: isExternalVault,
                          vaultPath: vaultPathDisplay,
                          accountCount: isLocked ? nil : accounts.count,
                          lastBackup: lastBackupExport)
    }
}
