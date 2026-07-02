import XCTest
import Foundation
import TesseraArgon2
@testable import TesseraCore

/// Verifies the DEK-mutation path the app relies on: after any unlock the app
/// holds the DEK and edits the vault WITHOUT the passphrase (the biometrics-first
/// model). Catches the class of bug where editing required the password.
final class AuthFlowTests: XCTestCase {
    private let argon2 = Argon2Reference()

    func testEditWithDEKThenReopenBothWays() throws {
        let key = "RECOVERYKEYABC234"
        let env = try Envelope.create(accounts: [], passphrase: key, argon2: argon2)
        let dek = try env.recoverDEK(passphrase: key, argon2: argon2)

        let acct = Account(id: "1", type: .totp, issuer: "GitHub", account: "me",
                           secret: Data("12345678901234567890".utf8), algorithm: "SHA1",
                           digits: 6, period: 30)
        // Edit using only the DEK (no passphrase) — the daily/Touch ID path.
        let updated = try env.reseal(accounts: [acct], dek: dek)

        // Reopen with the DEK directly.
        XCTAssertEqual(try updated.open(dek: dek).first?.issuer, "GitHub")
        // And still recoverable with the recovery key.
        let viaKey = try updated.open(passphrase: key, argon2: argon2)
        XCTAssertEqual(viaKey.count, 1)
        XCTAssertEqual(viaKey.first?.account, "me")
    }

    func testWrongRecoveryKeyRejected() throws {
        let env = try Envelope.create(accounts: [], passphrase: "RIGHTKEY", argon2: argon2)
        XCTAssertThrowsError(try env.open(passphrase: "WRONGKEY", argon2: argon2))
    }

    /// The app-key model: a random 32-byte key (kept in the Keychain) is the
    /// passphrase-wrap secret, base64-encoded. Create, edit via DEK, reopen.
    func testAppKeyAsPassphraseWrap() throws {
        var keyBytes = Data(count: 32)
        for i in 0..<keyBytes.count { keyBytes[i] = UInt8.random(in: 0...255) }
        let pass = keyBytes.base64EncodedString()

        let env = try Envelope.create(accounts: [], passphrase: pass, argon2: argon2)
        let dek = try env.recoverDEK(passphrase: pass, argon2: argon2)

        let acct = Account(id: "1", type: .totp, issuer: "GitHub", account: "me",
                           secret: Data("12345678901234567890".utf8), algorithm: "SHA1",
                           digits: 6, period: 30)
        let updated = try env.reseal(accounts: [acct], dek: dek)

        XCTAssertEqual(try updated.open(dek: dek).first?.issuer, "GitHub")
        XCTAssertEqual(try updated.open(passphrase: pass, argon2: argon2).count, 1)
    }

    /// The CLI-vault adoption path: the app opens a passphrase-only vault with
    /// its passphrase, then attaches a second wrap for silent opens. The
    /// passphrase wrap must survive so the CLI keeps working on the same file.
    func testForeignVaultGainsSecondWrapKeepsPassphrase() throws {
        let pass = "cli-vault-passphrase"
        let acct = Account(id: "1", type: .totp, issuer: "GitHub", account: "me",
                           secret: Data("12345678901234567890".utf8), algorithm: "SHA1",
                           digits: 6, period: 30)
        var env = try Envelope.create(accounts: [acct], passphrase: pass, argon2: argon2)
        let dek = try env.recoverDEK(passphrase: pass, argon2: argon2)

        // Simulate SecureEnclaveWrap.enable (hardware-free): any second wrap type.
        env.wraps.append(VaultWrap(type: "secure-enclave", kdf: nil, params: nil, salt: nil,
                                   se_key: "c3R1Yg==", nonce: "c3R1Yg==", ct: "c3R1Yg=="))
        let decoded = try Envelope.decode(env.encoded())

        XCTAssertEqual(decoded.wraps.map(\.type).sorted(), ["passphrase", "secure-enclave"])
        XCTAssertEqual(try decoded.open(passphrase: pass, argon2: argon2).first?.issuer, "GitHub")
        XCTAssertEqual(try decoded.open(dek: dek).count, 1)
    }

    /// The Secure-Enclave path builds the envelope with no wraps and attaches the
    /// SE wrap on-device; verify the wrapless DEK round-trip the app relies on.
    func testCreateUnwrappedRoundTrip() throws {
        let acct = Account(id: "1", type: .totp, issuer: "GitHub", account: "me",
                           secret: Data("12345678901234567890".utf8), algorithm: "SHA1",
                           digits: 6, period: 30)
        let (env, dek) = try Envelope.createUnwrapped(accounts: [acct])
        XCTAssertTrue(env.wraps.isEmpty)
        XCTAssertEqual(try env.open(dek: dek).first?.issuer, "GitHub")
        let updated = try env.reseal(accounts: [], dek: dek)
        XCTAssertEqual(try updated.open(dek: dek).count, 0)
    }
}
