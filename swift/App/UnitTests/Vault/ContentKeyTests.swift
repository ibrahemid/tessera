import Foundation
import XCTest
import TesseraCore
@testable import Tessera

final class ContentKeyTests: XCTestCase {
    private struct Fields: Hashable {
        let type: String
        let issuer: String
        let account: String
        let secret: Data
    }

    private func mergeCases() throws -> [(name: String, accounts: Set<Fields>)] {
        let url = try XCTUnwrap(Bundle(for: Self.self).url(forResource: "testvectors", withExtension: "json"))
        let root = try XCTUnwrap(try JSONSerialization.jsonObject(with: Data(contentsOf: url)) as? [String: Any])
        let merge = try XCTUnwrap(root["merge"] as? [String: Any])
        let cases = try XCTUnwrap(merge["cases"] as? [[String: Any]])
        return try cases.map { vector in
            var accounts = Set<Fields>()
            for list in ["local", "source", "local_after", "source_after"] {
                let raw = try XCTUnwrap(vector[list] as? String)
                let rows = try XCTUnwrap(try JSONSerialization.jsonObject(with: Data(raw.utf8)) as? [[String: Any]])
                for row in rows {
                    let secret = try XCTUnwrap(row["secret"] as? String)
                    accounts.insert(Fields(type: try XCTUnwrap(row["type"] as? String),
                                           issuer: try XCTUnwrap(row["issuer"] as? String),
                                           account: try XCTUnwrap(row["account"] as? String),
                                           secret: try XCTUnwrap(Data(base64Encoded: secret))))
                }
            }
            return (try XCTUnwrap(vector["name"] as? String), accounts)
        }
    }

    private func account(_ fields: Fields) throws -> Account {
        var a = VaultFixture.account("x", issuer: fields.issuer, account: fields.account,
                                     type: try XCTUnwrap(OTPType(rawValue: fields.type)))
        a.secret = fields.secret
        return a
    }

    func testEveryMergeVectorAccountUsesTheCoreContentKey() throws {
        let cases = try mergeCases()
        XCTAssertFalse(cases.isEmpty)
        for vector in cases {
            for fields in vector.accounts {
                let a = try account(fields)
                XCTAssertEqual(VaultChange.dedupeKey(a), Merge.ContentKey(a), vector.name)
            }
        }
    }

    func testCaseVariantsInTheMergeVectorsShareOneKey() throws {
        let duplicate = try XCTUnwrap(try mergeCases().first { $0.name == "content-duplicate-not-added" })
        let keys = Set(try duplicate.accounts.map { VaultChange.dedupeKey(try account($0)) })

        XCTAssertEqual(duplicate.accounts.count, 2)
        XCTAssertEqual(keys.count, 1)
    }

    func testDottedCapitalIMatchesPlainIAsInTheCLI() {
        let dotted = VaultFixture.account("1", issuer: "\u{130}\u{15F} Bankas\u{131}", account: "me")
        let plain = VaultFixture.account("2", issuer: "i\u{15F} bankas\u{131}", account: "ME")

        XCTAssertEqual(VaultChange.dedupeKey(dotted), VaultChange.dedupeKey(plain))
        XCTAssertEqual(VaultChange.droppingDuplicates([dotted, plain]).map(\.id), ["1"])
    }
}
