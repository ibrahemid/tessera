import Foundation

/// The strict otpauth, strict migration and text cleaning vector sets, checked
/// against the Swift core. Text is compared as UTF-8 bytes.
func runOTPChecks(_ vectors: [String: Any], _ check: (Bool, String) -> Void) {
    checkOTPAuthStrict(vectors["otpauth_strict"] as! [String: Any], check)
    checkMigrationStrict(vectors["migration_strict"] as! [String: Any], check)
    checkTextClean(vectors["text_clean"] as! [String: Any], check)
}

private func sameText(_ a: String, _ b: Any?) -> Bool {
    guard let b = b as? String else { return false }
    return Array(a.utf8) == Array(b.utf8)
}

private func matchesVector(_ a: Account, _ w: [String: Any]) -> Bool {
    guard a.type.rawValue == w["type"] as? String,
          sameText(a.issuer, w["issuer"]),
          sameText(a.account, w["account"]),
          Base32.encodeNoPad(a.secret) == w["secret_b32"] as? String,
          a.algorithm == w["algorithm"] as? String,
          a.digits == (w["digits"] as? NSNumber)?.intValue else { return false }
    if let period = w["period"] as? NSNumber, a.period != period.intValue { return false }
    if let counter = w["counter"] as? NSNumber, a.counter != counter.int64Value { return false }
    return true
}

private func invalidMessage(_ body: () throws -> Void) -> String? {
    do {
        try body()
        return nil
    } catch AccountError.invalid(let m) {
        return m
    } catch {
        return ""
    }
}

private func checkOTPAuthStrict(_ set: [String: Any], _ check: (Bool, String) -> Void) {
    let valid = set["valid"] as! [[String: Any]]
    var failed: [String] = []
    for c in valid {
        let a = try? OTPAuth.parse(c["uri"] as! String)
        if a.map({ matchesVector($0, c) }) != true { failed.append(c["name"] as! String) }
    }
    check(!valid.isEmpty && failed.isEmpty,
          "otpauth_strict valid (\(valid.count))\(failed.isEmpty ? "" : ": " + failed.joined(separator: ", "))")

    let invalid = set["invalid"] as! [[String: Any]]
    failed = []
    for c in invalid {
        let m = invalidMessage { _ = try OTPAuth.parse(c["uri"] as! String) }
        if m?.hasPrefix("otpauth: ") != true { failed.append(c["name"] as! String) }
    }
    check(!invalid.isEmpty && failed.isEmpty,
          "otpauth_strict invalid (\(invalid.count))\(failed.isEmpty ? "" : ": " + failed.joined(separator: ", "))")
}

private func checkMigrationStrict(_ set: [String: Any], _ check: (Bool, String) -> Void) {
    let valid = set["valid"] as! [[String: Any]]
    var failed: [String] = []
    for c in valid {
        let want = c["accounts"] as! [[String: Any]]
        let got = (try? Migration.parse(c["uri"] as! String)) ?? []
        if got.count != want.count || !zip(got, want).allSatisfy(matchesVector) {
            failed.append(c["name"] as! String)
        }
    }
    check(!valid.isEmpty && failed.isEmpty,
          "migration_strict valid (\(valid.count))\(failed.isEmpty ? "" : ": " + failed.joined(separator: ", "))")

    let invalid = set["invalid"] as! [[String: Any]]
    failed = []
    for c in invalid {
        let m = invalidMessage { _ = try Migration.parse(c["uri"] as! String) }
        if m?.hasPrefix("migration: ") != true { failed.append(c["name"] as! String) }
    }
    check(!invalid.isEmpty && failed.isEmpty,
          "migration_strict invalid (\(invalid.count))\(failed.isEmpty ? "" : ": " + failed.joined(separator: ", "))")
}

private func checkTextClean(_ set: [String: Any], _ check: (Bool, String) -> Void) {
    for (key, clean) in [("text", TextClean.text), ("note", TextClean.note)] as [(String, (String) -> String)] {
        let cases = set[key] as! [[String: Any]]
        var failed: [String] = []
        for c in cases where !sameText(clean(c["in"] as! String), c["out"]) {
            failed.append(c["name"] as! String)
        }
        check(!cases.isEmpty && failed.isEmpty,
              "text_clean \(key) (\(cases.count))\(failed.isEmpty ? "" : ": " + failed.joined(separator: ", "))")
    }
    // JSONSerialization drops a string's leading U+FEFF, so the vectors that start with one are repeated here.
    check(TextClean.text("\u{feff}ACME\u{feff}") == "ACME" && TextClean.note("\u{feff}a\u{202e}b\u{2066}c") == "abc",
          "text_clean leading U+FEFF")
}
