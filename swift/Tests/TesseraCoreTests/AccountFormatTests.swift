import XCTest
import Foundation
@testable import TesseraCore

/// The version 2 payload: strict reading, the new keys, extension fields,
/// HOTP counter range and display order, against /spec vectors.
final class AccountFormatTests: XCTestCase {
    private func account(_ id: String) -> Account {
        Account(id: id, type: .totp, issuer: "Issuer", account: "user", secret: Data("12345678901234567890".utf8))
    }

    private func value(_ json: String) throws -> ExtensionValue {
        try ExtensionValue(json: Data(json.utf8))
    }

    private func message(_ body: () throws -> Void) -> String {
        do {
            try body()
            return "no error"
        } catch {
            return "\(error)"
        }
    }

    // MARK: - Vectors

    func testPayloadStrictVectors() throws {
        let p = try VectorNode.root().get("payload_strict")
        for c in try p.get("valid").items {
            let name = try c.get("name").text
            let payload = try c.get("payload").data
            XCTAssertEqual(CanonicalJSON.encode(try CanonicalJSON.decode(payload)), payload, name)
        }
        let invalid = try p.get("invalid").items
        XCTAssertFalse(invalid.isEmpty)
        for c in invalid {
            let name = try c.get("name").text
            var got = "accepted"
            do { _ = try CanonicalJSON.decode(c.get("payload").data) } catch { got = payloadErrorClass(error) }
            XCTAssertEqual(got, try c.get("error").text, name)
        }
    }

    func testVaultV2CanonicalVectors() throws {
        let cases = try VectorNode.root().get("vault_v2").get("canonical").items
        XCTAssertFalse(cases.isEmpty)
        for c in cases {
            let name = try c.get("name").text
            var accounts = try CanonicalJSON.decode(c.get("input").data)
            for key in c["clear"]?.texts ?? [] {
                for i in accounts.indices {
                    switch key {
                    case "deleted_at": accounts[i].deletedAt = 0
                    case "note": accounts[i].note = ""
                    case "order": accounts[i].order = 0
                    default:
                        XCTAssertTrue(accounts[i].extensions.contains { $0.key == key }, "\(name) \(key)")
                        accounts[i].extensions.removeAll { $0.key == key }
                    }
                }
            }
            XCTAssertEqual(CanonicalJSON.encode(accounts), try c.get("expected_canonical_json").data, name)
            XCTAssertEqual(Account.formatVersion(accounts), Int(try c.get("expected_version").int()), name)
        }
    }

    func testCanonicalEdgeV2RoundTrip() throws {
        let edge = try Data(contentsOf: VectorNode.specFile("canonical_edge_v2.json"))
        let accounts = try CanonicalJSON.decode(edge)
        XCTAssertEqual(CanonicalJSON.encode(accounts), edge)
        XCTAssertEqual(Account.formatVersion(accounts), 2)
    }

    func testHOTPCounterRangeVectors() throws {
        let h = try VectorNode.root().get("hotp_counter_range")
        XCTAssertEqual(try h.get("max_counter").int(), Account.maxCounter)
        for c in try h.get("payload_accept").items {
            let payload = try c.get("payload").data
            let accounts = try CanonicalJSON.decode(payload)
            XCTAssertEqual(accounts.map(\.counter), [try c.get("counter").int()])
            XCTAssertEqual(CanonicalJSON.encode(accounts), payload)
        }
        for c in try h.get("payload_reject").items {
            var got = "accepted"
            do { _ = try CanonicalJSON.decode(c.get("payload").data) } catch { got = payloadErrorClass(error) }
            XCTAssertEqual(got, try c.get("error").text)
        }
        for c in try h.get("advance").items {
            XCTAssertEqual(try Account.nextCounter(after: c.get("from").int()), try c.get("to").int())
        }
        for c in try h.get("advance_refused").items {
            let from = try c.int()
            XCTAssertThrowsError(try Account.nextCounter(after: from)) { error in
                XCTAssertEqual(error as? CounterRangeError, CounterRangeError(counter: from))
            }
        }
        let m = try h.get("code_at_max")
        let secret = try XCTUnwrap(Data(base64Encoded: m.get("secret_b64").text))
        let atMax = try Account(id: "max", type: .hotp, issuer: "", account: "x", secret: secret,
                            algorithm: m.get("algorithm").text, digits: Int(m.get("digits").int()),
                            counter: m.get("counter").int())
        XCTAssertEqual(try OTP.code(for: atMax, at: Date()), try m.get("code").text)
    }

