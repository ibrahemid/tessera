import XCTest
import Foundation
@testable import TesseraCore

private struct DeriveFailure: Error {}

/// Succeeds `allowed` times, then throws.
private final class LimitedArgon2: Argon2idProvider, @unchecked Sendable {
    private let lock = NSLock()
    private var remaining: Int

    init(allowed: Int) { remaining = allowed }

    func deriveKey(passphrase: Data, salt: Data, memoryKiB: UInt32, iterations: UInt32,
                   parallelism: UInt8, keyLength: Int) throws -> Data {
        let ok = lock.withLock { () -> Bool in
            remaining -= 1
            return remaining >= 0
        }
        guard ok else { throw DeriveFailure() }
        return try XorArgon2().deriveKey(passphrase: passphrase, salt: salt, memoryKiB: memoryKiB,
                                         iterations: iterations, parallelism: parallelism, keyLength: keyLength)
    }
}

/// Envelope reading, writing, unlocking and rotation, with a fast stand-in
/// for argon2id. EnvelopeVectorTests covers the Go-sealed vectors.
final class VaultEnvelopeTests: XCTestCase {
    private let argon2 = XorArgon2()
    private let nonce = Data(repeating: 2, count: 24).base64EncodedString()
    private let ct = Data(repeating: 3, count: 20).base64EncodedString()

    private func account(_ id: String, note: String = "") -> Account {
        Account(id: id, type: .totp, issuer: "Issuer \(id)", account: "user", secret: Data("12345678901234567890".utf8),
                note: note)
    }

    private func passphraseWrap(_ env: Envelope, dek: Data, passphrase: String) throws -> VaultWrap {
        var copy = env
        try copy.setPassphraseWrap(dek: dek, passphrase: passphrase, argon2: argon2)
        return try XCTUnwrap(copy.wraps.last)
    }

    private func currentEnclaveWrap(_ fill: UInt8 = 9) throws -> VaultWrap {
        try VaultWrap.secureEnclave(keyID: Data(repeating: fill, count: 16), nonce: Data(repeating: fill, count: 24),
                                    ct: Data(repeating: fill, count: 48))
    }

    private func legacyEnclaveWrap() -> VaultWrap {
        VaultWrap(type: "secure-enclave", kdf: nil, params: nil, salt: nil,
                  se_key: Data(repeating: 4, count: 65).base64EncodedString(), nonce: nonce, ct: ct)
    }

    private func unknownWrap() -> VaultWrap {
        VaultWrap(type: "hardware-key", kdf: nil, params: nil, salt: nil, se_key: nil, nonce: nonce, ct: ct)
    }

    private func assertError<E: Error & Equatable>(_ expected: E, _ body: () throws -> Void,
                                                   file: StaticString = #filePath, line: UInt = #line) {
        do {
            try body()
            XCTFail("expected \(expected)", file: file, line: line)
        } catch let e as E {
            XCTAssertEqual(e, expected, file: file, line: line)
        } catch {
            XCTFail("expected \(expected), got \(error)", file: file, line: line)
        }
    }

    private func assertVaultError(_ expected: String, _ body: () throws -> Void,
                                  file: StaticString = #filePath, line: UInt = #line) {
        do {
            try body()
            XCTFail("expected \(expected)", file: file, line: line)
        } catch {
            XCTAssertEqual(envelopeErrorClass(error), expected, file: file, line: line)
        }
    }

    // MARK: - Writing

    func testEncodedWritesGoFieldOrderAndSortedUnknownKeys() throws {
        let input = """
        {"x_b":true,"payload":{"ct":"\(ct)","nonce":"\(nonce)"},"wraps":[{"ct":"\(ct)", "nonce":"\(nonce)","type":"hardware-key","extra":"a\\/b"}],"aead":"xchacha20poly1305","version":1,"x_a":[1, {"k":"v"}]}
        """
        let env = try Envelope.decode(Data(input.utf8))
        let want = """
        {
          "version": 1,
          "aead": "xchacha20poly1305",
          "wraps": [
            {
              "ct": "\(ct)",
              "nonce": "\(nonce)",
              "type": "hardware-key",
              "extra": "a\\/b"
            }
          ],
          "payload": {
            "nonce": "\(nonce)",
            "ct": "\(ct)"
          },
          "x_a": [
            1,
            {
              "k": "v"
            }
          ],
          "x_b": true
        }
        """
        XCTAssertEqual(String(decoding: try env.encoded(), as: UTF8.self), want)
    }

