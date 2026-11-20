import XCTest
import Foundation
#if canImport(CryptoKit)
import CryptoKit
#elseif canImport(Crypto)
import Crypto
#endif
@testable import TesseraCore

final class MigrationTests: XCTestCase {

    private func vectors() throws -> [String: Any] {
        var dir = URL(fileURLWithPath: #filePath)
        for _ in 0..<8 {
            dir.deleteLastPathComponent()
            let candidate = dir.appendingPathComponent("spec/testvectors.json")
            if FileManager.default.fileExists(atPath: candidate.path) {
                return try JSONSerialization.jsonObject(with: Data(contentsOf: candidate)) as! [String: Any]
            }
        }
        throw XCTSkip("spec/testvectors.json not found")
    }

    private func assertAccounts(_ got: [Account], _ want: [[String: Any]], _ name: String,
                                file: StaticString = #filePath, line: UInt = #line) {
        XCTAssertEqual(got.count, want.count, "\(name): account count", file: file, line: line)
        for (a, w) in zip(got, want) {
            XCTAssertEqual(a.type.rawValue, w["type"] as? String, "\(name): type", file: file, line: line)
            XCTAssertEqual(Array(a.issuer.utf8), Array((w["issuer"] as! String).utf8), "\(name): issuer", file: file, line: line)
            XCTAssertEqual(Array(a.account.utf8), Array((w["account"] as! String).utf8), "\(name): account", file: file, line: line)
            XCTAssertEqual(Base32.encodeNoPad(a.secret), w["secret_b32"] as? String, "\(name): secret", file: file, line: line)
            XCTAssertEqual(a.algorithm, w["algorithm"] as? String, "\(name): algorithm", file: file, line: line)
            XCTAssertEqual(a.digits, (w["digits"] as? NSNumber)?.intValue, "\(name): digits", file: file, line: line)
            if let counter = w["counter"] as? NSNumber {
                XCTAssertEqual(a.counter, counter.int64Value, "\(name): counter", file: file, line: line)
            }
        }
    }

    // MARK: - parsing

    func testMigrationVectors() throws {
        let set = try vectors()["migration"] as! [String: Any]
        let cases = set["cases"] as! [[String: Any]]
        XCTAssertFalse(cases.isEmpty, "migration.cases is empty")
        for c in cases {
            let uri = c["uri"] as! String
            assertAccounts(try Migration.parse(uri), c["accounts"] as! [[String: Any]], uri)
        }
    }

    func testStrictValidVectors() throws {
        let set = try vectors()["migration_strict"] as! [String: Any]
        let cases = set["valid"] as! [[String: Any]]
        XCTAssertFalse(cases.isEmpty, "migration_strict.valid is empty")
        for c in cases {
            let name = c["name"] as! String
            assertAccounts(try Migration.parse(c["uri"] as! String), c["accounts"] as! [[String: Any]], name)
        }
    }

    func testStrictInvalidVectors() throws {
        let set = try vectors()["migration_strict"] as! [String: Any]
        let cases = set["invalid"] as! [[String: Any]]
        XCTAssertFalse(cases.isEmpty, "migration_strict.invalid is empty")
        for c in cases {
            let name = c["name"] as! String
            XCTAssertThrowsError(try Migration.parse(c["uri"] as! String), name) { error in
                guard case AccountError.invalid(let m) = error else {
                    XCTFail("\(name): \(error) is not AccountError.invalid")
                    return
                }
                XCTAssertTrue(m.hasPrefix("migration: "), "\(name): \(m)")
            }
        }
    }

    /// Each row's text is what Go `migration.Parse` returns for the same input;
    /// an empty text means Go accepts it.
    func testErrorTextMatchesGo() {
        let cases: [(String, String)] = [
            ("otpauth-migration://offline?data=Ch8KCkhlbGxvId6tvu8SBWFsaWNlGgRBQ01FIAQoATACEAEYASAAKLlg", "migration: otp parameter 0: algorithm MD5 is not supported by RFC TOTP clients"),
            ("otpauth-migration://offline?data=Ch8KCkhlbGxvId6tvu8SBWFsaWNlGgRBQ01FIAUoATACEAEYASAAKLlg", "migration: otp parameter 0: unknown algorithm enum 5"),
            ("otpauth-migration://offline?data=Ch8KCkhlbGxvId6tvu8SBWFsaWNlGgRBQ01FIAEoAzACEAEYASAAKLlg", "migration: otp parameter 0: unknown digit-count enum 3"),
            ("otpauth-migration://offline?data=Ch8KCkhlbGxvId6tvu8SBWFsaWNlGgRBQ01FIAEoATADEAEYASAAKLlg", "migration: otp parameter 0: unknown otp-type enum 3"),
            ("otpauth-migration://offline?data=CigKCkhlbGxvId6tvu8SA29wcxoEQmFuayABKAEwATj%2F%2F%2F%2F%2F%2F%2F%2F%2F%2F%2F8BEAEYASAAKLlg", "migration: counter 18446744073709551615 exceeds 9223372036854775806"),
            ("otpauth-migration://offline?data=CicKCkhlbGxvId6tvu8SA29wcxoEQmFuayABKAEwATj%2F%2F%2F%2F%2F%2F%2F%2F%2F%2F38QARgBIAAouWA%3D", "migration: counter 9223372036854775807 exceeds 9223372036854775806"),
            ("otpauth-migration://offline?data=ChYKCkhlbGxvId6tvu8SBWFsaWNlIgEBEAEYASAAKLlg", "migration: algorithm has wire type 2, want varint"),
            ("otpauth-migration://offline?data=CAEQARgBIAAouWA%3D", "migration: otp_parameters has wire type 0, want bytes"),
            ("otpauth-migration://offline?data=Ch8KCkhlbGxvId6tvu8SBWFsaWNlIICAgICAgICAgIAAEAEYASAAKLlg", "migration: malformed algorithm: proto: variable length integer overflow"),
            ("otpauth-migration://offline?data=Ch4KCkhlbGxvId6tvu8SBWFsaWNlIP%2F%2F%2F%2F%2F%2F%2F%2F%2F%2F%2FwIQARgBIAAouWA%3D", "migration: malformed algorithm: proto: variable length integer overflow"),
            ("otpauth-migration://offline?data=CiQKCkhlbGxvId6tvu8SBWFsaWNlGgRBQ01FIAEoATAC", "migration: malformed otp_parameters: unexpected EOF"),
            ("otpauth-migration://offline?data=CoCAgICAgICAgAEKCkhlbGxvId6tvu8SBWFsaWNlGgRBQ01FIAEoATAC", "migration: malformed otp_parameters: unexpected EOF"),
            ("otpauth-migration://offline", "migration: missing data query parameter"),
            ("otpauth-migration://offline?data=", "migration: empty data query parameter"),
            ("otpauth-migration://offline?data=%%%", "migration: missing data query parameter"),
            ("otpauth-migration://online?data=AA", "migration: unexpected host \"online\" (want \"offline\")"),
            ("otpauth://offline?data=AA", "migration: not an otpauth-migration uri (scheme \"otpauth\")"),
            ("otpauth-migration://offline?data=AA", "migration: base64-decode data: illegal base64 data at input byte 0"),
            ("otpauth-migration://offline?data=Cg==", "migration: malformed otp_parameters: unexpected EOF"),
            ("otpauth-migration://offline?data=EAE=", "migration: payload contained no otp parameters"),
        ]
        for (uri, want) in cases {
            do {
                _ = try Migration.parse(uri)
                XCTAssertEqual(want, "", "\(uri): accepted")
            } catch AccountError.invalid(let m) {
                XCTAssertEqual(m, want, uri)
            } catch {
                XCTFail("\(uri): \(error) is not AccountError.invalid")
            }
        }
    }

    // MARK: - building

    private func sha256Hex(_ d: Data) -> String {
        SHA256.hash(data: d).map { String(format: "%02x", $0) }.joined()
    }

    private func secret(_ b32: String) -> Data {
        try! Base32.decode(b32)
    }

    /// The fixture of Go `migrationFixture`: 60 eligible accounts mixing
    /// algorithms, digit counts and HOTP, then three the format cannot carry.
    private func fixture() -> (eligible: [Account], ineligible: [Account]) {
        let algorithms = ["SHA1", "SHA256", "SHA512"]
        var eligible: [Account] = []
        for i in 0..<60 {
            let n = String(format: "%02d", i)
            let isHOTP = i % 7 == 0
            eligible.append(Account(id: "id\(n)", type: isHOTP ? .hotp : .totp, issuer: "Issuer \(n)",
                                    account: "user\(n)@example.com", secret: Data("secret-bytes-\(n)".utf8),
                                    algorithm: algorithms[i % 3], digits: i % 5 == 0 ? 8 : 6,
                                    period: isHOTP ? 0 : 30, counter: isHOTP ? Int64(i) * 3 : 0))
        }
        let ineligible = [
            Account(id: "s1", type: .steam, issuer: "Steam", account: "gaben",
                    secret: secret("NBSWY3DPEB3W64TM"), digits: 5),
            Account(id: "s2", type: .totp, issuer: "Seven", account: "digits",
                    secret: secret("JBSWY3DPEHPK3PXP"), digits: 7),
            Account(id: "s3", type: .totp, issuer: "Slow", account: "period",
                    secret: secret("GEZDGNBVGY3TQOJQ"), period: 60),
        ]
        return (eligible, ineligible)
    }

    private func assertBatches(_ e: Migration.Export, sizes: [Int], lengths: [Int], sums: [String],
                               file: StaticString = #filePath, line: UInt = #line) throws {
        XCTAssertEqual(e.payloads.map(\.count), lengths, "payload lengths", file: file, line: line)
        XCTAssertEqual(e.payloads.map(sha256Hex), sums, "payload sha256", file: file, line: line)
        XCTAssertEqual(e.uris.count, e.payloads.count, file: file, line: line)
        var batchSizes: [Int] = []
        for uri in e.uris {
            batchSizes.append(try Migration.parse(uri).count)
        }
        XCTAssertEqual(batchSizes, sizes, "accounts per batch", file: file, line: line)
    }

    func testExportSingleAccountMatchesGoBytes() {
        let a = Account(id: "x", type: .totp, issuer: "", account: "host:port", secret: secret("JBSWY3DPEHPK3PXP"))
        let e = Migration.export([a], batchID: 1)
        XCTAssertEqual(e.payloads.map { $0.map { String(format: "%02x", $0) }.joined() },
                       ["0a1d0a0a48656c6c6f21deadbeef1209686f73743a706f72742001280130021001180120002801"])
        XCTAssertEqual(e.uris, ["otpauth-migration://offline?data=Ch0KCkhlbGxvId6tvu8SCWhvc3Q6cG9ydCABKAEwAhABGAEgACgB"])
        XCTAssertEqual(e.skipped, [])
    }

    func testExportFixtureMatchesGoBytes() throws {
        let (eligible, ineligible) = fixture()
        let e = Migration.export(eligible + ineligible, batchID: -123456789)
        try assertBatches(e, sizes: [10, 10, 10, 10, 10, 10], lengths: [681, 679, 681, 679, 682, 680], sums: [
            "d17aa8e54a3f6944e03df01d9da6f105d4c9259561dea2df69baaf70bb219355",
            "6fba32276d79db75a625712d97bf2f230413e9160bfe94e1719fad18343279dc",
            "7e0952de806c5b686afe322caa80d17b0cc230b24705df6db961cd7f58deab9b",
            "ac749dfc6192dcfe32b72910a4977593760e511ee63a2c8d784f90e095ae3b5a",
            "96d614bcd8d6b25b94480d2e78dcc8e70a07372b1501fc62670bacf2e4e934e8",
            "3f2bfa39fa66ef7b5edc54e14f2811cda324bbdc34292063a3ec0a3cb72feb07",
        ])
        XCTAssertEqual(e.skipped, [
            Migration.Skipped(label: "Steam (gaben)", reason: "Google Authenticator has no Steam entry type"),
            Migration.Skipped(label: "Seven (digits)",
                              reason: "Google Authenticator stores only 6- and 8-digit codes, this one has 7"),
            Migration.Skipped(label: "Slow (period)",
                              reason: "Google Authenticator stores only 30-second codes, this one uses 60s"),
        ])
    }

    func testExportMaxBatchIDMatchesGoBytes() throws {
        let e = Migration.export(fixture().eligible, batchID: Int32.max)
        try assertBatches(e, sizes: [10, 10, 10, 10, 10, 10], lengths: [676, 674, 676, 674, 677, 675], sums: [
            "6a2a9bcf6a092c7afc15c8a235304ceb9a436a3db88fb44237babbcb77096dff",
            "71308284a8305e23850855857b93e2283900f73877c3a75a90b423d3d696fbfa",
            "58796bf5fc099fbb4a6e3bcfd765c681caf604ee2db10bde0a8ff2a785e66d3c",
            "78b0b9815be1dd6bb565b87f7aacd3575c5810f9139235d1bb44595379d61828",
            "522bfe66cce7762908d0cc4e666367fd76bb950aec5a58dc95daa33d840366b7",
            "25d0bdacb2ba2e5b38da8ea887dc505a8238c39135fd9e5516123f2781e35bc6",
        ])
        XCTAssertEqual(e.skipped, [])
    }

    func testExportSplitsOnPayloadBytes() throws {
        var accounts: [Account] = []
        for i in 0..<12 {
            let secret = Data((0..<20).map { UInt8(i * 20 + $0) })
            accounts.append(Account(id: "", type: .totp, issuer: String(repeating: "x", count: 30 * i),
                                    account: "u\(i)", secret: secret))
        }
        let e = Migration.export(accounts, batchID: 42)
        try assertBatches(e, sizes: [5, 2, 1, 1, 1, 1, 1], lengths: [793, 748, 468, 528, 588, 649, 709], sums: [
            "31fa90710c498d611d73fb0697481e329bdf8e1effceee9d0dc08dfdc5bbcc50",
            "fbe116fcaa228115345d9d185829d5776514375fda47c282d960a3f71d8e95dc",
            "61baa361ed4062e3aef64bcc99525180b3fc94aa672ce20fcb25bf1522d48425",
            "675f86a33797f8285380d7f5fd5d3889b11c6561bc150e0536c2c50b521c29f3",
            "add14709962a8416b04f5206ea0c2aa07a99c95c300fd8c1841c525fa4e22ed9",
            "16100855d37bf7f2c8d23d0ce7117c0e2321f6bcf78cc041ca2e6b9b82258a0f",
            "a8df992c8d4dde428b07e53d8859452c73d8fb995458a7741fcb348f12376c25",
        ])
        XCTAssertTrue(e.payloads.allSatisfy { $0.count <= Migration.maxPayloadBytes })
    }

    func testExportReadsBackThroughParse() throws {
        let eligible = fixture().eligible
        let e = Migration.export(eligible + fixture().ineligible, batchID: 7)
        var back: [Account] = []
        for uri in e.uris {
            back += try Migration.parse(uri)
        }
        XCTAssertEqual(back.count, eligible.count)
        for (got, want) in zip(back, eligible) {
            let ctx = want.id
            XCTAssertEqual(got.type, want.type, ctx)
            XCTAssertEqual(got.issuer, want.issuer, ctx)
            XCTAssertEqual(got.account, want.account, ctx)
            XCTAssertEqual(got.secret, want.secret, ctx)
            XCTAssertEqual(got.algorithm, want.algorithm, ctx)
            XCTAssertEqual(got.digits, want.digits, ctx)
            XCTAssertEqual(got.counter, want.counter, ctx)
            if want.type != .hotp { XCTAssertEqual(got.period, want.period, ctx) }
        }
    }

    func testExportWithNothingEligibleHasNoPayloads() {
        let e = Migration.export([fixture().ineligible[0]], batchID: 1)
        XCTAssertEqual(e.payloads, [])
        XCTAssertEqual(e.uris, [])
        XCTAssertEqual(e.skipped.map(\.label), ["Steam (gaben)"])
    }

    func testExportSkipsMD5() {
        let a = Account(id: "", type: .totp, issuer: "Old", account: "", secret: secret("JBSWY3DPEHPK3PXP"),
                        algorithm: "MD5")
        let e = Migration.export([a], batchID: 1)
        XCTAssertEqual(e.payloads, [])
        XCTAssertEqual(e.skipped, [Migration.Skipped(label: "Old",
                                                     reason: "Google Authenticator does not store the MD5 algorithm")])
    }
}
