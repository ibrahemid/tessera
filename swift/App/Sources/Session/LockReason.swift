import Foundation

enum LockReason: String, Sendable {
    case manual, idle, screenLocked, sleep, sessionResigned, quit, keyChanged

    var requiresOwnerAuth: Bool { self == .manual || self == .idle }
}

extension Notification.Name {
    static let tesseraVaultDidLock = Notification.Name("com.ibrahemid.tessera.vaultDidLock")
}
