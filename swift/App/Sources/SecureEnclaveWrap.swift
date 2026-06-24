import CryptoKit
import Foundation
import LocalAuthentication
import TesseraCore

/// Manages the Touch ID-gated Secure Enclave wrap of the vault DEK.
///
/// A non-extractable SE P-256 key agreement key derives a deterministic wrap key
/// via ECDH against its own public key; the DEK is sealed under that key with
/// XChaCha20-Poly1305 and the SE key blob is stored in the envelope. The
/// plaintext DEK never rests on disk, and unwrapping requires biometric auth.
enum SecureEnclaveWrap {
    static let wrapType = "secure-enclave"
    private static let hkdfSalt = Data("tessera.se.salt.v1".utf8)
    private static let hkdfInfo = Data("tessera.se.dek.v1".utf8)

    enum SEError: Error { case notAvailable, noWrap, deriveFailed }

    static var isAvailable: Bool { SecureEnclave.isAvailable }

    /// Add (or replace) the SE wrap for the given DEK. Requires biometric consent.
    static func enable(on env: inout Envelope, dek: Data) throws {
        guard SecureEnclave.isAvailable else { throw SEError.notAvailable }
        let access = try SecAccessControlCreateWithFlags(
            nil, kSecAttrAccessibleWhenUnlockedThisDeviceOnly,
            [.privateKeyUsage, .biometryCurrentSet], nil
        ).get()
        let priv = try SecureEnclave.P256.KeyAgreement.PrivateKey(accessControl: access)
        let wrapKey = try deriveWrapKey(priv: priv)
        let nonce = randomData(XChaCha.nonceSize)
        let ct = try XChaCha.seal(dek, key: wrapKey, nonce: nonce)

        let wrap = VaultWrap(
            type: wrapType, kdf: nil, params: nil, salt: nil,
            se_key: priv.dataRepresentation.base64EncodedString(),
            nonce: nonce.base64EncodedString(), ct: ct.base64EncodedString()
        )
        env.wraps.removeAll { $0.type == wrapType }
        env.wraps.append(wrap)
    }

    static func disable(on env: inout Envelope) {
        env.wraps.removeAll { $0.type == wrapType }
    }

    static func hasWrap(_ env: Envelope) -> Bool { env.wraps.contains { $0.type == wrapType } }

    /// Unwrap the DEK via Touch ID. `reason` is shown in the biometric prompt.
    static func open(_ env: Envelope, reason: String) throws -> Data {
        guard let wrap = env.wraps.first(where: { $0.type == wrapType }),
              let keyB64 = wrap.se_key,
              let keyData = Data(base64Encoded: keyB64),
              let nonce = Data(base64Encoded: wrap.nonce),
              let ct = Data(base64Encoded: wrap.ct) else {
            throw SEError.noWrap
        }
        let context = LAContext()
        context.localizedReason = reason
        let priv = try SecureEnclave.P256.KeyAgreement.PrivateKey(
            dataRepresentation: keyData, authenticationContext: context
        )
        let wrapKey = try deriveWrapKey(priv: priv)
        return try XChaCha.open(ct, key: wrapKey, nonce: nonce)
    }

    private static func deriveWrapKey(priv: SecureEnclave.P256.KeyAgreement.PrivateKey) throws -> Data {
        let shared = try priv.sharedSecretFromKeyAgreement(with: priv.publicKey)
        let key = shared.hkdfDerivedSymmetricKey(
            using: SHA256.self, salt: hkdfSalt, sharedInfo: hkdfInfo, outputByteCount: 32
        )
        return key.withUnsafeBytes { Data($0) }
    }

    private static func randomData(_ n: Int) -> Data {
        var d = Data(count: n)
        _ = d.withUnsafeMutableBytes { SecRandomCopyBytes(kSecRandomDefault, n, $0.baseAddress!) }
        return d
    }
}

private extension Optional where Wrapped == SecAccessControl {
    func get() throws -> SecAccessControl {
        guard let v = self else { throw SecureEnclaveWrap.SEError.deriveFailed }
        return v
    }
}
