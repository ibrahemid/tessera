import Foundation
import Security
import LocalAuthentication

/// The vault's app key: 32 random bytes kept in the macOS Keychain. It is the
/// secret behind the vault's passphrase wrap, so the on-disk vault format is
/// unchanged. With "Require Touch ID" off the key reads silently while the Mac is
/// unlocked; with it on the Keychain item is gated by biometrics.
enum AppKey {
    static let service = "com.ibrahemid.tessera"
    static let account = "vault-key"
    private static let byteCount = 32

    enum AppKeyError: Error, CustomStringConvertible {
        case keychain(OSStatus)
        case missing
        var description: String {
            switch self {
            case .keychain(let s):
                let msg = SecCopyErrorMessageString(s, nil) as String? ?? "unknown"
                return "Keychain error \(s): \(msg)"
            case .missing: return "App key not found in Keychain"
            }
        }
    }

    /// True when this Mac can gate the key behind Touch ID.
    static var biometricsAvailable: Bool {
        LAContext().canEvaluatePolicy(.deviceOwnerAuthenticationWithBiometrics, error: nil)
    }

    /// Whether the Keychain item exists, without prompting for biometrics.
    static var exists: Bool {
        let context = LAContext()
        context.interactionNotAllowed = true
        var query = baseQuery
        query[kSecUseAuthenticationContext as String] = context
        query[kSecReturnData as String] = false
        let status = SecItemCopyMatching(query as CFDictionary, nil)
        return status == errSecSuccess || status == errSecInteractionNotAllowed
    }

    /// Generate and store a fresh random key.
    @discardableResult
    static func create(requireBiometrics: Bool) throws -> Data {
        let key = randomBytes(byteCount)
        try store(key, requireBiometrics: requireBiometrics)
        return key
    }

    /// Read the key. Prompts for Touch ID only when the item is biometric-gated.
    static func read(reason: String) throws -> Data {
        let context = LAContext()
        context.localizedReason = reason
        var query = baseQuery
        query[kSecReturnData as String] = true
        query[kSecUseAuthenticationContext as String] = context
        var item: CFTypeRef?
        let status = SecItemCopyMatching(query as CFDictionary, &item)
        if status == errSecItemNotFound { throw AppKeyError.missing }
        guard status == errSecSuccess, let data = item as? Data else {
            throw AppKeyError.keychain(status)
        }
        return data
    }

    /// Store (replacing any existing) the key, with or without biometric gating.
    /// Everything that can fail locally happens before the old item is deleted,
    /// so a thrown error here never leaves the Keychain without the key.
    static func store(_ key: Data, requireBiometrics: Bool) throws {
        var attrs = baseQuery
        attrs[kSecValueData as String] = key
        if requireBiometrics {
            guard let access = SecAccessControlCreateWithFlags(
                nil, kSecAttrAccessibleWhenUnlockedThisDeviceOnly, .biometryCurrentSet, nil
            ) else { throw AppKeyError.keychain(errSecParam) }
            attrs[kSecAttrAccessControl as String] = access
        } else {
            attrs[kSecAttrAccessible as String] = kSecAttrAccessibleWhenUnlockedThisDeviceOnly
        }
        delete()
        let status = SecItemAdd(attrs as CFDictionary, nil)
        guard status == errSecSuccess else { throw AppKeyError.keychain(status) }
    }

    static func delete() {
        SecItemDelete(baseQuery as CFDictionary)
    }

    private static var baseQuery: [String: Any] {
        [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: account,
            kSecUseDataProtectionKeychain as String: true,
        ]
    }

    /// The app target's one CSPRNG helper; aborts rather than returning weak bytes.
    static func randomBytes(_ n: Int) -> Data {
        var d = Data(count: n)
        let rc = d.withUnsafeMutableBytes { SecRandomCopyBytes(kSecRandomDefault, n, $0.baseAddress!) }
        precondition(rc == errSecSuccess, "SecRandomCopyBytes failed")
        return d
    }
}
