import Foundation

// Local interop verifier: compiles TesseraCore with this entrypoint via swiftc
// and checks the Swift core against the shared /spec vectors. Argument 1 is the
// path to the repo's /spec directory. Optional argument 2 is the path to
// go/internal/importers/testdata, which adds the shared importer fixture table
// — the only importer coverage available without full Xcode.

var failures = 0
func check(_ cond: Bool, _ msg: String) {
    if cond { print("ok   - \(msg)") } else { print("FAIL - \(msg)"); failures += 1 }
}

guard CommandLine.arguments.count >= 2 else {
    FileHandle.standardError.write(Data("usage: verify <spec-dir> [importer-testdata-dir]\n".utf8)); exit(2)
}
let specDir = CommandLine.arguments[1]
let testdataDir: String? = CommandLine.arguments.count >= 3 ? CommandLine.arguments[2] : nil
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

    let rejects = arr("decode_reject", b)
    var rejectOK = !rejects.isEmpty
    for c in rejects {
        let input = c["input"] as! String
        if let got = try? Base32.decode(input) {
            print("     accepted \(input.count) chars -> \(got.count) bytes, want error (\(c["why"] as! String))")
            rejectOK = false
        }
    }
    check(rejectOK, "base32 strict decode rejects (\(rejects.count) vectors)")
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
        let emitted = OTPAuth.format(a)
        if emitted.contains("+") && !a.issuer.contains("+") && !a.account.contains("+") {
            print("     emitted a bare '+': \(emitted)")
            allOK = false
        }
        let reparsed = try OTPAuth.parse(emitted)
        if reparsed.secret != a.secret || reparsed.issuer != a.issuer { allOK = false }
        if reparsed.account != a.account { allOK = false }
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
                               se_key: "AA==", nonce: Data(repeating: 7, count: 24).base64EncodedString(), ct: "AA=="))
    let a2 = Account(id: "b", type: .totp, issuer: "Z", account: "w",
                     secret: Data("abcdefghij".utf8), algorithm: "SHA1", digits: 6, period: 30)
    let resealed = try env.reseal(accounts: [a1, a2], passphrase: "pw", argon2: stub)
    check(resealed.wraps.contains { $0.type == "secure-enclave" }, "reseal preserves secure-enclave wrap")
    check(try resealed.open(passphrase: "pw", argon2: stub).count == 2, "reseal updates accounts")
}

// 10. Handles: deterministic assignment + canonical serialization vectors.
do {
    let h = dict("handles", V)
    var allOK = true
    for c in arr("assignment", h) {
        var accts = (c["accounts"] as! [[String: Any]]).map { a in
            Account(id: a["id"] as! String, type: .totp, issuer: a["issuer"] as? String ?? "",
                    account: a["account"] as? String ?? "", secret: Data("x".utf8),
                    handle: a["handle"] as? String ?? "", createdAt: Int64((a["created_at"] as! NSNumber).intValue))
        }
        Handles.assign(&accts)
        let want = c["expected"] as! [String: String]
        for a in accts where a.handle != want[a.id] { allOK = false }
        // Assignment is idempotent: a second pass changes nothing.
        var again = accts
        if Handles.assign(&again) || again.map(\.handle) != accts.map(\.handle) { allOK = false }
    }
    check(allOK, "handle assignment vectors (deterministic, idempotent)")

    let canon = dict("canonical", h)["expected_canonical_json"] as! String
    let a = Account(id: "00000000-0000-4000-8000-000000000010", type: .totp, issuer: "ACME",
                    account: "alice@example.com", secret: Data("12345678901234567890".utf8),
                    algorithm: "SHA1", digits: 6, period: 30, handle: "ac",
                    createdAt: 1700000000, updatedAt: 1700000000)
    let got = String(decoding: CanonicalJSON.encode([a]), as: UTF8.self)
    check(got == canon, "handle canonical serialization is byte-identical to Go")
}