    func testEncodedRefusesNoWrapsAndMoreThanSixteen() throws {
        let box = VaultBox(nonce: nonce, ct: ct)
        let empty = Envelope(version: 1, aead: "xchacha20poly1305", wraps: [], payload: box)
        assertError(EnvelopeError.noWraps) { _ = try empty.encoded() }
        let full = Envelope(version: 1, aead: "xchacha20poly1305", wraps: Array(repeating: unknownWrap(), count: 16),
                            payload: box)
        XCTAssertNoThrow(try full.encoded())
        let over = Envelope(version: 1, aead: "xchacha20poly1305", wraps: Array(repeating: unknownWrap(), count: 17),
                            payload: box)
        assertError(EnvelopeError.tooManyWraps) { _ = try over.encoded() }
    }

    func testDecodedWrapIsWrittenVerbatimUntilAFieldChanges() throws {
        let salt = Data(repeating: 0xFF, count: 16).base64EncodedString()
        let escapedSalt = salt.replacingOccurrences(of: "/", with: "\\/")
        let input = """
        {"version":1,"aead":"xchacha20poly1305","wraps":[{"type":"passphrase","kdf":"argon2id","params":{"v":1,"m":8,"t":1,"p":1},"salt":"\(escapedSalt)","nonce":"\(nonce)","ct":"\(ct)","x_wrap":"kept"}],"payload":{"nonce":"\(nonce)","ct":"\(ct)"}}
        """
        var env = try Envelope.decode(Data(input.utf8))
        XCTAssertEqual(env.wraps[0].salt, salt)
        let verbatim = String(decoding: try env.encoded(), as: UTF8.self)
        XCTAssertTrue(verbatim.contains("\"salt\": \"\(escapedSalt)\""))
        XCTAssertTrue(verbatim.contains("\"x_wrap\": \"kept\""))

        env.wraps[0].ct = ct
        let rewritten = String(decoding: try env.encoded(), as: UTF8.self)
        XCTAssertTrue(rewritten.contains("\"salt\": \"\(salt)\""))
        XCTAssertFalse(rewritten.contains("x_wrap"))
    }

    func testResealSetsVersionFromAccounts() throws {
        let env = try Envelope.create(accounts: [account("1")], passphrase: "pass", argon2: argon2)
        XCTAssertEqual(env.version, 1)
        let dek = try env.recoverDEK(passphrase: "pass", argon2: argon2)
        let v2 = try env.reseal(accounts: [account("1", note: "kept")], dek: dek)
        XCTAssertEqual(v2.version, 2)
        XCTAssertEqual(try Envelope.decode(v2.encoded()).version, 2)
        XCTAssertEqual(try v2.reseal(accounts: [account("1")], dek: dek).version, 1)
        XCTAssertEqual(try Envelope.create(accounts: [account("1", note: "n")], passphrase: "p", argon2: argon2).version, 2)
    }

    // MARK: - Opening

    func testOpenWithWrongOrShortDEK() throws {
        let env = try Envelope.create(accounts: [account("1")], passphrase: "pass", argon2: argon2)
        assertVaultError("auth_failed") { _ = try env.open(dek: Data(repeating: 1, count: 32)) }
        assertVaultError("corrupt") { _ = try env.open(dek: Data(repeating: 1, count: 16)) }
    }

