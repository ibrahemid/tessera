import Foundation

extension AppModel {
    static let lockRequiresAuthPrefKey = "tessera.lockRequiresAuth"

    var isOwnerAuthRequired: Bool {
        get { UserDefaults.standard.bool(forKey: AppModel.lockRequiresAuthPrefKey) }
        set { UserDefaults.standard.set(newValue, forKey: AppModel.lockRequiresAuthPrefKey) }
    }

    func lock(reason: LockReason = .manual) {
        lockGeneration &+= 1
        if reason.requiresOwnerAuth { isOwnerAuthRequired = true }
        guard !isLocked else { return }
        accounts = []
        dek = nil
        appKey = nil
        envelope = nil
        isLocked = true
        errorMessage = nil
        notice = nil
        importReport = nil
        codeCache.removeAll()
        NotificationCenter.default.post(name: .tesseraVaultDidLock, object: self,
                                        userInfo: ["reason": reason.rawValue])
    }
}
