import CryptoKit
import Foundation
import LocalAuthentication
import Security

/// A P-256 key agreement key whose private half never leaves its device.
protocol EnclaveKey {
    var dataRepresentation: Data { get }
    var publicKey: P256.KeyAgreement.PublicKey { get }
    func sharedSecretFromKeyAgreement(with publicKeyShare: P256.KeyAgreement.PublicKey) throws -> SharedSecret
}

protocol EnclaveKeyFactory: Sendable {
    var isAvailable: Bool { get }
    func makeKey(requireBiometrics: Bool, context: LAContext?) throws -> any EnclaveKey
    /// Restores a key from its data representation. Restoring never prompts;
    /// using a biometric key does.
    func restoreKey(dataRepresentation: Data, context: LAContext?) throws -> any EnclaveKey
}

protocol OwnerAuthenticating: Sendable {
    /// Cancels and failures pass through as the `LAError` the system reports.
    func authenticate(reason: String) async throws
}

extension SecureEnclave.P256.KeyAgreement.PrivateKey: EnclaveKey {}

/// Keys in this Mac's Secure Enclave.
struct SecureEnclaveKeyFactory: EnclaveKeyFactory {
    var isAvailable: Bool { SecureEnclave.isAvailable }

    func makeKey(requireBiometrics: Bool, context: LAContext?) throws -> any EnclaveKey {
        guard SecureEnclave.isAvailable else { throw SEKeyError.notAvailable }
        let flags: SecAccessControlCreateFlags =
            requireBiometrics ? [.privateKeyUsage, .biometryCurrentSet] : [.privateKeyUsage]
        guard let access = SecAccessControlCreateWithFlags(nil, kSecAttrAccessibleWhenUnlockedThisDeviceOnly, flags, nil)
        else { throw SEKeyError.deriveFailed }
        return try SecureEnclave.P256.KeyAgreement.PrivateKey(accessControl: access, authenticationContext: context)
    }

    func restoreKey(dataRepresentation: Data, context: LAContext?) throws -> any EnclaveKey {
        guard SecureEnclave.isAvailable else { throw SEKeyError.notAvailable }
        return try SecureEnclave.P256.KeyAgreement.PrivateKey(dataRepresentation: dataRepresentation,
                                                              authenticationContext: context)
    }
}