    func testPassphraseUnlockDerivesAtMostFourKeysAndSkipsMalformedWraps() throws {
        var env = try Envelope.create(accounts: [account("1")], passphrase: "p0", argon2: argon2)
        let dek = try env.recoverDEK(passphrase: "p0", argon2: argon2)
        var malformed = env.wraps[0]
        malformed.kdf = nil
        var wraps = [malformed]
        for i in 0..<6 { wraps.append(try passphraseWrap(env, dek: dek, passphrase: "p\(i)")) }
        env.wraps = wraps

        let counter = CountingArgon2()
        XCTAssertEqual(try env.open(passphrase: "p3", argon2: counter).count, 1)
        XCTAssertEqual(counter.count, 4)

        let limited = CountingArgon2()
        assertVaultError("auth_failed") { _ = try env.open(passphrase: "p4", argon2: limited) }
        XCTAssertEqual(limited.count, 4)

        env.wraps = [malformed]
        let none = CountingArgon2()
        assertVaultError("corrupt") { _ = try env.open(passphrase: "p0", argon2: none) }
        XCTAssertEqual(none.count, 0)
    }

    func testPassphraseUnlockWithoutPassphraseWrap() throws {
        let (base, _) = try Envelope.createUnwrapped(accounts: [])
        var enclave = base
        enclave.wraps = [unknownWrap(), legacyEnclaveWrap()]
        assertVaultError("app_only") { _ = try enclave.open(passphrase: "p", argon2: argon2) }
        var unknown = base
        unknown.wraps = [unknownWrap()]
        assertVaultError("no_passphrase_wrap") { _ = try unknown.open(passphrase: "p", argon2: argon2) }
    }

    func testUnwrapDEKUpgradesOnlyBelowDefaultWraps() throws {
        var env = try Envelope.create(accounts: [account("1")], passphrase: "pass", argon2: argon2)
        XCTAssertEqual(env.wraps[0].params, Argon2Params.defaultCost)
        let dek = try env.unwrapDEK(passphrase: "pass", argon2: argon2)
        XCTAssertFalse(env.needsPersist)

        env.wraps[0].params = Argon2Params(v: 1, m: 8, t: 1, p: 1)
        let original = env.wraps[0]
        XCTAssertEqual(try env.recoverDEK(passphrase: "pass", argon2: argon2), dek)
        _ = try env.open(passphrase: "pass", argon2: argon2)
        XCTAssertFalse(env.needsPersist)

        var failing = env
        XCTAssertEqual(try failing.unwrapDEK(passphrase: "pass", argon2: LimitedArgon2(allowed: 1)), dek)
        XCTAssertFalse(failing.needsPersist)
        XCTAssertEqual(failing.wraps[0].salt, original.salt)

        XCTAssertEqual(try env.unwrapDEK(passphrase: "pass", argon2: argon2), dek)
        XCTAssertTrue(env.needsPersist)
        XCTAssertEqual(env.wraps[0].params, Argon2Params.defaultCost)
        XCTAssertNotEqual(env.wraps[0].salt, original.salt)
        XCTAssertEqual(try Envelope.decode(env.encoded()).recoverDEK(passphrase: "pass", argon2: argon2), dek)

        try env.rotate(accounts: [account("1")], passphrases: ["pass"], argon2: argon2)
        XCTAssertFalse(env.needsPersist)
    }

    // MARK: - Rotation

    func testRotateRemovedListNamesEveryWrapNotRecreatedInOrder() throws {
        var env = try Envelope.create(accounts: [account("1")], passphrase: "first", argon2: argon2)
        let dek = try env.recoverDEK(passphrase: "first", argon2: argon2)
        env.wraps += [try currentEnclaveWrap(), try passphraseWrap(env, dek: dek, passphrase: "second"),
                      unknownWrap(), legacyEnclaveWrap()]

        var keepNone = env
        let all = try keepNone.rotate(accounts: [account("1")], passphrases: ["first"], argon2: argon2)
        XCTAssertEqual(all.removed, ["passphrase", "secure-enclave", "passphrase", "hardware-key", "secure-enclave"])

        let some = try env.rotate(accounts: [account("1")], passphrases: ["first"], recreating: [0, 1], argon2: argon2)
        XCTAssertEqual(some.removed, ["passphrase", "hardware-key", "secure-enclave"])
        XCTAssertEqual(env.wrapTypes, ["passphrase"])
        assertVaultError("auth_failed") { _ = try env.open(passphrase: "second", argon2: argon2) }
    }