// 12. Version 2 vectors, read through the strict parser so strings and
// numbers keep the exact bytes Go decodes.
struct VectorMismatch: Error, CustomStringConvertible {
    let description: String
}
let vectorBytes = [UInt8](raw)
let vectorRoot = try StrictJSON.parse(vectorBytes, rule: .finiteDouble)
func vnode(_ n: JSONNode, _ k: String) -> JSONNode { n.member(k)! }
func vtext(_ n: JSONNode) -> String { String(decoding: n.string, as: UTF8.self) }
func vtext(_ n: JSONNode, _ k: String) -> String { vtext(vnode(n, k)) }
func vbytes(_ n: JSONNode, _ k: String) -> Data { Data(vnode(n, k).string) }
func vint(_ n: JSONNode) -> Int64 { Int64(String(decoding: vectorBytes[n.range], as: UTF8.self))! }
func vint(_ n: JSONNode, _ k: String) -> Int64 { vint(vnode(n, k)) }
func vraw(_ n: JSONNode) -> [UInt8] { Array(vectorBytes[n.range]) }
func vtexts(_ n: JSONNode, _ k: String) -> [String] { vnode(n, k).items.map { vtext($0) } }

func payloadErrorClass(_ error: Error) -> String {
    switch error {
    case PayloadError.malformed: return "malformed_payload"
    case PayloadError.duplicateKey: return "duplicate_key"
    case PayloadError.nonCanonicalNumber: return "non_canonical_number"
    case PayloadError.fieldType: return "field_type"
    case PayloadError.nonCanonicalBase64: return "non_canonical_base64"
    case PayloadError.counterRange: return "counter_range"
    default: return "other (\(error))"
    }
}

func envelopeErrorClass(_ error: Error) -> String {
    switch error {
    case VaultError.corrupt: return "corrupt"
    case VaultError.wrongPassphrase: return "auth_failed"
    case VaultError.unsupportedVersion: return "unsupported_version"
    case VaultError.unsupportedAEAD: return "unsupported_aead"
    case VaultError.noPassphraseWrap: return "no_passphrase_wrap"
    case EnvelopeError.noWraps: return "no_wraps"
    case EnvelopeError.tooManyWraps: return "too_many_wraps"
    case EnvelopeError.appOnlyVault: return "app_only"
    default: return "other (\(error))"
    }
}

// 12a. Strict payload reader.
do {
    let p = vnode(vectorRoot, "payload_strict")
    var allOK = true
    let valid = vnode(p, "valid").items
    for c in valid {
        let payload = vbytes(c, "payload")
        do {
            if CanonicalJSON.encode(try CanonicalJSON.decode(payload)) != payload {
                allOK = false
                print("     \(vtext(c, "name")): round trip changed the payload")
            }
        } catch {
            allOK = false
            print("     \(vtext(c, "name")): \(error)")
        }
    }
    let invalid = vnode(p, "invalid").items
    for c in invalid {
        var got = "accepted"
        do { _ = try CanonicalJSON.decode(vbytes(c, "payload")) } catch { got = payloadErrorClass(error) }
        if got != vtext(c, "error") {
            allOK = false
            print("     \(vtext(c, "name")): \(got), want \(vtext(c, "error"))")
        }
    }
    check(allOK, "payload_strict (\(valid.count) valid, \(invalid.count) invalid)")
}

// 12b. Version 2 canonical keys and format version.
do {
    let cases = vnode(vnode(vectorRoot, "vault_v2"), "canonical").items
    var allOK = true
    for c in cases {
        let name = vtext(c, "name")
        do {
            var accts = try CanonicalJSON.decode(vbytes(c, "input"))
            let clear = c.member("clear").map { $0.items.map { vtext($0) } } ?? []
            for i in accts.indices {
                for key in clear {
                    switch key {
                    case "deleted_at": accts[i].deletedAt = 0
                    case "note": accts[i].note = ""
                    case "order": accts[i].order = 0
                    default:
                        guard accts[i].extensions.contains(where: { $0.key == key }) else {
                            throw VectorMismatch(description: "clear \(key): key not present")
                        }
                        accts[i].extensions.removeAll { $0.key == key }
                    }
                }
            }
            if CanonicalJSON.encode(accts) != vbytes(c, "expected_canonical_json") {
                throw VectorMismatch(description: "canonical mismatch")
            }
            if Account.formatVersion(accts) != Int(vint(c, "expected_version")) {
                throw VectorMismatch(description: "format version \(Account.formatVersion(accts))")
            }
        } catch {
            allOK = false
            print("     \(name): \(error)")
        }
    }
    check(allOK, "vault_v2 canonical (\(cases.count) cases)")

    let v2 = vnode(vectorRoot, "vault_v2")
    let env = try Envelope.decode(Data(vraw(vnode(v2, "envelope"))))
    check(env.version == Int(vint(v2, "expected_version")) && env.wrapTypes == ["passphrase"] && !env.needsPersist,
          "vault_v2 envelope decodes as version 2 (argon2id open in XCTest)")
}

