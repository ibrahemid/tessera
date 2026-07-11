import XCTest
import Foundation
@testable import TesseraCore

/// Locates the repo's /spec directory by walking up from this source file.
private func specDirURL() -> URL {
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

private func handleVectors() throws -> [String: Any] {
    let data = try Data(contentsOf: specDirURL().appendingPathComponent("testvectors.json"))
    let v = try JSONSerialization.jsonObject(with: data) as! [String: Any]
    return v["handles"] as! [String: Any]
}

/// Handle parity with the Go core. The `assignment` vectors carry the spec's
/// worked examples (GitHub→gi collision chain, acct verbatim, user-edited skip),
/// so consuming them IS the worked-examples test.
final class HandleTests: XCTestCase {

    private func accounts(_ raw: [[String: Any]]) -> [Account] {
        raw.map { a in
            Account(id: a["id"] as! String, type: .totp, issuer: a["issuer"] as? String ?? "",
                    account: a["account"] as? String ?? "", secret: Data("x".utf8),
                    handle: a["handle"] as? String ?? "",
                    createdAt: Int64((a["created_at"] as! NSNumber).intValue))
        }
    }

    func testAssignmentVectors() throws {
        let cases = try handleVectors()["assignment"] as! [[String: Any]]
        XCTAssertFalse(cases.isEmpty, "no handle assignment vectors loaded")
        for c in cases {
            let name = c["name"] as! String
            var accts = accounts(c["accounts"] as! [[String: Any]])
            Handles.assign(&accts)
            let want = c["expected"] as! [String: String]
            for a in accts {
                XCTAssertEqual(a.handle, want[a.id], "case \(name): account \(a.id)")
            }
        }
    }

    /// Migration "assigns once": a second pass over already-assigned accounts
    /// changes nothing and reports no work, which is what makes the app's
    /// re-seal-once-on-unlock guard hold.
    func testAssignmentIsIdempotent() throws {
        let cases = try handleVectors()["assignment"] as! [[String: Any]]
        for c in cases {
            var accts = accounts(c["accounts"] as! [[String: Any]])
            XCTAssertTrue(Handles.assign(&accts))   // first pass assigns
            let snapshot = accts
            XCTAssertFalse(Handles.assign(&accts))  // second pass is a no-op
            XCTAssertEqual(accts, snapshot)
        }
    }

    func testCanonicalSerializationVector() throws {
        let canon = try handleVectors()["canonical"] as! [String: Any]
        let want = canon["expected_canonical_json"] as! String
        let a = Account(id: "00000000-0000-4000-8000-000000000010", type: .totp, issuer: "ACME",
                        account: "alice@example.com", secret: Data("12345678901234567890".utf8),
                        algorithm: "SHA1", digits: 6, period: 30, handle: "ac",
                        createdAt: 1700000000, updatedAt: 1700000000)
        XCTAssertEqual(String(decoding: CanonicalJSON.encode([a]), as: UTF8.self), want)
    }

    func testCharsetValidation() {
        for good in ["gi", "ac2", "x1p", "a", "abcdefghijkl"] {
            XCTAssertTrue(Handles.isValid(good), good)
        }
        // Leading digit, uppercase, punctuation, empty, over 12 chars, leading space.
        for bad in ["2fa", "Gi", "a-b", "", "abcdefghijklm", " gi", "1p"] {
            XCTAssertFalse(Handles.isValid(bad), bad)
        }
    }

    func testUniquenessRejectsDuplicate() {
        let a = Account(id: "a", type: .totp, issuer: "X", account: "", secret: Data("x".utf8), handle: "gi")
        let b = Account(id: "b", type: .totp, issuer: "Y", account: "", secret: Data("x".utf8), handle: "gi")
        XCTAssertThrowsError(try Handles.checkUniqueness([a, b]))
        XCTAssertNoThrow(try Handles.checkUniqueness([a]))
    }

    /// A handle freed by a user edit becomes available to the next auto-assignment
    /// without renumbering the accounts that already hold handles.
    func testFreedHandleReused() {
        var accts = [
            Account(id: "a", type: .totp, issuer: "ACME", account: "", secret: Data("x".utf8), handle: "ac", createdAt: 1),
            Account(id: "b", type: .totp, issuer: "ACME", account: "", secret: Data("x".utf8), createdAt: 2),
        ]
        Handles.assign(&accts)
        XCTAssertEqual(accts[1].handle, "ac2")
        // User frees "ac2" by renaming b, then a new ACME account claims it back.
        accts[1].handle = "acme"
        accts.append(Account(id: "c", type: .totp, issuer: "ACME", account: "", secret: Data("x".utf8), createdAt: 3))
        Handles.assign(&accts)
        XCTAssertEqual(accts[0].handle, "ac", "existing handle must not be renumbered")
        XCTAssertEqual(accts[2].handle, "ac2", "freed handle should be reused")
    }
}
