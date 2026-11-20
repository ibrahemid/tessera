import Darwin
import Foundation

extension UserMessage {
    static let backupNeedsPassphrase =
        "Automatic backups need a recovery passphrase. Set one in Settings > Security."
    static let backupFolderUnavailable = "Can’t reach the backup folder. Choose it again."
    static let backupOff = "Off"
    static let backupReady = "On. Tessera saves a backup to this folder after each change."
    static let noAutomaticBackupYet = "No backup yet"

    static func text(for e: AutoBackupError) -> String {
        switch e {
        case .folderUnavailable:
            return "Couldn’t save the automatic backup: can’t reach the backup folder. "
                + "Choose it again in Settings > Backup."
        case .needsPassphrase:
            return backupNeedsPassphrase
        case .write(let code):
            return "Couldn’t save the automatic backup: \(backupReason(code))."
        case .prune(let failed):
            switch failed {
            case 0: return "Saved the automatic backup, but couldn’t read the backup folder to delete older ones."
            case 1: return "Saved the automatic backup, but couldn’t delete an older one. Delete it from the backup folder."
            default:
                return "Saved the automatic backup, but couldn’t delete \(failed) older ones. Delete them from the backup folder."
            }
        }
    }

    static func text(for status: BackupStatus) -> String {
        switch status {
        case .off: return backupOff
        case .needsPassphrase: return backupNeedsPassphrase
        case .folderUnavailable: return backupFolderUnavailable
        case .ready: return backupReady
        }
    }

    static func lastAutomaticBackup(_ result: BackupResult?) -> String {
        guard let result else { return noAutomaticBackupYet }
        if let failure = result.failure { return failure }
        let time = result.at.formatted(date: .abbreviated, time: .shortened)
        guard let fileName = result.fileName else { return "Saved \(time)" }
        return "Saved \(fileName), \(time)"
    }

    private static func backupReason(_ code: Int32) -> String {
        String(cString: strerror(code)).lowercased()
    }
}
