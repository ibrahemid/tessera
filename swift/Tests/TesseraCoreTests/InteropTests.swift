import XCTest
import Foundation
import TesseraArgon2
@testable import TesseraCore

/// Locates the repo's /spec directory by walking up from this source file.
private func specDir() -> URL {
    var dir = URL(fileURLWithPath: #filePath)
    for _ in 0..<8 {
        dir.deleteLastPathComponent()
        let candidate = dir.appendingPathComponent("spec/testvectors.json")
        if FileManager.default.fileExists(atPath: candidate.path) {
            return dir.appendingPathComponent("spec")
        }
    }
    fatalError("spec/testvectors.json not found")
}

private func vectors() throws -> [String: Any] {
    let data = try Data(contentsOf: specDir().appendingPathComponent("testvectors.json"))
    return try JSONSerialization.jsonObject(with: data) as! [String: Any]
}

private let argon2: Argon2idProvider = Argon2Reference()

final class InteropTests: XCTestCase {

    func testHOTP() throws {
        let h = try vectors()["hotp_rfc4226"] as! [String: Any]
        let secret = Data((h["_secret_ascii"] as! String).utf8)
        for (counter, want) in (h["codes_by_counter"] as! [String]).enumerated() {
            let got = try OTP.hotp(secret: secret, counter: UInt64(counter), digits: 6, algorithm: .sha1)
            XCTAssertEqual(got, want, "counter \(counter)")
        }
    }

    func testTOTP() throws {
        let t = try vectors()["totp_rfc6238"] as! [String: Any]
        for c in t["cases"] as! [[String: Any]] {
            let alg = try OTP.Algorithm.parse(c["algorithm"] as! String)
            let secret = Data((c["secret_ascii"] as! String).utf8)
            let times = c["times"] as! [NSNumber]
            let want = c["codes"] as! [String]
            for (i, ts) in times.enumerated() {
                let got = try OTP.totp(secret: secret, time: Date(timeIntervalSince1970: ts.doubleValue),
                                       period: 30, digits: 8, algorithm: alg)
                XCTAssertEqual(got, want[i])
            }
        }
    }

    func testSteam() throws {
        for c in (try vectors()["steam"] as! [String: Any])["cases"] as! [[String: Any]] {
            let secret = try OTP.decodeSteamSecret(c["secret_b64"] as! String)
            let got = try OTP.steam(secret: secret, time: Date(timeIntervalSince1970: (c["time"] as! NSNumber).doubleValue))
            XCTAssertEqual(got, c["code"] as! String)
        }
    }

    func testCanonicalEdgeRoundTrip() throws {
        let edge = try Data(contentsOf: specDir().appendingPathComponent("canonical_edge.json"))
        let accounts = try CanonicalJSON.decode(edge)
        XCTAssertEqual(CanonicalJSON.encode(accounts), edge)
    }

    func testHChaCha20KAT() throws {
        let h = try vectors()["hchacha20"] as! [String: Any]
        let key = [UInt8](Data(base64Encoded: h["key_b64"] as! String)!)
        let nonce = [UInt8](Data(base64Encoded: h["nonce16_b64"] as! String)!)
        let want = Data(base64Encoded: h["subkey_b64"] as! String)!
        XCTAssertEqual(Data(XChaCha.hchacha20(key: key, nonce16: nonce)), want)
    }

    func testAEADPayload() throws {
        let a = try vectors()["aead_payload"] as! [String: Any]
        let dek = Data(base64Encoded: a["dek_b64"] as! String)!
        let nonce = Data(base64Encoded: a["nonce_b64"] as! String)!
        let ct = Data(base64Encoded: a["ct_b64"] as! String)!
        let plain = try XChaCha.open(ct, key: dek, nonce: nonce)
        XCTAssertEqual(plain, Data((a["expected_canonical_json"] as! String).utf8))
        XCTAssertEqual(try XChaCha.seal(plain, key: dek, nonce: nonce), ct)
    }

    func testArgon2idVector() throws {
        let a = try vectors()["argon2id"] as! [String: Any]
        let params = a["params"] as! [String: Any]
        for c in a["cases"] as! [[String: Any]] {
            let pass = Data(base64Encoded: c["passphrase_b64"] as! String)!
            let salt = Data(base64Encoded: c["salt_b64"] as! String)!
            let want = Data(base64Encoded: c["key_b64"] as! String)!
            let got = try argon2.deriveKey(
                passphrase: pass, salt: salt,
                memoryKiB: UInt32((params["m"] as! NSNumber).intValue),
                iterations: UInt32((params["t"] as! NSNumber).intValue),
                parallelism: UInt8((params["p"] as! NSNumber).intValue),
                keyLength: 32)
            XCTAssertEqual(got, want, "argon2id must match Go x/crypto")
        }
    }

    func testFullVaultCrossDecrypt() throws {
        let v = try vectors()["vault_crossdecrypt"] as! [String: Any]
        let envData = try JSONSerialization.data(withJSONObject: v["envelope"] as! [String: Any])
        let env = try Envelope.decode(envData)
        let accounts = try env.open(passphrase: v["passphrase"] as! String, argon2: argon2)
        XCTAssertEqual(CanonicalJSON.encode(accounts), Data((v["expected_canonical_json"] as! String).utf8))

        let neg = v["negatives"] as! [String: Any]
        XCTAssertThrowsError(try env.open(passphrase: neg["wrong_passphrase"] as! String, argon2: argon2))
    }
}
