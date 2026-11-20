import Foundation

extension UserMessage {
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
