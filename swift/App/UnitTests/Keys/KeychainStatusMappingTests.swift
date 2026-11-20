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

    private func item(_ data: Data, pending: Bool = false, biometrics: Bool = false) -> KeyItem {
        KeyItem(data: data, isRotationPending: pending, requiresBiometrics: biometrics)
    }

    func testAddAttributesAreExact() throws {
        let data = Data("rep".utf8)
        let plain = keychain.addAttributes(item(data), account: account)
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

        let pending = keychain.addAttributes(item(data, pending: true), account: account)
        XCTAssertEqual(pending[kSecAttrGeneric as String] as? Data, Data("tessera.rotation-pending.v1".utf8))
        XCTAssertEqual(Set(pending.keys), Set(plain.keys).union([kSecAttrGeneric as String]))

        let biometric = keychain.addAttributes(item(data, biometrics: true), account: account)
        XCTAssertEqual(biometric[kSecAttrDescription as String] as? String, "tessera.biometry-required.v1")
        XCTAssertEqual(Set(biometric.keys), Set(plain.keys).union([kSecAttrDescription as String]))

        let both = keychain.addAttributes(item(data, pending: true, biometrics: true), account: account)
        XCTAssertEqual(both[kSecAttrGeneric as String] as? Data, Data("tessera.rotation-pending.v1".utf8))
        XCTAssertEqual(both[kSecAttrDescription as String] as? String, "tessera.biometry-required.v1")
        XCTAssertEqual(Set(both.keys), Set(plain.keys).union([kSecAttrGeneric as String, kSecAttrDescription as String]))
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
        XCTAssertEqual(DataProtectionKeychain.keyItem(from: pending as CFDictionary), item(data, pending: true))
        let other: [String: Any] = [kSecValueData as String: data, kSecAttrGeneric as String: Data("x".utf8)]
        XCTAssertEqual(DataProtectionKeychain.keyItem(from: other as CFDictionary), item(data))
        let plain: [String: Any] = [kSecValueData as String: data]
        XCTAssertEqual(DataProtectionKeychain.keyItem(from: plain as CFDictionary), item(data))
        XCTAssertNil(DataProtectionKeychain.keyItem(from: [kSecAttrAccount as String: account] as CFDictionary))
        XCTAssertNil(DataProtectionKeychain.keyItem(from: nil))
    }

    func testKeyItemParsesTheBiometricSetting() {
        let data = Data("rep".utf8)
        let biometric: [String: Any] = [kSecValueData as String: data,
                                        kSecAttrDescription as String: "tessera.biometry-required.v1"]
        XCTAssertEqual(DataProtectionKeychain.keyItem(from: biometric as CFDictionary), item(data, biometrics: true))
        let both: [String: Any] = [kSecValueData as String: data,
                                   kSecAttrGeneric as String: Data("tessera.rotation-pending.v1".utf8),
                                   kSecAttrDescription as String: "tessera.biometry-required.v1"]
        XCTAssertEqual(DataProtectionKeychain.keyItem(from: both as CFDictionary),
                       item(data, pending: true, biometrics: true))
        let other: [String: Any] = [kSecValueData as String: data, kSecAttrDescription as String: "tessera.other"]
        XCTAssertEqual(DataProtectionKeychain.keyItem(from: other as CFDictionary), item(data))
        let wrongType: [String: Any] = [kSecValueData as String: data,
                                        kSecAttrDescription as String: Data("tessera.biometry-required.v1".utf8)]
        XCTAssertEqual(DataProtectionKeychain.keyItem(from: wrongType as CFDictionary), item(data))
    }
}
