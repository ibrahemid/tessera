import XCTest
import Foundation
import TesseraArgon2
@testable import TesseraCore

/// The version 2 envelope vectors, opened with the argon2id the app ships.
final class EnvelopeVectorTests: XCTestCase {
    private let argon2 = Argon2Reference()

    func testVaultV2EnvelopeOpens() throws {
        let v = try VectorNode.root().get("vault_v2")
        var env = try Envelope.decode(Data(v.get("envelope").raw))
        let dek = try env.unwrapDEK(passphrase: v.get("passphrase").text, argon2: argon2)
        let accounts = try env.open(dek: dek)
        XCTAssertEqual(CanonicalJSON.encode(accounts), try v.get("expected_canonical_json").data)
        XCTAssertEqual(env.version, Int(try v.get("expected_version").int()))
        XCTAssertEqual(Account.formatVersion(accounts), env.version)
        XCTAssertFalse(env.needsPersist)
    }

    func testEnvelopeStrictValidCasesOpen() throws {
        let es = try VectorNode.root().get("envelope_strict")
        let passphrase = try es.get("passphrase").text
        let want = try es.get("expected_canonical_json").data
        let cases = try es.get("valid").items
        XCTAssertEqual(cases.count, 10)
        for c in cases {
            let name = try c.get("name").text
            do {
                let env = try Envelope.decode(c.get("envelope_json").data)
                XCTAssertEqual(CanonicalJSON.encode(try env.open(passphrase: passphrase, argon2: argon2)), want, name)
            } catch {
                XCTFail("\(name): \(error)")
            }
        }
    }

    func testEnvelopeStrictInvalidCasesFailWithTheirClass() throws {
        let es = try VectorNode.root().get("envelope_strict")
        let passphrase = try es.get("passphrase").text
        let cases = try es.get("invalid").items
        XCTAssertEqual(cases.count, 44)
        for c in cases {
            let name = try c.get("name").text
            var got = "opened"
            do {
                let env = try Envelope.decode(c.get("envelope_json").data)
                _ = try env.open(passphrase: c["passphrase"]?.text ?? passphrase, argon2: argon2)
            } catch {
                got = envelopeErrorClass(error)
            }
            XCTAssertEqual(got, try c.get("error").text, name)
        }
    }

    func testEnvelopePreserveKeepsUnknownKeysAndWraps() throws {
        let es = try VectorNode.root().get("envelope_strict")
        let preserve = try es.get("preserve")
        let original = try preserve.get("envelope_json").data
        var env = try Envelope.decode(original)
        let before = try JSONSerialization.jsonObject(with: original) as? NSDictionary
        let same = try JSONSerialization.jsonObject(with: env.encoded()) as? NSDictionary
        XCTAssertNotNil(before)
        XCTAssertEqual(before, same)

        let dek = try env.unwrapDEK(passphrase: es.get("passphrase").text, argon2: argon2)
        XCTAssertTrue(env.needsPersist)
        env = try env.reseal(accounts: env.open(dek: dek), dek: dek)
        let out = [UInt8](try env.encoded())
        let orig = [UInt8](original)
        for key in try preserve.get("unknown_top_level").texts {
            let want = try envelopeMember(orig, key)
            XCTAssertNotNil(want, key)
            XCTAssertEqual(try envelopeMember(out, key), want, key)
        }
        for type in try ["secure-enclave"] + preserve.get("unknown_wrap_types").texts {
            let want = try envelopeWraps(orig, ofType: type)
            XCTAssertFalse(want.isEmpty, type)
            XCTAssertEqual(try envelopeWraps(out, ofType: type), want, type)
        }
    }

    func testUnwrapDEKUpgradesBelowDefaultWrap() throws {
        let es = try VectorNode.root().get("envelope_strict")
        let passphrase = try es.get("passphrase").text
        let base = try XCTUnwrap(es.get("valid").items.first { $0["name"]?.text == "base" })
        var env = try Envelope.decode(base.get("envelope_json").data)
        let cheap = Argon2Params(v: 1, m: 8, t: 1, p: 1)

        let viaRecover = try env.recoverDEK(passphrase: passphrase, argon2: argon2)
        _ = try env.open(passphrase: passphrase, argon2: argon2)
        XCTAssertFalse(env.needsPersist)
        XCTAssertEqual(env.wraps[0].params, cheap)

        let dek = try env.unwrapDEK(passphrase: passphrase, argon2: argon2)
        XCTAssertEqual(dek, viaRecover)
        XCTAssertTrue(env.needsPersist)
        XCTAssertEqual(env.wraps[0].params, Argon2Params(v: 1, m: 131072, t: 3, p: 4))

        var reread = try Envelope.decode(env.encoded())
        XCTAssertFalse(reread.needsPersist)
        XCTAssertEqual(try reread.unwrapDEK(passphrase: passphrase, argon2: argon2), dek)
        XCTAssertFalse(reread.needsPersist)
    }

