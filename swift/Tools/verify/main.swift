import Foundation

// Local interop verifier: compiles TesseraCore with this entrypoint via swiftc
// and checks the Swift core against the shared /spec vectors. Argument 1 is the
// path to the repo's /spec directory.

var failures = 0
func check(_ cond: Bool, _ msg: String) {
    if cond { print("ok   - \(msg)") } else { print("FAIL - \(msg)"); failures += 1 }
}

guard CommandLine.arguments.count >= 2 else {
    FileHandle.standardError.write(Data("usage: verify <spec-dir>\n".utf8)); exit(2)
}
let specDir = CommandLine.arguments[1]
let vectorsURL = URL(fileURLWithPath: specDir).appendingPathComponent("testvectors.json")
let raw = try Data(contentsOf: vectorsURL)
let V = try JSONSerialization.jsonObject(with: raw) as! [String: Any]

func dict(_ k: String, _ o: [String: Any]) -> [String: Any] { o[k] as! [String: Any] }
func arr(_ k: String, _ o: [String: Any]) -> [[String: Any]] { o[k] as! [[String: Any]] }

// 1. HOTP RFC 4226
do {
    let h = dict("hotp_rfc4226", V)
    let secret = Data((h["_secret_ascii"] as! String).utf8)
    let codes = h["codes_by_counter"] as! [String]
    var allOK = true
    for (counter, want) in codes.enumerated() {
        let got = try OTP.hotp(secret: secret, counter: UInt64(counter), digits: 6, algorithm: .sha1)
        if got != want { allOK = false }
    }
    check(allOK, "HOTP RFC 4226 (\(codes.count) counters)")
}

// 2. TOTP RFC 6238
do {
    let t = dict("totp_rfc6238", V)
    var allOK = true
    for c in arr("cases", t) {
        let alg = try OTP.Algorithm.parse(c["algorithm"] as! String)
        let secret = Data((c["secret_ascii"] as! String).utf8)
        let times = c["times"] as! [NSNumber]
        let want = c["codes"] as! [String]
        for (i, ts) in times.enumerated() {
            let got = try OTP.totp(secret: secret, time: Date(timeIntervalSince1970: ts.doubleValue),
                                   period: 30, digits: 8, algorithm: alg)
            if got != want[i] { allOK = false }
        }
    }
    check(allOK, "TOTP RFC 6238 (SHA1/256/512)")
}

// 3. Steam
do {
    let s = dict("steam", V)
    var allOK = true
    for c in arr("cases", s) {
        let secret = try OTP.decodeSteamSecret(c["secret_b64"] as! String)
        let ts = (c["time"] as! NSNumber).doubleValue
        let got = try OTP.steam(secret: secret, time: Date(timeIntervalSince1970: ts))
        if got != (c["code"] as! String) || got.count != 5 { allOK = false }
    }
    check(allOK, "Steam Guard codes")
}

// 4. base32
do {
    let b = dict("base32", V)
    var allOK = true
    for c in arr("encode", b) {
        if Base32.encode(Data((c["ascii"] as! String).utf8)) != (c["b32"] as! String) { allOK = false }
    }
    for c in arr("decode_lenient", b) {
        let got = try Base32.decode(c["input"] as! String)
        if String(decoding: got, as: UTF8.self) != (c["ascii"] as! String) { allOK = false }
    }
    check(allOK, "base32 encode + lenient decode")
}

// 5. otpauth parse + round-trip
do {
    let o = dict("otpauth_uri", V)
    var allOK = true
    for c in arr("parse", o) {
        let a = try OTPAuth.parse(c["uri"] as! String)
        if a.issuer != (c["issuer"] as! String) || a.account != (c["account"] as! String) { allOK = false }
        if a.type.rawValue != (c["type"] as! String) || a.digits != (c["digits"] as! Int) { allOK = false }
        let wantSecret = try Base32.decode(c["secret_b32"] as! String)
        if a.secret != wantSecret { allOK = false }
        // round-trip
        let reparsed = try OTPAuth.parse(OTPAuth.format(a))
        if reparsed.secret != a.secret || reparsed.issuer != a.issuer { allOK = false }
        if reparsed.type != a.type || reparsed.digits != a.digits { allOK = false }
    }
    check(allOK, "otpauth parse + format round-trip")
}

// 6. canonical JSON edge round-trip (byte-for-byte vs Go ground truth)
do {
    let edgeURL = URL(fileURLWithPath: specDir).appendingPathComponent("canonical_edge.json")
    let edge = try Data(contentsOf: edgeURL)
    let accounts = try CanonicalJSON.decode(edge)
    let reencoded = CanonicalJSON.encode(accounts)
    check(reencoded == edge, "canonical JSON edge re-encode is byte-identical to Go")
}

// 6b. HChaCha20 known-answer test (localizes subkey-derivation bugs)
do {
    let h = dict("hchacha20", V)
    let key = [UInt8](Data(base64Encoded: h["key_b64"] as! String)!)
    let nonce = [UInt8](Data(base64Encoded: h["nonce16_b64"] as! String)!)
    let want = Data(base64Encoded: h["subkey_b64"] as! String)!
    let got = Data(XChaCha.hchacha20(key: key, nonce16: nonce))
    check(got == want, "HChaCha20 KAT (draft-irtf-cfrg-xchacha)")
}

