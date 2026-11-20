import CryptoKit
import Foundation
import TesseraCore

/// Seals and opens the DEK under a key derived from a Secure Enclave key's
/// ECDH with its own public key (HKDF-SHA256, 32 bytes), over XChaCha20-Poly1305.
enum SEWrapCodec {
    /// `legacy` is the derivation of wraps without `kdf`; `current` is `se-keychain-v1`.
    enum Version: Sendable, Equatable { case legacy, current }

    static let hkdfSalt = Data("tessera.se.salt.v1".utf8)
    static let legacyInfo = Data("tessera.se.dek.v1".utf8)
    static let currentInfo = Data("tessera.se.dek.v2".utf8)
    private static let wrapKeyLength = 32

    static func deriveWrapKey(key: any EnclaveKey, version: Version) throws -> Data {
        let shared = try key.sharedSecretFromKeyAgreement(with: key.publicKey)
        let derived = shared.hkdfDerivedSymmetricKey(
            using: SHA256.self, salt: hkdfSalt, sharedInfo: info(version), outputByteCount: wrapKeyLength
        )
        return derived.withUnsafeBytes { Data($0) }
    }

    /// A current-form wrap of `dek` under `key`, naming the item `id`.
    static func seal(dek: Data, with key: any EnclaveKey, id: SEKeyID) throws -> VaultWrap {
        var wrapKey = try deriveWrapKey(key: key, version: .current)
        defer { wrapKey.resetBytes(in: 0..<wrapKey.count) }
        let nonce = SecureRandom.bytes(XChaCha.nonceSize)
        let ct = try XChaCha.seal(dek, key: wrapKey, nonce: nonce)
        return try VaultWrap.secureEnclave(keyID: id.bytes, nonce: nonce, ct: ct)
    }

    /// Opens a wrap of either form; the form picks the derivation.
    static func open(wrap: VaultWrap, with key: any EnclaveKey) throws -> Data {
        guard let form = wrap.secureEnclaveForm,
              let nonce = Data(base64Encoded: wrap.nonce), let ct = Data(base64Encoded: wrap.ct) else {
            throw SEKeyError.malformedWrap
        }
        var wrapKey = try deriveWrapKey(key: key, version: form == .current ? .current : .legacy)
        defer { wrapKey.resetBytes(in: 0..<wrapKey.count) }
        do {
            return try XChaCha.open(ct, key: wrapKey, nonce: nonce)
        } catch {
            throw SEKeyError.verifyFailed
        }
    }

    /// The key blob a legacy wrap carries in `se_key`.
    static func legacyKeyData(_ wrap: VaultWrap) throws -> Data {
        guard wrap.secureEnclaveForm == .legacy, let k = wrap.se_key, let data = Data(base64Encoded: k) else {
            throw SEKeyError.malformedWrap
        }
        return data
    }

    private static func info(_ version: Version) -> Data {
        switch version {
        case .legacy: return legacyInfo
        case .current: return currentInfo
        }
    }
}