    func testDisplayOrderVectors() throws {
        let cases = try VectorNode.root().get("display_order").get("cases").items
        XCTAssertFalse(cases.isEmpty)
        for c in cases {
            let name = try c.get("name").text
            let accounts = try c.get("accounts").items.map { a in
                Account(id: try a.get("id").text, type: .totp, issuer: a["issuer"]?.text ?? "",
                        account: a["account"]?.text ?? "", secret: Data("x".utf8),
                        deletedAt: try a["deleted_at"]?.int() ?? 0, order: Int(try a["order"]?.int() ?? 0))
            }
            XCTAssertEqual(Account.sortedForDisplay(Account.live(accounts)).map(\.id),
                           try c.get("expected_ids").texts, name)
        }
    }

    // MARK: - HOTP counter

    func testHOTPCodeRefusesCounterOutsideRange() {
        for counter in [Int64(-1), Int64.max] {
            let a = Account(id: "h", type: .hotp, issuer: "", account: "x", secret: Data("12345678901234567890".utf8),
                            counter: counter)
            XCTAssertEqual(message { _ = try OTP.code(for: a, at: Date()) },
                           "account: account \"h\": counter outside 0..9223372036854775806, got \(counter)")
        }
    }

    func testNextCounter() throws {
        XCTAssertEqual(try Account.nextCounter(after: 0), 1)
        XCTAssertEqual(try Account.nextCounter(after: Account.maxCounter - 1), Account.maxCounter)
        XCTAssertEqual(message { _ = try Account.nextCounter(after: Account.maxCounter) },
                       "counter outside 0..9223372036854775806: cannot advance 9223372036854775806")
        XCTAssertEqual(message { _ = try Account.nextCounter(after: -1) },
                       "counter outside 0..9223372036854775806: cannot advance -1")
    }

    // MARK: - Version 2 keys

    func testFormatVersionFollowsVersion2Keys() throws {
        var a = account("1")
        XCTAssertFalse(a.usesFormatVersion2)
        XCTAssertEqual(Account.formatVersion([a]), 1)
        XCTAssertEqual(Account.formatVersion([]), 1)
        a.note = "n"
        XCTAssertEqual(Account.formatVersion([account("0"), a]), 2)
        a.note = ""
        a.order = 1
        XCTAssertTrue(a.usesFormatVersion2)
        a.order = 0
        a.deletedAt = 5
        XCTAssertTrue(a.usesFormatVersion2)
        a.deletedAt = 0
        a.extensions = [try ExtensionField(key: "x_new", value: value("1"))]
        XCTAssertTrue(a.usesFormatVersion2)
    }

    func testLiveAndTrashed() {
        var trashed = account("2")
        trashed.deletedAt = 1_700_000_000
        let accounts = [account("1"), trashed, account("3")]
        XCTAssertTrue(trashed.isTrashed)
        XCTAssertFalse(account("1").isTrashed)
        XCTAssertEqual(Account.live(accounts).map(\.id), ["1", "3"])
        XCTAssertEqual(Account.trashed(accounts).map(\.id), ["2"])
    }

    func testValidateRefusesNegativeVersion2Values() {
        var a = account("x\"y")
        a.deletedAt = -1
        XCTAssertEqual(message { try a.validate() },
                       "account: account \"x\\\"y\": deleted_at must not be negative, got -1")
        a.deletedAt = 0
        a.order = -2
        XCTAssertEqual(message { try a.validate() },
                       "account: account \"x\\\"y\": order must not be negative, got -2")
        a.order = 0
        XCTAssertNoThrow(try a.validate())
    }