    func testRotateWrapsTheNewDEKForEveryPassphraseAndEnclaveWrap() throws {
        var env = try Envelope.create(accounts: [account("1")], passphrase: "old", argon2: argon2)
        env.extras = [EnvelopeMember(key: Array("x_future".utf8), value: Array("{\"a\":1}".utf8))]
        let oldDEK = try env.recoverDEK(passphrase: "old", argon2: argon2)
        let enclaveKey = Data(repeating: 5, count: 32)
        let enclaveNonce = Data(repeating: 6, count: 24)
        var handed: Data?

        let result = try env.rotate(accounts: [account("1"), account("2")], passphrases: ["one", "two"],
                                    argon2: argon2) { dek in
            handed = dek
            return [try VaultWrap.secureEnclave(keyID: Data(repeating: 1, count: 16), nonce: enclaveNonce,
                                                ct: XChaCha.seal(dek, key: enclaveKey, nonce: enclaveNonce))]
        }
        XCTAssertEqual(handed, result.dek)
        XCTAssertNotEqual(result.dek, oldDEK)
        XCTAssertEqual(result.removed, ["passphrase"])
        XCTAssertEqual(env.wrapTypes, ["passphrase", "passphrase", "secure-enclave"])
        XCTAssertEqual(env.secureEnclaveWraps(.current).count, 1)

        let reread = try Envelope.decode(env.encoded())
        XCTAssertEqual(reread.extras.map(\.value), [Array("{\"a\":1}".utf8)])
        XCTAssertEqual(try reread.open(passphrase: "one", argon2: argon2).map(\.id), ["1", "2"])
        XCTAssertEqual(try reread.open(passphrase: "two", argon2: argon2).map(\.id), ["1", "2"])
        XCTAssertEqual(try reread.open(dek: result.dek).count, 2)
        let sealed = try XCTUnwrap(Data(base64Encoded: reread.wraps[2].ct))
        XCTAssertEqual(try XChaCha.open(sealed, key: enclaveKey, nonce: enclaveNonce), result.dek)
        assertVaultError("auth_failed") { _ = try reread.open(dek: oldDEK) }
        assertVaultError("auth_failed") { _ = try reread.open(passphrase: "old", argon2: argon2) }
    }

    func testRotateFailureLeavesEnvelopeUnchanged() throws {
        var env = try Envelope.create(accounts: [account("1")], passphrase: "pass", argon2: argon2)
        env.wraps.append(unknownWrap())
        let before = try env.encoded()
        let accounts = [account("1")]

        assertError(EnvelopeError.noWraps) {
            try env.rotate(accounts: accounts, passphrases: [], argon2: argon2)
        }
        assertError(EnvelopeError.tooManyWraps) {
            try env.rotate(accounts: accounts, passphrases: Array(repeating: "p", count: 17), argon2: argon2)
        }
        assertError(EnvelopeError.tooManyWraps) {
            try env.rotate(accounts: accounts, passphrases: Array(repeating: "p", count: 16), argon2: argon2) { _ in
                [try self.currentEnclaveWrap()]
            }
        }
        assertError(EnvelopeError.invalidSecureEnclaveWrap) {
            try env.rotate(accounts: accounts, passphrases: ["p"], argon2: argon2) { _ in [self.unknownWrap()] }
        }
        XCTAssertThrowsError(try env.rotate(accounts: accounts, passphrases: ["p"], argon2: argon2) { _ in
            throw DeriveFailure()
        }) { XCTAssertTrue($0 is DeriveFailure) }
        XCTAssertThrowsError(try env.rotate(accounts: [Account(id: "", type: .totp, issuer: "", account: "",
                                                               secret: Data("x".utf8))],
                                            passphrases: ["p"], argon2: argon2))
        XCTAssertThrowsError(try env.rotate(accounts: accounts, passphrases: ["p"],
                                            argon2: LimitedArgon2(allowed: 0)))

        XCTAssertEqual(try env.encoded(), before)
        XCTAssertEqual(try env.open(passphrase: "pass", argon2: argon2).count, 1)
    }