    func testRotationVector() throws {
        let r = try VectorNode.root().get("rotation")
        let passphrase = try r.get("passphrase").text
        let newPassphrase = try r.get("new_passphrase").text
        let oldDEK = try XCTUnwrap(Data(base64Encoded: r.get("old_dek_b64").text))
        let original = try r.get("envelope").raw
        let expected = try r.get("expected_canonical_json").data

        for name in ["passwd", "rekey"] {
            var env = try Envelope.decode(Data(original))
            let removed: [String]
            let opener: String
            let stale: String?
            if name == "passwd" {
                removed = try env.rotatePassphrase(old: passphrase, new: newPassphrase, argon2: argon2)
                opener = newPassphrase
                stale = passphrase
            } else {
                removed = try env.rekey(passphrase: passphrase, argon2: argon2)
                opener = passphrase
                stale = nil
            }
            XCTAssertEqual(removed, try r.get("expected_removed").texts, name)
            XCTAssertEqual(env.wrapTypes, try r.get("expected_wrap_types").texts, name)

            let out = [UInt8](try env.encoded())
            for key in try r.get("kept_top_level").texts {
                XCTAssertEqual(try envelopeMember(out, key), try envelopeMember(original, key), "\(name) \(key)")
            }
            let reopened = try Envelope.decode(Data(out))
            XCTAssertEqual(CanonicalJSON.encode(try reopened.open(passphrase: opener, argon2: argon2)), expected, name)
            XCTAssertEqual(reopened.version, Int(try r.get("expected_version").int()), name)
            XCTAssertThrowsError(try reopened.open(dek: oldDEK)) { error in
                XCTAssertEqual(envelopeErrorClass(error), "auth_failed", name)
            }
            if let stale {
                XCTAssertThrowsError(try reopened.open(passphrase: stale, argon2: argon2)) { error in
                    XCTAssertEqual(envelopeErrorClass(error), "auth_failed", name)
                }
            }
        }
    }

    /// A rotation of an envelope with a second passphrase wrap removes it with
    /// the hardware wraps.
    func testRotationWithAnExtraPassphraseVector() throws {
        let r = try VectorNode.root().get("rotation_extra_passphrase")
        let passphrase = try r.get("passphrase").text
        let newPassphrase = try r.get("new_passphrase").text
        let oldDEK = try XCTUnwrap(Data(base64Encoded: r.get("old_dek_b64").text))
        let original = try r.get("envelope").raw
        let expected = try r.get("expected_canonical_json").data
        XCTAssertEqual(try envelopeWraps(original, ofType: "passphrase").count, 2)

        for name in ["passwd", "rekey"] {
            var env = try Envelope.decode(Data(original))
            let removed: [String]
            let opener: String
            if name == "passwd" {
                removed = try env.rotatePassphrase(old: passphrase, new: newPassphrase, argon2: argon2)
                opener = newPassphrase
            } else {
                removed = try env.rekey(passphrase: passphrase, argon2: argon2)
                opener = passphrase
            }
            XCTAssertEqual(removed, try r.get("expected_removed").texts, name)
            XCTAssertEqual(env.wrapTypes, try r.get("expected_wrap_types").texts, name)

            let out = [UInt8](try env.encoded())
            for key in try r.get("kept_top_level").texts {
                XCTAssertEqual(try envelopeMember(out, key), try envelopeMember(original, key), "\(name) \(key)")
            }
            let reopened = try Envelope.decode(Data(out))
            XCTAssertEqual(CanonicalJSON.encode(try reopened.open(passphrase: opener, argon2: argon2)), expected, name)
            XCTAssertEqual(reopened.version, Int(try r.get("expected_version").int()), name)
            XCTAssertThrowsError(try reopened.open(dek: oldDEK)) { error in
                XCTAssertEqual(envelopeErrorClass(error), "auth_failed", name)
            }
        }
    }

    func testBackupCopyKeepsOnlyThePassphraseWraps() throws {
        let r = try VectorNode.root().get("rotation_extra_passphrase")
        let original = try r.get("envelope").raw
        let env = try Envelope.decode(Data(original))
        let copy = try env.passphraseWrapsOnly()
        XCTAssertEqual(copy.wrapTypes, ["passphrase", "passphrase"])

        let out = [UInt8](try copy.encoded())
        XCTAssertEqual(try envelopeWraps(out, ofType: "passphrase"), try envelopeWraps(original, ofType: "passphrase"))
        for key in ["version", "aead", "payload", "x_future"] {
            XCTAssertEqual(try envelopeMember(out, key), try envelopeMember(original, key), key)
        }
        let reopened = try Envelope.decode(Data(out))
        XCTAssertEqual(CanonicalJSON.encode(try reopened.open(passphrase: r.get("passphrase").text, argon2: argon2)),
                       try r.get("expected_canonical_json").data)
        XCTAssertEqual(env.wrapTypes, ["passphrase", "passphrase", "secure-enclave", "hardware-key"])
    }

    func testBackupCopyRefusesAnEnvelopeWithoutAPassphraseWrap() throws {
        var env = try Envelope.decode(Data(VectorNode.root().get("rotation_extra_passphrase").get("envelope").raw))
        env.wraps = env.wraps.filter { $0.type != "passphrase" }
        XCTAssertThrowsError(try env.passphraseWrapsOnly()) { error in
            XCTAssertEqual(error as? EnvelopeError, .backupNeedsPassphrase)
            XCTAssertEqual(error.localizedDescription,
                           "store: the vault has no passphrase unlock; only a vault that opens with a passphrase is backed up")
        }
    }
}