    func testDecodeRefusesUnknownOrMissingType() {
        XCTAssertEqual(message { _ = try CanonicalJSON.decode(Data(#"[{"id":"1","type":"push"}]"#.utf8)) },
                       "account: account \"1\": invalid type \"push\"")
        XCTAssertEqual(message { _ = try CanonicalJSON.decode(Data(#"[{"id":"1"}]"#.utf8)) },
                       "account: account \"1\": invalid type \"\"")
    }

    func testHandleUniquenessMessagesQuoteLikeGo() {
        var a = account("1")
        a.handle = "gh"
        var b = account("2")
        b.handle = "gh"
        XCTAssertEqual(message { try Handles.checkUniqueness([a, b]) },
                       "account: duplicate handle \"gh\" on accounts \"1\" and \"2\"")
        b.handle = "Bad Handle"
        XCTAssertEqual(message { try Handles.checkUniqueness([a, b]) },
                       "account: account \"2\": invalid handle \"Bad Handle\"")
    }

    func testPayloadErrorDescriptions() {
        XCTAssertEqual(PayloadError.fieldType(field: "issuer").description,
                       "account: field \"issuer\": account: field has the wrong JSON type")
        XCTAssertEqual(PayloadError.nonCanonicalBase64(field: "secret").description,
                       "account: field \"secret\": account: secret is not canonical standard base64")
        XCTAssertEqual(PayloadError.extensionKey("note").description,
                       "account: extension field uses a key the spec defines: \"note\"")
        XCTAssertEqual(PayloadError.counterRange(-1).description,
                       "account: field \"counter\": counter outside 0..9223372036854775806, got -1")
        XCTAssertEqual(PayloadError.duplicateKey.errorDescription, "account: duplicate key")
        XCTAssertEqual(CounterRangeError(counter: 7).errorDescription,
                       "counter outside 0..9223372036854775806: cannot advance 7")
    }

    // MARK: - Extension fields

    func testExtensionFieldRefusesDefinedKeys() throws {
        for key in ["issuer", "note", "deleted_at", "order", "secret"] {
            XCTAssertThrowsError(try ExtensionField(key: key, value: value("1"))) { error in
                XCTAssertEqual(error as? PayloadError, .extensionKey(key))
            }
        }
        XCTAssertEqual(try ExtensionField(key: "x_new", value: value("1")).key, "x_new")
    }

    func testExtensionValueKeepsCanonicalForm() throws {
        XCTAssertEqual(String(decoding: try value(#"{"b":1, "a":["é\/<&>"]}"#).canonicalJSON, as: UTF8.self),
                       #"{"a":["é/<&>"],"b":1}"#)
        XCTAssertEqual(try value(#"{"b":1,"a":2}"#), try value(#"{"a":2,"b":1}"#))
        XCTAssertEqual(try value("-12").canonicalJSON, Data("-12".utf8))
    }

    func testExtensionValueRefusesInvalidJSON() {
        let cases: [(String, PayloadError)] = [
            ("1.5", .nonCanonicalNumber),
            ("1e3", .nonCanonicalNumber),
            ("-0", .nonCanonicalNumber),
            (#"{"a":1,"a":2}"#, .duplicateKey),
            ("{", .malformed),
            ("1 2", .malformed),
            (String(repeating: "[", count: 70) + String(repeating: "]", count: 70), .malformed),
        ]
        for (json, expected) in cases {
            XCTAssertThrowsError(try value(json), json) { error in
                XCTAssertEqual(error as? PayloadError, expected, json)
            }
        }
    }

    func testExtensionsSetterKeepsLastValueAndSortsByKey() throws {
        var a = account("1")
        a.extensions = [try ExtensionField(key: "z_last", value: value("1")),
                        try ExtensionField(key: "a_first", value: value("2")),
                        try ExtensionField(key: "z_last", value: value("3"))]
        XCTAssertEqual(a.extensions.map(\.key), ["a_first", "z_last"])
        XCTAssertEqual(a.extensions.last?.value, try value("3"))
        let viaInit = Account(id: "1", type: .totp, issuer: "", account: "", secret: Data("x".utf8),
                              extensions: a.extensions.reversed())
        XCTAssertEqual(viaInit.extensions, a.extensions)
    }

    func testExtensionsRoundTripThroughPayload() throws {
        var a = account("1")
        a.extensions = [try ExtensionField(key: "x_obj", value: value(#"{"k":[1,"two",null,true]}"#)),
                        try ExtensionField(key: "b_num", value: value("42"))]
        let payload = CanonicalJSON.encode([a])
        let text = String(decoding: payload, as: UTF8.self)
        XCTAssertTrue(text.contains(#""b_num":42,"counter":0"#))
        XCTAssertTrue(text.hasSuffix(#""updated_at":0,"x_obj":{"k":[1,"two",null,true]}}]"#))
        let decoded = try CanonicalJSON.decode(payload)
        XCTAssertEqual(decoded, [a])
        XCTAssertEqual(CanonicalJSON.encode(decoded), payload)
        XCTAssertEqual(Account.formatVersion(decoded), 2)
    }
}