    func testRotatePassphraseRemovesOtherPassphraseWraps() throws {
        var env = try Envelope.create(accounts: [account("1")], passphrase: "first", argon2: argon2)
        let dek = try env.recoverDEK(passphrase: "first", argon2: argon2)
        env.wraps.append(try passphraseWrap(env, dek: dek, passphrase: "second"))

        XCTAssertEqual(try env.rotatePassphrase(old: "second", new: "third", argon2: argon2), ["passphrase"])
        XCTAssertEqual(env.wrapTypes, ["passphrase"])
        XCTAssertEqual(try env.open(passphrase: "third", argon2: argon2).map(\.id), ["1"])
        assertVaultError("auth_failed") { _ = try env.open(passphrase: "first", argon2: argon2) }
        assertVaultError("auth_failed") { _ = try env.open(passphrase: "second", argon2: argon2) }

        let unchanged = try env.encoded()
        assertVaultError("auth_failed") { _ = try env.rotatePassphrase(old: "wrong", new: "x", argon2: argon2) }
        XCTAssertEqual(try env.encoded(), unchanged)
    }

    func testRekeyChangesDEKAndKeepsPassphrase() throws {
        var env = try Envelope.create(accounts: [account("1")], passphrase: "pass", argon2: argon2)
        env.wraps.append(legacyEnclaveWrap())
        let oldDEK = try env.recoverDEK(passphrase: "pass", argon2: argon2)
        XCTAssertEqual(try env.rekey(passphrase: "pass", argon2: argon2), ["secure-enclave"])
        let newDEK = try env.recoverDEK(passphrase: "pass", argon2: argon2)
        XCTAssertNotEqual(newDEK, oldDEK)
        XCTAssertEqual(env.wrapTypes, ["passphrase"])
        XCTAssertEqual(try env.open(dek: newDEK).map(\.id), ["1"])
        assertVaultError("auth_failed") { _ = try env.open(dek: oldDEK) }
    }

    // MARK: - Secure Enclave wraps

    func testSecureEnclaveWrapFactoryValidatesSizes() throws {
        let wrap = try currentEnclaveWrap()
        XCTAssertEqual(wrap.type, "secure-enclave")
        XCTAssertEqual(wrap.kdf, "se-keychain-v1")
        XCTAssertEqual(wrap.secureEnclaveForm, .current)

        let key = Data(repeating: 1, count: 16), n = Data(repeating: 1, count: 24), c = Data(repeating: 1, count: 48)
        assertError(EnvelopeError.invalidSecureEnclaveWrap) {
            _ = try VaultWrap.secureEnclave(keyID: Data(repeating: 1, count: 15), nonce: n, ct: c)
        }
        assertError(EnvelopeError.invalidSecureEnclaveWrap) {
            _ = try VaultWrap.secureEnclave(keyID: key, nonce: Data(repeating: 1, count: 12), ct: c)
        }
        assertError(EnvelopeError.invalidSecureEnclaveWrap) {
            _ = try VaultWrap.secureEnclave(keyID: key, nonce: n, ct: Data())
        }
    }

    func testSecureEnclaveWrapForms() throws {
        XCTAssertEqual(legacyEnclaveWrap().secureEnclaveForm, .legacy)
        var emptyKDF = legacyEnclaveWrap()
        emptyKDF.kdf = ""
        XCTAssertEqual(emptyKDF.secureEnclaveForm, .legacy)

        var longID = try currentEnclaveWrap()
        longID.se_key = Data(repeating: 1, count: 32).base64EncodedString()
        XCTAssertNil(longID.secureEnclaveForm)
        var otherKDF = try currentEnclaveWrap()
        otherKDF.kdf = "other"
        XCTAssertNil(otherKDF.secureEnclaveForm)
        var shortNonce = legacyEnclaveWrap()
        shortNonce.nonce = Data(repeating: 1, count: 4).base64EncodedString()
        XCTAssertNil(shortNonce.secureEnclaveForm)
        var urlKey = legacyEnclaveWrap()
        urlKey.se_key = "-_-_"
        XCTAssertNil(urlKey.secureEnclaveForm)
        var noKey = legacyEnclaveWrap()
        noKey.se_key = nil
        XCTAssertNil(noKey.secureEnclaveForm)
        var passphrase = legacyEnclaveWrap()
        passphrase.type = "passphrase"
        XCTAssertNil(passphrase.secureEnclaveForm)
    }

