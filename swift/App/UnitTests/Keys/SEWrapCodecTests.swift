import CryptoKit
import XCTest
import TesseraCore
@testable import Tessera

final class SEWrapCodecTests: XCTestCase {
    private let factory = SoftwareEnclaveKeyFactory(deviceTag: KeyFixture.thisMac)
    private let dek = Data((0..<32).map { UInt8($0) })

    /// The wrap key derivation of 1.1.0, copied as it shipped. It must never change.
    private func frozenLegacyWrapKey(_ priv: any EnclaveKey) throws -> Data {
        let hkdfSalt = Data("tessera.se.salt.v1".utf8)
        let hkdfInfo = Data("tessera.se.dek.v1".utf8)
        let shared = try priv.sharedSecretFromKeyAgreement(with: priv.publicKey)
        let key = shared.hkdfDerivedSymmetricKey(
            using: SHA256.self, salt: hkdfSalt, sharedInfo: hkdfInfo, outputByteCount: 32
        )
        return key.withUnsafeBytes { Data($0) }
    }

    func testCurrentWrapRoundTripsWithASoftwareKey() throws {
        let key = try factory.makeKey(requireBiometrics: false, context: nil)
        let id = SEKeyID.random()
        let wrap = try SEWrapCodec.seal(dek: dek, with: key, id: id)
        XCTAssertEqual(wrap.secureEnclaveForm, .current)
        XCTAssertEqual(wrap.kdf, "se-keychain-v1")
        XCTAssertEqual(wrap.secureEnclaveKeyID, id)
        XCTAssertEqual(try SEWrapCodec.open(wrap: wrap, with: key), dek)
        XCTAssertNotEqual(try SEWrapCodec.seal(dek: dek, with: key, id: id).nonce, wrap.nonce)
    }

    func testLegacyDerivationEqualsTheFrozenCopy() throws {
        for requireBiometrics in [false, true] {
            let key = try factory.makeKey(requireBiometrics: requireBiometrics, context: nil)
            XCTAssertEqual(try SEWrapCodec.deriveWrapKey(key: key, version: .legacy), try frozenLegacyWrapKey(key))
        }
    }

    func testWrapSealedThe110WayOpens() throws {
        let key = try factory.makeKey(requireBiometrics: false, context: nil)
        let nonce = SecureRandom.bytes(XChaCha.nonceSize)
        let ct = try XChaCha.seal(dek, key: try frozenLegacyWrapKey(key), nonce: nonce)
        let wrap = VaultWrap(type: "secure-enclave", kdf: nil, params: nil, salt: nil,
                             se_key: key.dataRepresentation.base64EncodedString(),
                             nonce: nonce.base64EncodedString(), ct: ct.base64EncodedString())
        XCTAssertEqual(wrap.secureEnclaveForm, .legacy)
        XCTAssertEqual(try SEWrapCodec.legacyKeyData(wrap), key.dataRepresentation)
        XCTAssertEqual(try SEWrapCodec.open(wrap: wrap, with: key), dek)
    }

    func testVersionsDeriveDifferentKeys() throws {
        let key = try factory.makeKey(requireBiometrics: false, context: nil)
        XCTAssertNotEqual(try SEWrapCodec.deriveWrapKey(key: key, version: .legacy),
                          try SEWrapCodec.deriveWrapKey(key: key, version: .current))
    }

    func testWrongVersionFails() throws {
        let key = try factory.makeKey(requireBiometrics: false, context: nil)
        var asLegacy = try SEWrapCodec.seal(dek: dek, with: key, id: .random())
        asLegacy.kdf = nil
        XCTAssertEqual(asLegacy.secureEnclaveForm, .legacy)
        XCTAssertThrowsError(try SEWrapCodec.open(wrap: asLegacy, with: key)) {
            XCTAssertEqual($0 as? SEKeyError, .verifyFailed)
        }
    }

    func testOtherKeyFails() throws {
        let key = try factory.makeKey(requireBiometrics: false, context: nil)
        let other = try factory.makeKey(requireBiometrics: false, context: nil)
        let wrap = try SEWrapCodec.seal(dek: dek, with: key, id: .random())
        XCTAssertThrowsError(try SEWrapCodec.open(wrap: wrap, with: other)) {
            XCTAssertEqual($0 as? SEKeyError, .verifyFailed)
        }
    }

    func testMalformedWrapIsRejected() throws {
        let key = try factory.makeKey(requireBiometrics: false, context: nil)
        let passphraseLike = VaultWrap(type: "passphrase", kdf: "argon2id", params: nil, salt: nil, se_key: nil,
                                       nonce: Data(count: 24).base64EncodedString(),
                                       ct: Data(count: 48).base64EncodedString())
        XCTAssertThrowsError(try SEWrapCodec.open(wrap: passphraseLike, with: key)) {
            XCTAssertEqual($0 as? SEKeyError, .malformedWrap)
        }
        let current = try SEWrapCodec.seal(dek: dek, with: key, id: .random())
        XCTAssertThrowsError(try SEWrapCodec.legacyKeyData(current)) {
            XCTAssertEqual($0 as? SEKeyError, .malformedWrap)
        }
    }
}
