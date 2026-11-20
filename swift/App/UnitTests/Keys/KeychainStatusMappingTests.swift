import Security
import XCTest
@testable import Tessera

final class KeychainStatusMappingTests: XCTestCase {
    private let keychain = DataProtectionKeychain(service: "tessera.tests.mapping")
    private let account = "se-key.00112233445566778899aabbccddeeff"

    func testStatusMapsToItsError() {
        XCTAssertNil(KeyItemError(status: errSecSuccess, operation: "add"))
        XCTAssertEqual(KeyItemError(status: -34018, operation: "add"), .missingEntitlement)
        XCTAssertEqual(KeyItemError(status: errSecItemNotFound, operation: "read"), .notFound)
        XCTAssertEqual(KeyItemError(status: errSecDuplicateItem, operation: "add"), .duplicate)
        XCTAssertEqual(KeyItemError(status: errSecInteractionNotAllowed, operation: "read"), .interactionNotAllowed)
        XCTAssertEqual(KeyItemError(status: errSecAuthFailed, operation: "delete"),
                       .unexpected(errSecAuthFailed, operation: "delete"))
    }

    func testCheckThrowsOnlyOnFailure() throws {
        try KeyItemError.check(errSecSuccess, operation: "add")
        XCTAssertThrowsError(try KeyItemError.check(errSecItemNotFound, operation: "read")) {
            XCTAssertEqual($0 as? KeyItemError, .notFound)
        }
    }

    func testAddAttributesAreExact() throws {
        let data = Data("rep".utf8)
        let plain = keychain.addAttributes(data, account: account, isRotationPending: false)
        XCTAssertEqual(Set(plain.keys), [
            kSecClass as String, kSecAttrService as String, kSecAttrAccount as String, kSecValueData as String,
            kSecAttrAccessible as String, kSecAttrSynchronizable as String, kSecUseDataProtectionKeychain as String,
        ])
        XCTAssertEqual(plain[kSecClass as String] as? String, kSecClassGenericPassword as String)
        XCTAssertEqual(plain[kSecAttrService as String] as? String, "tessera.tests.mapping")
        XCTAssertEqual(plain[kSecAttrAccount as String] as? String, account)
        XCTAssertEqual(plain[kSecValueData as String] as? Data, data)
        XCTAssertEqual(plain[kSecAttrAccessible as String] as? String, kSecAttrAccessibleWhenUnlockedThisDeviceOnly as String)
        XCTAssertEqual(plain[kSecAttrSynchronizable as String] as? Bool, false)
        XCTAssertEqual(plain[kSecUseDataProtectionKeychain as String] as? Bool, true)
        XCTAssertNil(plain[kSecAttrAccessGroup as String])

        let pending = keychain.addAttributes(data, account: account, isRotationPending: true)
        XCTAssertEqual(pending[kSecAttrGeneric as String] as? Data, Data("tessera.rotation-pending.v1".utf8))
        XCTAssertEqual(Set(pending.keys), Set(plain.keys).union([kSecAttrGeneric as String]))
    }

    func testReadAndDeleteQueriesAreExact() {
        let read = keychain.readQuery(account: account)
        XCTAssertEqual(Set(read.keys), [
            kSecClass as String, kSecAttrService as String, kSecAttrAccount as String,
            kSecUseDataProtectionKeychain as String, kSecMatchLimit as String, kSecReturnData as String,
            kSecReturnAttributes as String,
        ])
        XCTAssertEqual(read[kSecMatchLimit as String] as? String, kSecMatchLimitOne as String)
        XCTAssertEqual(read[kSecReturnData as String] as? Bool, true)
        XCTAssertEqual(read[kSecReturnAttributes as String] as? Bool, true)

        let delete = keychain.deleteQuery(account: account)
        XCTAssertEqual(Set(delete.keys), [
            kSecClass as String, kSecAttrService as String, kSecAttrAccount as String,
            kSecUseDataProtectionKeychain as String,
        ])
        XCTAssertEqual(delete[kSecAttrAccount as String] as? String, account)
    }

    func testDefaultServiceIsTheAppService() {
        XCTAssertEqual(DataProtectionKeychain().service, "com.ibrahemid.tessera")
    }

    func testKeyItemParsesTheMarker() {
        let data = Data("rep".utf8)
        let pending: [String: Any] = [kSecValueData as String: data,
                                      kSecAttrGeneric as String: Data("tessera.rotation-pending.v1".utf8)]
        XCTAssertEqual(DataProtectionKeychain.keyItem(from: pending as CFDictionary),
                       KeyItem(data: data, isRotationPending: true))
        let other: [String: Any] = [kSecValueData as String: data, kSecAttrGeneric as String: Data("x".utf8)]
        XCTAssertEqual(DataProtectionKeychain.keyItem(from: other as CFDictionary),
                       KeyItem(data: data, isRotationPending: false))
        let plain: [String: Any] = [kSecValueData as String: data]
        XCTAssertEqual(DataProtectionKeychain.keyItem(from: plain as CFDictionary),
                       KeyItem(data: data, isRotationPending: false))
        XCTAssertNil(DataProtectionKeychain.keyItem(from: [kSecAttrAccount as String: account] as CFDictionary))
        XCTAssertNil(DataProtectionKeychain.keyItem(from: nil))
    }
}