    func testSecureEnclaveWrapEdits() throws {
        var env = try Envelope.create(accounts: [account("1")], passphrase: "pass", argon2: argon2)
        XCTAssertFalse(env.hasSecureEnclaveWrap)
        try env.addSecureEnclaveWrap(legacyEnclaveWrap())
        try env.addSecureEnclaveWrap(try currentEnclaveWrap(1))
        XCTAssertTrue(env.hasSecureEnclaveWrap)
        XCTAssertEqual(env.secureEnclaveWraps().count, 2)
        XCTAssertEqual(env.secureEnclaveWraps(.legacy).count, 1)
        XCTAssertEqual(env.secureEnclaveWraps(.current).count, 1)
        assertError(EnvelopeError.invalidSecureEnclaveWrap) { try env.addSecureEnclaveWrap(self.unknownWrap()) }

        try env.replaceSecureEnclaveWraps(.legacy, with: try currentEnclaveWrap(2))
        XCTAssertEqual(env.wrapTypes, ["passphrase", "secure-enclave", "secure-enclave"])
        XCTAssertEqual(env.secureEnclaveWraps(.current).map(\.se_key),
                       [Data(repeating: 1, count: 16).base64EncodedString(),
                        Data(repeating: 2, count: 16).base64EncodedString()])
        try env.replaceSecureEnclaveWraps(nil, with: try currentEnclaveWrap(3))
        XCTAssertEqual(env.secureEnclaveWraps().map(\.se_key), [Data(repeating: 3, count: 16).base64EncodedString()])
        assertError(EnvelopeError.invalidSecureEnclaveWrap) {
            try env.replaceSecureEnclaveWraps(nil, with: self.legacyEnclaveWrap().withKDF("other"))
        }

        XCTAssertEqual(try env.removeSecureEnclaveWraps(.legacy), 0)
        XCTAssertEqual(try env.removeSecureEnclaveWraps(), 1)
        XCTAssertEqual(env.wrapTypes, ["passphrase"])
        XCTAssertEqual(try env.open(passphrase: "pass", argon2: argon2).count, 1)
    }

    func testSecureEnclaveRemovalKeepsTheLastWrap() throws {
        var (env, dek) = try Envelope.createUnwrapped(accounts: [account("1")])
        try env.addSecureEnclaveWrap(try currentEnclaveWrap())
        assertError(EnvelopeError.noWraps) { _ = try env.removeSecureEnclaveWraps() }
        XCTAssertEqual(env.wrapTypes, ["secure-enclave"])
        XCTAssertEqual(try env.open(dek: dek).count, 1)
        dek.resetBytes(in: 0..<dek.count)
    }

    func testWrapEditsRefuseMoreThanSixteenWraps() throws {
        var env = try Envelope.create(accounts: [account("1")], passphrase: "pass", argon2: argon2)
        let dek = try env.recoverDEK(passphrase: "pass", argon2: argon2)
        for i in 0..<15 { try env.addSecureEnclaveWrap(try currentEnclaveWrap(UInt8(i))) }
        XCTAssertEqual(env.wraps.count, 16)
        assertError(EnvelopeError.tooManyWraps) { try env.addSecureEnclaveWrap(try self.currentEnclaveWrap(99)) }
        try env.replaceSecureEnclaveWraps(.current, with: try currentEnclaveWrap(99))
        XCTAssertEqual(env.wraps.count, 2)

        env.wraps = (0..<16).map { _ in legacyEnclaveWrap() }
        assertError(EnvelopeError.tooManyWraps) {
            try env.setPassphraseWrap(dek: dek, passphrase: "pass", argon2: self.argon2)
        }
        XCTAssertFalse(env.hasPassphraseWrap)
    }
}

private extension VaultWrap {
    func withKDF(_ kdf: String) -> VaultWrap {
        var copy = self
        copy.kdf = kdf
        return copy
    }
}