// 12c. canonical_edge_v2.json round-trip.
do {
    let edge = try Data(contentsOf: URL(fileURLWithPath: specDir).appendingPathComponent("canonical_edge_v2.json"))
    let accts = try CanonicalJSON.decode(edge)
    check(CanonicalJSON.encode(accts) == edge && Account.formatVersion(accts) == 2,
          "canonical_edge_v2 re-encode is byte-identical to Go")
}

// 12d. Strict envelope reader. The stub argon2 cannot open these wraps, so
// valid cases are checked at decode and auth_failed cases fail as auth_failed
// either way; XCTest reruns every case with argon2id.
do {
    let es = vnode(vectorRoot, "envelope_strict")
    let valid = vnode(es, "valid").items
    var validOK = true
    for c in valid {
        do { _ = try Envelope.decode(vbytes(c, "envelope_json")) } catch {
            validOK = false
            print("     \(vtext(c, "name")): \(error)")
        }
    }
    check(validOK, "envelope_strict valid cases decode (\(valid.count))")

    let invalid = vnode(es, "invalid").items
    var invalidOK = true
    for c in invalid {
        let pass = c.member("passphrase").map { vtext($0) } ?? vtext(es, "passphrase")
        var got = "opened"
        do {
            let env = try Envelope.decode(vbytes(c, "envelope_json"))
            _ = try env.open(passphrase: pass, argon2: StubArgon2())
        } catch {
            got = envelopeErrorClass(error)
        }
        if got != vtext(c, "error") {
            invalidOK = false
            print("     \(vtext(c, "name")): \(got), want \(vtext(c, "error"))")
        }
    }
    check(invalidOK, "envelope_strict invalid cases fail with their error class (\(invalid.count))")

    let preserve = vnode(es, "preserve")
    let original = vbytes(preserve, "envelope_json")
    let env = try Envelope.decode(original)
    let before = try JSONSerialization.jsonObject(with: original) as! NSDictionary
    let after = try JSONSerialization.jsonObject(with: env.encoded()) as! NSDictionary
    check(before.isEqual(after), "envelope_strict preserve: re-marshal keeps unknown keys and wraps")
}

// 12e. Rotation from the vector's old DEK, with stub-derived passphrase wraps.
do {
    let r = vnode(vectorRoot, "rotation")
    let oldDEK = Data(base64Encoded: vtext(r, "old_dek_b64"))!
    let original = vraw(vnode(r, "envelope"))
    let expected = vbytes(r, "expected_canonical_json")
    var env = try Envelope.decode(Data(original))
    let accts = try env.open(dek: oldDEK)
    check(CanonicalJSON.encode(accts) == expected, "rotation vector opens with its old DEK")

    let newPass = vtext(r, "new_passphrase")
    let opened = Set(env.wraps.indices.filter { env.wraps[$0].type == "passphrase" }.prefix(1))
    let result = try env.rotate(accounts: accts, passphrases: [newPass], recreating: opened, argon2: StubArgon2())
    check(result.removed == vtexts(r, "expected_removed"), "rotation removed list matches Go")
    check(env.wrapTypes == vtexts(r, "expected_wrap_types"), "rotation wrap types match Go")

    let out = [UInt8](try env.encoded())
    let outTop = try StrictJSON.parse(out, rule: .finiteDouble)
    let origTop = try StrictJSON.parse(original, rule: .finiteDouble)
    let kept = vtexts(r, "kept_top_level").allSatisfy { k in
        guard let a = origTop.member(k), let b = outTop.member(k) else { return false }
        return StrictJSON.compact(original[a.range]) == StrictJSON.compact(out[b.range])
    }
    check(kept, "rotation keeps unknown top-level keys")

    let reopened = try Envelope.decode(Data(out))
    let reopenedAccts = try reopened.open(passphrase: newPass, argon2: StubArgon2())
    check(CanonicalJSON.encode(reopenedAccts) == expected && reopened.version == Int(vint(r, "expected_version")),
          "rotated envelope reopens with the new passphrase")
    var oldDEKRefused = false
    do { _ = try reopened.open(dek: oldDEK) } catch VaultError.wrongPassphrase { oldDEKRefused = true }
    check(oldDEKRefused, "rotated envelope refuses the old DEK")
}

