import Foundation

/// The `rotation_extra_passphrase` vector set, with stub-derived passphrase
/// wraps: a rotation that recreates either passphrase wrap removes the other
/// wraps and keeps unknown top-level keys, and the backup copy keeps only the
/// passphrase wraps. XCTest reruns the rotation with argon2id.
func runEnvelopeChecks(_ root: JSONNode, _ check: (Bool, String) -> Void) {
    let r = vnode(root, "rotation_extra_passphrase")
    let original = vraw(vnode(r, "envelope"))
    let expected = vbytes(r, "expected_canonical_json")
    let newPass = vtext(r, "new_passphrase")
    guard let oldDEK = Data(base64Encoded: vtext(r, "old_dek_b64")),
          let env = try? Envelope.decode(Data(original)),
          let accts = try? env.open(dek: oldDEK) else {
        check(false, "rotation_extra_passphrase opens with its old DEK")
        return
    }
    check(CanonicalJSON.encode(accts) == expected, "rotation_extra_passphrase opens with its old DEK")

    let passphraseWraps = env.wraps.indices.filter { env.wraps[$0].type == "passphrase" }
    var allOK = passphraseWraps.count == 2
    for index in passphraseWraps {
        var rotated = env
        do {
            let result = try rotated.rotate(accounts: accts, passphrases: [newPass], recreating: [index],
                                            argon2: StubArgon2())
            let out = [UInt8](try rotated.encoded())
            let reopened = try Envelope.decode(Data(out))
            let kept = try vtexts(r, "kept_top_level").allSatisfy { k in
                try topLevel(out, k) == topLevel(original, k)
            }
            let refusesOldDEK = (try? reopened.open(dek: oldDEK)) == nil
            let reopenedAccts = try reopened.open(passphrase: newPass, argon2: StubArgon2())
            if result.removed != vtexts(r, "expected_removed")
                || rotated.wrapTypes != vtexts(r, "expected_wrap_types")
                || !kept || !refusesOldDEK
                || CanonicalJSON.encode(reopenedAccts) != expected
                || reopened.version != Int(vint(r, "expected_version")) {
                allOK = false
                print("     recreating wrap \(index): removed \(result.removed), wraps \(rotated.wrapTypes)")
            }
        } catch {
            allOK = false
            print("     recreating wrap \(index): \(error)")
        }
    }
    check(allOK, "rotation_extra_passphrase removes the other wraps and keeps unknown keys")

    do {
        let copy = try env.passphraseWrapsOnly()
        let out = [UInt8](try copy.encoded())
        let sameRest = try ["version", "aead", "payload", "x_future"].allSatisfy { k in
            try topLevel(out, k) == topLevel(original, k)
        }
        let sameWraps = try copy.wrapTypes == ["passphrase", "passphrase"]
            && wraps(out) == wraps(original).filter { w in
                (try? StrictJSON.parse(w, rule: .finiteDouble).member("type")?.string) == Array("passphrase".utf8)
            }
        let opens = (try? Envelope.decode(Data(out)).open(dek: oldDEK)).map(CanonicalJSON.encode) == expected
        check(sameRest && sameWraps && opens, "backup copy keeps only the passphrase wraps")
    } catch {
        check(false, "backup copy: \(error)")
    }

    var appOnly = env
    appOnly.wraps = env.wraps.filter { $0.type != "passphrase" }
    var refused = false
    do { _ = try appOnly.passphraseWrapsOnly() } catch EnvelopeError.backupNeedsPassphrase { refused = true } catch {}
    check(refused, "backup copy refuses an envelope without a passphrase wrap")
}

private func topLevel(_ envelope: [UInt8], _ key: String) throws -> [UInt8]? {
    let root = try StrictJSON.parse(envelope, rule: .finiteDouble)
    return root.member(key).map { StrictJSON.compact(envelope[$0.range]) }
}

private func wraps(_ envelope: [UInt8]) throws -> [[UInt8]] {
    let root = try StrictJSON.parse(envelope, rule: .finiteDouble)
    return (root.member("wraps")?.items ?? []).map { StrictJSON.compact(envelope[$0.range]) }
}
