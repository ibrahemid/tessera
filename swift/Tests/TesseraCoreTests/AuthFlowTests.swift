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
}