// 12f. HOTP counter range.
do {
    let h = vnode(vectorRoot, "hotp_counter_range")
    check(vint(h, "max_counter") == Account.maxCounter, "max HOTP counter matches Go")
    var allOK = true
    for c in vnode(h, "payload_accept").items {
        let payload = vbytes(c, "payload")
        let accts = try? CanonicalJSON.decode(payload)
        if accts?.count != 1 || accts?.first?.counter != vint(c, "counter")
            || accts.map(CanonicalJSON.encode) != payload { allOK = false }
    }
    for c in vnode(h, "payload_reject").items {
        var got = "accepted"
        do { _ = try CanonicalJSON.decode(vbytes(c, "payload")) } catch { got = payloadErrorClass(error) }
        if got != vtext(c, "error") { allOK = false }
    }
    for c in vnode(h, "advance").items where (try? Account.nextCounter(after: vint(c, "from"))) != vint(c, "to") {
        allOK = false
    }
    for c in vnode(h, "advance_refused").items {
        do {
            _ = try Account.nextCounter(after: vint(c))
            allOK = false
        } catch is CounterRangeError {}
    }
    check(allOK, "hotp_counter_range accept, reject, advance, advance refused")

    let m = vnode(h, "code_at_max")
    let acct = Account(id: "max", type: .hotp, issuer: "", account: "x",
                       secret: Data(base64Encoded: vtext(m, "secret_b64"))!, algorithm: vtext(m, "algorithm"),
                       digits: Int(vint(m, "digits")), counter: vint(m, "counter"))
    check((try? OTP.code(for: acct, at: Date())) == vtext(m, "code"), "HOTP code at the maximum counter")
}

// 12g. Display order.
do {
    let cases = vnode(vnode(vectorRoot, "display_order"), "cases").items
    var allOK = true
    for c in cases {
        let accts = vnode(c, "accounts").items.map { a in
            Account(id: vtext(a, "id"), type: .totp, issuer: a.member("issuer").map { vtext($0) } ?? "",
                    account: a.member("account").map { vtext($0) } ?? "", secret: Data("x".utf8),
                    deletedAt: a.member("deleted_at").map { vint($0) } ?? 0,
                    order: a.member("order").map { Int(vint($0)) } ?? 0)
        }
        let ids = Account.sortedForDisplay(Account.live(accts)).map(\.id)
        if ids != vtexts(c, "expected_ids") {
            allOK = false
            print("     \(vtext(c, "name")): \(ids)")
        }
    }
    check(allOK, "display_order (\(cases.count) cases)")
}

// 11. Shared importer fixture table (go/internal/importers/testdata/expected.json).
// Same fixtures, same expectations as the Go TestTestdataTable; rows whose
// "cores" omit "swift" are skipped (the .1pux zip container is CLI-only).
if let testdataDir {
    let dir = URL(fileURLWithPath: testdataDir)
    let table = try JSONSerialization.jsonObject(
        with: Data(contentsOf: dir.appendingPathComponent("expected.json"))) as! [[String: Any]]
    check(!table.isEmpty, "expected.json is not empty")
    for entry in table {
        let file = entry["file"] as! String
        let cores = entry["cores"] as? [String] ?? ["go", "swift"]
        if !cores.contains("swift") {
            print("skip - \(file) (cores: \(cores.joined(separator: ",")))")
            continue
        }
        do {
            try runImporterFixture(entry, dir.appendingPathComponent(file))
            check(true, "fixture \(file)")
        } catch {
            print("     \(error)")
            check(false, "fixture \(file)")
        }
    }
}