// 7. XChaCha20-Poly1305 AEAD + canonical decode (fixed DEK, no argon2id)
do {
    let a = dict("aead_payload", V)
    let dek = Data(base64Encoded: a["dek_b64"] as! String)!
    let nonce = Data(base64Encoded: a["nonce_b64"] as! String)!
    let ct = Data(base64Encoded: a["ct_b64"] as! String)!
    let plain = try XChaCha.open(ct, key: dek, nonce: nonce)
    let expected = Data((a["expected_canonical_json"] as! String).utf8)
    check(plain == expected, "XChaCha20-Poly1305 open == Go canonical payload")
    // and it parses into accounts
    let accounts = try CanonicalJSON.decode(plain)
    check(accounts.count == 2 && accounts[0].issuer == "ACME", "decrypted payload parses to accounts")

    // negative: tamper a byte
    var bad = ct; bad[bad.count - 1] ^= 0x01
    var threw = false
    do { _ = try XChaCha.open(bad, key: dek, nonce: nonce) } catch { threw = true }
    check(threw, "tampered AEAD ciphertext is rejected")

    // round-trip seal -> open
    let sealed = try XChaCha.seal(plain, key: dek, nonce: nonce)
    let reopened = try XChaCha.open(sealed, key: dek, nonce: nonce)
    check(reopened == plain, "XChaCha seal/open round-trip")
    check(sealed == ct, "Swift seal reproduces Go ciphertext byte-for-byte")
}

// 7b. Google migration import
do {
    let m = dict("migration", V)
    var allOK = true
    for c in arr("cases", m) {
        let accounts = try Migration.parse(c["uri"] as! String)
        let want = c["accounts"] as! [[String: Any]]
        if accounts.count != want.count { allOK = false }
        for (i, a) in accounts.enumerated() {
            if a.issuer != (want[i]["issuer"] as! String) { allOK = false }
            if a.account != (want[i]["account"] as! String) { allOK = false }
            if Base32.encodeNoPad(a.secret) != (want[i]["secret_b32"] as! String) { allOK = false }
        }
    }
    check(allOK, "Google otpauth-migration import")
}

// 8. duplicate-key rejection
do {
    let dup = Data(#"[{"id":"a","id":"b","type":"totp","account":"x","algorithm":"SHA1","secret":"AA==","digits":6,"period":30,"counter":0,"folder":"","issuer":"","pinned":false,"tags":[],"created_at":0,"updated_at":0}]"#.utf8)
    var threw = false
    do { _ = try CanonicalJSON.decode(dup) } catch { threw = true }
    check(threw, "canonical decode rejects duplicate keys")
}

// 9. Envelope create/open/reseal round-trip (stub argon2; real argon2 in XCTest)
struct StubArgon2: Argon2idProvider {
    func deriveKey(passphrase: Data, salt: Data, memoryKiB: UInt32, iterations: UInt32,
                   parallelism: UInt8, keyLength: Int) throws -> Data {
        let p = [UInt8](passphrase), s = [UInt8](salt)
        var out = [UInt8](repeating: 0, count: keyLength)
        for i in 0..<keyLength {
            out[i] = (p.isEmpty ? 0 : p[i % p.count]) ^ (s.isEmpty ? 0 : s[i % s.count]) ^ UInt8(i & 0xff)
        }
        return Data(out)
    }
}
do {
    let stub = StubArgon2()
    let a1 = Account(id: "a", type: .totp, issuer: "X", account: "y",
                     secret: Data("12345678901234567890".utf8), algorithm: "SHA1", digits: 6, period: 30)
    var env = try Envelope.create(accounts: [a1], passphrase: "pw", argon2: stub)
    let opened = try env.open(passphrase: "pw", argon2: stub)
    check(opened.count == 1 && opened[0].issuer == "X", "Envelope.create + open round-trip")

    var threw = false
    do { _ = try env.open(passphrase: "wrong", argon2: stub) } catch { threw = true }
    check(threw, "wrong passphrase rejected (stub)")

    // reseal must preserve a non-passphrase wrap.
    env.wraps.append(VaultWrap(type: "secure-enclave", kdf: nil, params: nil, salt: nil,
                               se_key: "AA==", nonce: "AA==", ct: "AA=="))
    let a2 = Account(id: "b", type: .totp, issuer: "Z", account: "w",
                     secret: Data("abcdefghij".utf8), algorithm: "SHA1", digits: 6, period: 30)
    let resealed = try env.reseal(accounts: [a1, a2], passphrase: "pw", argon2: stub)
    check(resealed.wraps.contains { $0.type == "secure-enclave" }, "reseal preserves secure-enclave wrap")
    check(try resealed.open(passphrase: "pw", argon2: stub).count == 2, "reseal updates accounts")
}

print("")
if failures == 0 { print("ALL SWIFT INTEROP CHECKS PASSED") } else { print("\(failures) CHECK(S) FAILED") }
exit(failures == 0 ? 0 : 1)
