import Foundation

enum BackupStatus: Equatable {
    case off, needsPassphrase, folderUnavailable, ready(last: BackupResult?)

    static func derive(folder: BackupFolder, hasPassphraseWrap: Bool, last: BackupResult?) -> BackupStatus {
        switch folder {
        case .none: return .off
        case .unavailable: return .folderUnavailable
        case .available: return hasPassphraseWrap ? .ready(last: last) : .needsPassphrase
        }
    }

    static func isFirstFailure(_ next: BackupResult, after previous: BackupResult?) -> Bool {
        next.failure != nil && previous?.failure == nil
    }
}