runOTPChecks(V, check)

print("")
if failures == 0 { print("ALL SWIFT INTEROP CHECKS PASSED") } else { print("\(failures) CHECK(S) FAILED") }
exit(failures == 0 ? 0 : 1)

// MARK: - importer fixture table

struct FixtureMismatch: Error, CustomStringConvertible {
    let description: String
    init(_ d: String) { description = d }
}

/// Runs one expected.json row: parses the fixture the way its "via" says, then
/// asserts the accounts positionally or the rejection message by substring.
func runImporterFixture(_ entry: [String: Any], _ path: URL) throws {
    let data = try Data(contentsOf: path)
    let via = entry["via"] as? String ?? "importers"
    let wantSource = entry["source"] as? String
    let reject = entry["reject"] as? String
    let wantMessage = entry["message_contains"] as? String ?? ""

    var accounts: [Account] = []
    var source = ""
    var thrown: Error?
    switch via {
    case "importers":
        var found: (accounts: [Account], source: String)?
        do { found = try Importers.parse(data) } catch { thrown = error }
        if thrown == nil {
            guard let found else {
                throw FixtureMismatch("Importers.parse did not recognize the fixture as an export")
            }
            accounts = found.accounts
            source = found.source
        }
    case "detect":
        guard let text = String(data: data, encoding: .utf8) else {
            throw FixtureMismatch("fixture is not valid UTF-8")
        }
        let (accs, errs) = InputDetect.parseText(text)
        accounts = accs
        if let first = errs.first { thrown = FixtureMismatch(first.reason) }
    default:
        throw FixtureMismatch("unknown via \"\(via)\"")
    }

    if let reject {
        guard let thrown else {
            throw FixtureMismatch("expected the export to be rejected (\(reject)), got \(accounts.count) accounts")
        }
        let text = "\(thrown)"
        guard text.contains(wantMessage) else {
            throw FixtureMismatch("error \"\(text)\" does not contain \"\(wantMessage)\"")
        }
        return
    }
    if let thrown { throw FixtureMismatch("parse: \(thrown)") }
    if let wantSource, source != wantSource {
        throw FixtureMismatch("source = \"\(source)\", want \"\(wantSource)\"")
    }
    try assertImportedAccounts(accounts, entry["accounts"] as! [[String: Any]])
}

func assertImportedAccounts(_ got: [Account], _ want: [[String: Any]]) throws {
    guard got.count == want.count else {
        throw FixtureMismatch("got \(got.count) accounts, want \(want.count)")
    }
    for (i, w) in want.enumerated() {
        let a = got[i]
        func fail(_ field: String, _ gotValue: Any, _ wantValue: Any) -> FixtureMismatch {
            FixtureMismatch("account \(i): \(field) = \(gotValue), want \(wantValue)")
        }
        let wantType = w["type"] as! String
        if a.type.rawValue != wantType { throw fail("type", a.type.rawValue, wantType) }
        let wantIssuer = w["issuer"] as! String
        if a.issuer != wantIssuer { throw fail("issuer", a.issuer, wantIssuer) }
        let wantAccount = w["account"] as! String
        if a.account != wantAccount { throw fail("account", a.account, wantAccount) }
        if a.secret != (try Base32.decode(w["secret_b32"] as! String)) {
            throw FixtureMismatch("account \(i): secret mismatch")
        }
        let wantAlgo = w["algorithm"] as! String
        if a.algorithm != wantAlgo { throw fail("algorithm", a.algorithm, wantAlgo) }
        let wantDigits = (w["digits"] as! NSNumber).intValue
        if a.digits != wantDigits { throw fail("digits", a.digits, wantDigits) }
        let wantPeriod = (w["period"] as! NSNumber).intValue
        if a.period != wantPeriod { throw fail("period", a.period, wantPeriod) }
        let wantCounter = (w["counter"] as! NSNumber).int64Value
        if a.counter != wantCounter { throw fail("counter", a.counter, wantCounter) }
        let wantNote = w["note"] as? String ?? ""
        if Array(a.note.utf8) != Array(wantNote.utf8) { throw fail("note", a.note.debugDescription, wantNote.debugDescription) }
    }
}
