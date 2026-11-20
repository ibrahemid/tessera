import XCTest
import Foundation
@testable import TesseraCore

/// Adding a passphrase wrap beside the others, naming the passphrase wrap
/// that opened, and replacing or removing one wrap by identity.
final class VaultWrapEditTests: XCTestCase {
    private let argon2 = XorArgon2()
    private let nonce = Data(repeating: 2, count: 24).base64EncodedString()
    private let ct = Data(repeating: 3, count: 20).base64EncodedString()

    private func account(_ id: String) -> Account {
        Account(id: id, type: .totp, issuer: "Issuer \(id)", account: "user", secret: Data("12345678901234567890".utf8))
    }

    private func currentEnclaveWrap(_ fill: UInt8) throws -> VaultWrap {
        try VaultWrap.secureEnclave(keyID: Data(repeating: fill, count: 16), nonce: Data(repeating: fill, count: 24),
                                    ct: Data(repeating: fill, count: 48))
    }

    private func legacyEnclaveWrap(_ fill: UInt8) -> VaultWrap {
        VaultWrap(type: "secure-enclave", kdf: nil, params: nil, salt: nil,
                  se_key: Data(repeating: fill, count: 65).base64EncodedString(), nonce: nonce, ct: ct)
    }

    private func unknownWrap() -> VaultWrap {
        VaultWrap(type: "hardware-key", kdf: nil, params: nil, salt: nil, se_key: nil, nonce: nonce, ct: ct)
    }

    func testAddPassphraseWrapKeepsEveryExistingWrap() throws {
        var env = try Envelope.create(accounts: [account("a")], passphrase: "first", argon2: argon2)
        let dek = try env.recoverDEK(passphrase: "first", argon2: argon2)
        try env.addSecureEnclaveWrap(try currentEnclaveWrap(9))
        let before = env.wraps

        try env.addPassphraseWrap(dek: dek, passphrase: "second", argon2: argon2)

        XCTAssertEqual(env.wrapTypes, ["passphrase", "secure-enclave", "passphrase"])
        for (old, kept) in zip(before, env.wraps) { XCTAssertTrue(old.isSameWrap(as: kept)) }
        XCTAssertEqual(try env.open(passphrase: "first", argon2: argon2), [account("a")])
        XCTAssertEqual(try env.open(passphrase: "second", argon2: argon2), [account("a")])
        let added = try XCTUnwrap(env.wraps.last)
        XCTAssertEqual(added.kdf, "argon2id")
        XCTAssertEqual(added.params, Argon2Params(v: 1, m: 131072, t: 3, p: 4))
        XCTAssertNotEqual(added.salt, env.wraps[0].salt)
    }

    func testAddPassphraseWrapRefusesASeventeenthWrap() throws {
        var env = try Envelope.create(accounts: [], passphrase: "p", argon2: argon2)
        let dek = try env.recoverDEK(passphrase: "p", argon2: argon2)
        for i in 1..<16 { try env.addSecureEnclaveWrap(try currentEnclaveWrap(UInt8(i))) }
        let before = try env.encoded()

        XCTAssertThrowsError(try env.addPassphraseWrap(dek: dek, passphrase: "q", argon2: argon2)) {
            XCTAssertEqual($0 as? EnvelopeError, .tooManyWraps)
        }
        XCTAssertEqual(try env.encoded(), before)
    }

    func testRecoverDEKAndSaltNamesTheWrapThatOpened() throws {
        var env = try Envelope.create(accounts: [account("a")], passphrase: "first", argon2: argon2)
        let dek = try env.recoverDEK(passphrase: "first", argon2: argon2)
        try env.addPassphraseWrap(dek: dek, passphrase: "second", argon2: argon2)

        let first = try env.recoverDEKAndSalt(passphrase: "first", argon2: argon2)
        let second = try env.recoverDEKAndSalt(passphrase: "second", argon2: argon2)

        XCTAssertEqual(first.dek, dek)
        XCTAssertEqual(second.dek, dek)
        XCTAssertEqual(first.salt, env.wraps[0].salt)
        XCTAssertEqual(second.salt, env.wraps[1].salt)
        XCTAssertThrowsError(try env.recoverDEKAndSalt(passphrase: "third", argon2: argon2))
    }

