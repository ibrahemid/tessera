#if DEBUG
import CryptoKit
import Foundation
import LocalAuthentication
import os

/// Key items held in memory, for `-uitest` runs and unit tests.
final class InMemoryKeyItemStore: KeyItemStore, Sendable {
    private let items = OSAllocatedUnfairLock(initialState: [String: KeyItem]())

    func add(_ item: KeyItem, account: String) throws(KeyItemError) {
        let added = items.withLock { state -> Bool in
            guard state[account] == nil else { return false }
            state[account] = item
            return true
        }
        if !added { throw .duplicate }
    }

    func read(account: String) throws(KeyItemError) -> KeyItem {
        guard let item = items.withLock({ $0[account] }) else { throw .notFound }
        return item
    }

    func delete(account: String) throws(KeyItemError) {
        items.withLock { _ = $0.removeValue(forKey: account) }
    }

    var accounts: [String] { items.withLock { $0.keys.sorted() } }
}

/// A software P-256 key standing in for a Secure Enclave key. Its data
/// representation carries the tag of the factory that made it, so only that
/// factory restores it, as only the Mac that made a Secure Enclave key can.
/// The biometric setting is not part of it: the key item records that.
struct SoftwareEnclaveKey: EnclaveKey {
    let privateKey: P256.KeyAgreement.PrivateKey
    let deviceTag: Data

    var dataRepresentation: Data { deviceTag + privateKey.rawRepresentation }

    var publicKey: P256.KeyAgreement.PublicKey { privateKey.publicKey }

    func sharedSecretFromKeyAgreement(with publicKeyShare: P256.KeyAgreement.PublicKey) throws -> SharedSecret {
        try privateKey.sharedSecretFromKeyAgreement(with: publicKeyShare)
    }
}

enum SoftwareEnclaveError: Error, Equatable {
    case otherDevice
}

struct SoftwareEnclaveKeyFactory: EnclaveKeyFactory {
    static let uiTestDeviceTag = Data(repeating: 0x54, count: 16)
    private static let rawKeyLength = 32

    let deviceTag: Data
    let isAvailable: Bool

    init(deviceTag: Data = Self.uiTestDeviceTag, isAvailable: Bool = true) {
        self.deviceTag = deviceTag
        self.isAvailable = isAvailable
    }

    func makeKey(requireBiometrics: Bool, context: LAContext?) throws -> any EnclaveKey {
        guard isAvailable else { throw SEKeyError.notAvailable }
        return SoftwareEnclaveKey(privateKey: P256.KeyAgreement.PrivateKey(), deviceTag: deviceTag)
    }

    func restoreKey(dataRepresentation: Data, context: LAContext?) throws -> any EnclaveKey {
        guard isAvailable else { throw SEKeyError.notAvailable }
        guard dataRepresentation.count == deviceTag.count + Self.rawKeyLength,
              Data(dataRepresentation.prefix(deviceTag.count)) == deviceTag else {
            throw SoftwareEnclaveError.otherDevice
        }
        let raw = dataRepresentation.suffix(Self.rawKeyLength)
        return SoftwareEnclaveKey(privateKey: try P256.KeyAgreement.PrivateKey(rawRepresentation: raw),
                                  deviceTag: deviceTag)
    }
}

struct UITestOwnerAuthenticator: OwnerAuthenticating {
    func authenticate(reason: String) async throws {}
}
#endif
