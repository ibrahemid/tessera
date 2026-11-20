import Foundation
import TesseraCore
import TesseraArgon2

/// Exercises the seal/write/read/open path under whatever sandbox the running
/// binary is signed with. `Tessera --selftest` prints PASS/FAIL and exits.
///
/// It writes to a throwaway file in the sandbox temp directory and never goes
/// through `VaultStore`, which would resolve the user's real vault (an external
/// bookmark, `TESSERA_VAULT`, or the container file) and delete it on the way
/// out. The self-test must never touch a vault that holds accounts.
@MainActor
enum SelfTest {
    static func runIfRequested() -> Bool {
        guard CommandLine.arguments.contains("--selftest") else { return false }
        let argon2 = Argon2Reference()
        let pass = "selftest-passphrase"
        let url = FileManager.default.temporaryDirectory
            .appendingPathComponent("tessera-selftest-\(UUID().uuidString).json")
        // exit() does not unwind, so every path cleans up before it.
        do {
            let acct = Account(id: "t1", type: .totp, issuer: "SelfTest", account: "x",
                               secret: Data("12345678901234567890".utf8), algorithm: "SHA1",
                               digits: 6, period: 30)
            let env = try Envelope.create(accounts: [acct], passphrase: pass, argon2: argon2)
            try env.encoded().write(to: url, options: [.atomic, .completeFileProtection])
            let reopened = try Envelope.decode(try Data(contentsOf: url))
            let got = try reopened.open(passphrase: pass, argon2: argon2)
            if got.count == 1 && got[0].issuer == "SelfTest" {
                print("SELFTEST PASS path=\(url.path)")
                try? FileManager.default.removeItem(at: url)
                exit(0)
            }
            print("SELFTEST FAIL: unexpected accounts \(got)")
            try? FileManager.default.removeItem(at: url)
            exit(1)
        } catch {
            print("SELFTEST FAIL: \(error)")
            try? FileManager.default.removeItem(at: url)
            exit(1)
        }
    }
}

/// Verifies the default daily-unlock path: a non-biometric Secure Enclave wrap
/// must open with NO Touch ID prompt. `Tessera --selftest-se` prints PASS/FAIL
/// (or SKIP on SE-less Macs). A PASS with no biometric dialog confirms silent
/// open. Kept in release builds because it validates the signed build's Secure
/// Enclave entitlement; it works entirely in memory and never reads, writes, or
/// deletes a vault file.
@MainActor
enum SelfTestSE {
    static func runIfRequested() -> Bool {
        guard CommandLine.arguments.contains("--selftest-se") else { return false }
        guard SecureEnclaveWrap.isAvailable else { print("SELFTEST-SE SKIP: no Secure Enclave"); exit(0) }
        do {
            let acct = Account(id: "t1", type: .totp, issuer: "SelfTest", account: "x",
                               secret: Data("12345678901234567890".utf8), algorithm: "SHA1",
                               digits: 6, period: 30)
            let made = try Envelope.createUnwrapped(accounts: [acct])
            var env = made.0
            try SecureEnclaveWrap.enable(on: &env, dek: made.1, requireBiometrics: false)
            let dek = try SecureEnclaveWrap.open(env, reason: "Tessera self-test")
            let got = try env.open(dek: dek)
            if got.count == 1 && got[0].issuer == "SelfTest" {
                print("SELFTEST-SE PASS: non-biometric SE wrap opened silently")
                exit(0)
            }
            print("SELFTEST-SE FAIL: unexpected accounts \(got)")
            exit(1)
        } catch {
            print("SELFTEST-SE FAIL: \(error)")
            exit(1)
        }
    }
}