    func testReplaceWrapKeepsPositionAndMatchesKDF() throws {
        var env = try Envelope.create(accounts: [], passphrase: "p", argon2: argon2)
        let legacy = legacyEnclaveWrap(4)
        try env.addSecureEnclaveWrap(legacy)
        try env.addSecureEnclaveWrap(legacyEnclaveWrap(5))
        let current = try currentEnclaveWrap(9)

        var sameKeyOtherKDF = legacy
        sameKeyOtherKDF.kdf = "se-keychain-v1"
        XCTAssertFalse(try env.replaceWrap(sameKeyOtherKDF, with: current))
        XCTAssertTrue(try env.replaceWrap(legacy, with: current))

        XCTAssertEqual(env.wraps.count, 3)
        XCTAssertTrue(env.wraps[1].isSameWrap(as: current))
        XCTAssertTrue(env.wraps[2].isSameWrap(as: legacyEnclaveWrap(5)))
        XCTAssertFalse(env.containsWrap(legacy))
        XCTAssertTrue(env.containsWrap(current))
        XCTAssertFalse(try env.replaceWrap(legacy, with: current))
    }

    func testReplaceWrapRefusesAMalformedEnclaveWrap() throws {
        var env = try Envelope.create(accounts: [], passphrase: "p", argon2: argon2)
        let legacy = legacyEnclaveWrap(4)
        try env.addSecureEnclaveWrap(legacy)
        var malformed = try currentEnclaveWrap(9)
        malformed.se_key = Data(repeating: 9, count: 8).base64EncodedString()

        XCTAssertThrowsError(try env.replaceWrap(legacy, with: malformed)) {
            XCTAssertEqual($0 as? EnvelopeError, .invalidSecureEnclaveWrap)
        }
        XCTAssertTrue(env.containsWrap(legacy))
    }

    func testRemoveWrapTakesOneWrapAndKeepsTheLast() throws {
        var env = try Envelope.create(accounts: [], passphrase: "p", argon2: argon2)
        let first = try currentEnclaveWrap(1)
        try env.addSecureEnclaveWrap(first)
        try env.addSecureEnclaveWrap(first)
        let passphrase = env.wraps[0]

        XCTAssertTrue(try env.removeWrap(first))
        XCTAssertEqual(env.wrapTypes, ["passphrase", "secure-enclave"])
        XCTAssertFalse(try env.removeWrap(try currentEnclaveWrap(2)))
        XCTAssertTrue(try env.removeWrap(first))
        XCTAssertThrowsError(try env.removeWrap(passphrase)) {
            XCTAssertEqual($0 as? EnvelopeError, .noWraps)
        }
        XCTAssertEqual(env.wraps.count, 1)
    }

    func testIdentityComparesWrapsReadFromAFile() throws {
        var env = try Envelope.create(accounts: [], passphrase: "p", argon2: argon2)
        try env.addSecureEnclaveWrap(legacyEnclaveWrap(4))
        env.wraps.append(unknownWrap())
        let decoded = try Envelope.decode(try env.encoded())

        for (built, read) in zip(env.wraps, decoded.wraps) { XCTAssertTrue(built.isSameWrap(as: read)) }
        XCTAssertTrue(decoded.containsWrap(unknownWrap()))
        var otherUnknown = unknownWrap()
        otherUnknown.ct = Data(repeating: 8, count: 20).base64EncodedString()
        XCTAssertFalse(decoded.containsWrap(otherUnknown))
        var otherParams = env.wraps[0]
        otherParams.params = Argon2Params(v: 1, m: 65536, t: 3, p: 4)
        XCTAssertFalse(decoded.containsWrap(otherParams))
    }
}
