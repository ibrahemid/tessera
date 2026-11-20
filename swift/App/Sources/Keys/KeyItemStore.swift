import Foundation
import Security

/// One keychain item holding a Secure Enclave key's data representation.
struct KeyItem: Sendable, Equatable {
    let data: Data
    let isRotationPending: Bool
}

enum KeyItemError: Error, Equatable, Sendable {
    case missingEntitlement, notFound, duplicate, interactionNotAllowed
    case unexpected(OSStatus, operation: String)

    static let missingEntitlementStatus: OSStatus = -34018

    /// The error a keychain status stands for; nil for `errSecSuccess`.
    init?(status: OSStatus, operation: String) {
        switch status {
        case errSecSuccess: return nil
        case Self.missingEntitlementStatus: self = .missingEntitlement
        case errSecItemNotFound: self = .notFound
        case errSecDuplicateItem: self = .duplicate
        case errSecInteractionNotAllowed: self = .interactionNotAllowed
        default: self = .unexpected(status, operation: operation)
        }
    }

    static func check(_ status: OSStatus, operation: String) throws(KeyItemError) {
        if let error = KeyItemError(status: status, operation: operation) { throw error }
    }
}

/// Where Secure Enclave key items live. Items are added and deleted, never updated.
protocol KeyItemStore: Sendable {
    func add(_ data: Data, account: String, isRotationPending: Bool) throws(KeyItemError)
    /// A fresh query on every call.
    func read(account: String) throws(KeyItemError) -> KeyItem
    /// Deleting an item that does not exist succeeds.
    func delete(account: String) throws(KeyItemError)
}

/// Key items in the data-protection keychain, in the app's default access group.
struct DataProtectionKeychain: KeyItemStore {
    static let defaultService = "com.ibrahemid.tessera"
    static let rotationPendingMarker = Data("tessera.rotation-pending.v1".utf8)

    let service: String

    init(service: String = Self.defaultService) { self.service = service }

    func add(_ data: Data, account: String, isRotationPending: Bool) throws(KeyItemError) {
        let attributes = addAttributes(data, account: account, isRotationPending: isRotationPending)
        try KeyItemError.check(SecItemAdd(attributes as CFDictionary, nil), operation: "add")
    }

    func read(account: String) throws(KeyItemError) -> KeyItem {
        var result: CFTypeRef?
        try KeyItemError.check(SecItemCopyMatching(readQuery(account: account) as CFDictionary, &result),
                               operation: "read")
        guard let item = Self.keyItem(from: result) else { throw .unexpected(errSecDecode, operation: "read") }
        return item
    }

    func delete(account: String) throws(KeyItemError) {
        let status = SecItemDelete(deleteQuery(account: account) as CFDictionary)
        if status == errSecItemNotFound { return }
        try KeyItemError.check(status, operation: "delete")
    }

    func addAttributes(_ data: Data, account: String, isRotationPending: Bool) -> [String: Any] {
        var attributes = baseQuery(account: account)
        attributes[kSecValueData as String] = data
        attributes[kSecAttrAccessible as String] = kSecAttrAccessibleWhenUnlockedThisDeviceOnly
        attributes[kSecAttrSynchronizable as String] = false
        if isRotationPending { attributes[kSecAttrGeneric as String] = Self.rotationPendingMarker }
        return attributes
    }

    func readQuery(account: String) -> [String: Any] {
        var query = baseQuery(account: account)
        query[kSecMatchLimit as String] = kSecMatchLimitOne
        query[kSecReturnData as String] = true
        query[kSecReturnAttributes as String] = true
        return query
    }

    func deleteQuery(account: String) -> [String: Any] { baseQuery(account: account) }

    static func keyItem(from result: CFTypeRef?) -> KeyItem? {
        guard let attributes = result as? [String: Any], let data = attributes[kSecValueData as String] as? Data else {
            return nil
        }
        let generic = attributes[kSecAttrGeneric as String] as? Data
        return KeyItem(data: data, isRotationPending: generic == rotationPendingMarker)
    }

    private func baseQuery(account: String) -> [String: Any] {
        [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: account,
            kSecUseDataProtectionKeychain as String: true,
        ]
    }
}
