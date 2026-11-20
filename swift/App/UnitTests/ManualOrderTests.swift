import Foundation
import XCTest
import TesseraCore
@testable import Tessera

final class ManualOrderTests: XCTestCase {
    private let trashedAt: Int64 = 1_700_000_000

    private func vault(pinned: Set<String> = [], orders: [String: Int] = [:]) -> [Account] {
        var accounts = [VaultFixture.account("a", issuer: "Alpha", handle: "al", updatedAt: 1),
                        VaultFixture.account("b", issuer: "Bravo", handle: "br", updatedAt: 1),
                        VaultFixture.account("c", issuer: "Charlie", handle: "ch", updatedAt: 1),
                        VaultFixture.account("d", issuer: "Delta", handle: "de", updatedAt: 1),
                        VaultFixture.account("e", issuer: "Echo", handle: "ec", updatedAt: trashedAt)]
        accounts[4].deletedAt = trashedAt
        for i in accounts.indices {
            accounts[i].pinned = pinned.contains(accounts[i].id)
            accounts[i].order = orders[accounts[i].id] ?? 0
        }
        return accounts
    }

    private func shown(_ accounts: [Account]) -> [String] {
        VaultFixture.ids(ManualOrder.displayOrder(Account.live(accounts)))
    }

    private func orders(_ accounts: [Account]) -> [String: Int] {
        Dictionary(uniqueKeysWithValues: accounts.map { ($0.id, $0.order) })
    }

    private func stamps(_ accounts: [Account]) -> [String: Int64] {
        Dictionary(uniqueKeysWithValues: accounts.map { ($0.id, $0.updatedAt) })
    }

    private func sections(_ accounts: [Account]) -> [[String]] {
        VaultList.sections(ManualOrder.displayOrder(Account.live(accounts)), scope: .all, query: "")
            .map { VaultFixture.ids($0.accounts) }
    }

    private func drag(_ id: String, to toOffset: Int, in accounts: inout [Account]) throws {
        let section = try XCTUnwrap(sections(accounts).first { $0.contains(id) })
        ManualOrder.move([id], toOffset: toOffset, section: section, in: &accounts)
    }

    private func moveToPosition(_ id: String, _ pos: Int, in accounts: inout [Account]) {
        var moved = shown(accounts).filter { $0 != id }
        moved.insert(id, at: pos - 1)
        for (n, mid) in moved.enumerated() {
            if let i = accounts.firstIndex(where: { $0.id == mid }) { accounts[i].order = n + 1 }
        }
    }

    private func setPinned(_ pinned: Bool, _ id: String, in accounts: inout [Account]) {
        if let i = accounts.firstIndex(where: { $0.id == id }) { accounts[i].pinned = pinned }
    }

    func testDisplayOrderVectors() throws {
        let url = try XCTUnwrap(Bundle(for: Self.self).url(forResource: "testvectors", withExtension: "json"))
        let root = try XCTUnwrap(try JSONSerialization.jsonObject(with: Data(contentsOf: url)) as? [String: Any])
        let block = try XCTUnwrap(root["display_order"] as? [String: Any])
        let cases = try XCTUnwrap(block["cases"] as? [[String: Any]])
        XCTAssertFalse(cases.isEmpty)
        let known: Set<String> = ["id", "issuer", "account", "order", "deleted_at"]
        for vector in cases {
            let name = try XCTUnwrap(vector["name"] as? String)
            let rows = try XCTUnwrap(vector["accounts"] as? [[String: Any]], name)
            let accounts = try rows.map { row -> Account in
                XCTAssertTrue(Set(row.keys).isSubset(of: known), name)
                var a = VaultFixture.account(try XCTUnwrap(row["id"] as? String, name),
                                             issuer: row["issuer"] as? String ?? "",
                                             account: row["account"] as? String ?? "")
                a.order = (row["order"] as? NSNumber)?.intValue ?? 0
                a.deletedAt = (row["deleted_at"] as? NSNumber)?.int64Value ?? 0
                return a
            }
            let expected = try XCTUnwrap(vector["expected_ids"] as? [String], name)
            XCTAssertEqual(shown(accounts), expected, name)
        }
    }

    func testCoreOrderMatchesTheCLI() {
        let rows: [(String, String, String, Int)] = [
            ("9", "service10", "", 0), ("8", "Service2", "", 0), ("7", "acme", "b", 0),
            ("6", "ACME", "a", 0), ("5", "x", "", 2), ("4", "y", "", 1),
            ("3", "acme", "B", 0), ("2", "z", "", 2), ("1", "Service02", "", 0)]
        let accounts = rows.map { row -> Account in
            var a = VaultFixture.account(row.0, issuer: row.1, account: row.2)
            a.order = row.3
            return a
        }
        XCTAssertEqual(shown(accounts), ["4", "2", "5", "6", "3", "7", "1", "8", "9"])
    }

    func testPinnedAccountsComeFirst() {
        var accounts = vault(pinned: ["d", "c"])
        XCTAssertEqual(shown(accounts), ["c", "d", "a", "b"])
        setPinned(false, "d", in: &accounts)
        XCTAssertEqual(shown(accounts), ["c", "a", "b", "d"])
    }

    func testPinnedStaysFirstOverAManualOrder() {
        var accounts = vault(pinned: ["b"], orders: ["a": 3, "b": 4, "c": 1, "d": 2])
        XCTAssertEqual(shown(accounts), ["b", "c", "d", "a"])
        setPinned(false, "b", in: &accounts)
        XCTAssertEqual(shown(accounts), ["c", "d", "a", "b"])
    }

    func testListingFollowsAMoveThenAPin() throws {
        var accounts = vault()
        try drag("c", to: 0, in: &accounts)
        setPinned(true, "b", in: &accounts)
        XCTAssertEqual(shown(accounts), ["b", "c", "a", "d"])
    }

    func testDragNumbersEveryLiveAccountAndLeavesTheTrashAlone() throws {
        var accounts = vault()
        accounts[4].order = 9
        try drag("d", to: 0, in: &accounts)
        XCTAssertEqual(orders(accounts), ["d": 1, "a": 2, "b": 3, "c": 4, "e": 9])
        XCTAssertEqual(stamps(accounts), stamps(vault()))
        try drag("a", to: 4, in: &accounts)
        XCTAssertEqual(shown(accounts), ["d", "b", "c", "a"])
        XCTAssertEqual(accounts[4].order, 9)
    }

    func testDragKeepsPinnedAccountsFirst() throws {
        var accounts = vault(pinned: ["c"])
        XCTAssertEqual(sections(accounts), [["c"], ["a", "b", "d"]])
        try drag("d", to: 0, in: &accounts)
        XCTAssertEqual(shown(accounts), ["c", "d", "a", "b"])
    }

    func testEveryDragMatchesMovingToThatPosition() {
        let starts = [vault(), vault(pinned: ["c"]), vault(pinned: ["b", "d"]),
                      vault(pinned: ["a", "b", "c", "d"]),
                      vault(pinned: ["d"], orders: ["a": 2, "b": 0, "c": 1, "d": 3])]
        for start in starts {
            let global = shown(start)
            for section in sections(start) {
                let base = global.firstIndex(of: section[0]) ?? 0
                for (from, id) in section.enumerated() {
                    for to in 0...section.count {
                        var dragged = start
                        ManualOrder.move([id], toOffset: to, section: section, in: &dragged)
                        var expected = start
                        moveToPosition(id, base + (to > from ? to - 1 : to) + 1, in: &expected)
                        let label = "\(global) \(id) \(from)->\(to)"
                        XCTAssertEqual(orders(dragged), orders(expected), label)
                        XCTAssertEqual(stamps(dragged), stamps(start), label)
                    }
                }
            }
        }
    }

    func testDraggingSeveralRowsKeepsTheirOrder() {
        var accounts = vault()
        ManualOrder.move(["c", "a"], toOffset: 4, section: ["a", "b", "c", "d"], in: &accounts)
        XCTAssertEqual(shown(accounts), ["b", "d", "a", "c"])
        XCTAssertEqual(orders(accounts), ["b": 1, "d": 2, "a": 3, "c": 4, "e": 0])
    }

    func testAnInvalidDragChangesNothing() {
        let start = vault(pinned: ["c"])
        let cases: [([String], Int, [String])] = [
            (["c"], 0, ["a", "b", "d"]),
            (["a"], -1, ["a", "b", "d"]),
            (["a"], 4, ["a", "b", "d"]),
            ([], 0, ["a", "b", "d"]),
            (["a", "a"], 3, ["a", "b", "d"]),
            (["a", "c"], 3, ["a", "b", "d"]),
            (["a"], 0, ["a", "b", "d", "e"]),
            (["a"], 0, ["a", "b"]),
            (["a"], 0, ["a", "b", "c", "d"]),
            (["a"], 0, ["a", "a", "b", "d"])]
        for (ids, to, section) in cases {
            var accounts = start
            ManualOrder.move(ids, toOffset: to, section: section, in: &accounts)
            XCTAssertEqual(accounts, start, "\(ids) \(to) \(section)")
        }
    }

    func testADragReplayedOnAReorderedVaultKeepsTheDraggedArrangement() {
        var accounts = vault(orders: ["c": 1])
        XCTAssertEqual(shown(accounts), ["c", "a", "b", "d"])
        ManualOrder.move(["d"], toOffset: 0, section: ["a", "b", "c", "d"], in: &accounts)
        XCTAssertEqual(shown(accounts), ["d", "a", "b", "c"])
    }

    func testMoveDownMovesASelectionAsABlock() {
        var accounts = vault()
        ManualOrder.move(["a", "c"], by: 1, in: &accounts)
        XCTAssertEqual(shown(accounts), ["b", "d", "a", "c"])
        XCTAssertEqual(orders(accounts), ["b": 1, "d": 2, "a": 3, "c": 4, "e": 0])
        XCTAssertEqual(stamps(accounts), stamps(vault()))
    }

    func testMoveUpMovesASelectionAsABlock() {
        var accounts = vault()
        ManualOrder.move(["b", "d"], by: -1, in: &accounts)
        XCTAssertEqual(shown(accounts), ["b", "d", "a", "c"])
        ManualOrder.move(["c"], by: -2, in: &accounts)
        XCTAssertEqual(shown(accounts), ["b", "c", "d", "a"])
    }

    func testMoveAtAnEdgeDoesNothing() {
        let start = vault()
        let cases: [(Set<String>, Int)] = [(["a"], -1), (["a", "c"], -1), (["d"], 1), (["b", "d"], 1),
                                           (["b"], -2), (["b"], 0), (["e"], 1), (["zz"], -1), ([], 1)]
        for (ids, delta) in cases {
            var accounts = start
            ManualOrder.move(ids, by: delta, in: &accounts)
            XCTAssertEqual(accounts, start, "\(ids.sorted()) \(delta)")
        }
    }

    func testKeyboardMovesStayInsideThePinnedGroup() {
        let start = vault(pinned: ["c", "d"])
        var accounts = start
        ManualOrder.move(["a"], by: -1, in: &accounts)
        XCTAssertEqual(accounts, start)
        ManualOrder.move(["d"], by: 1, in: &accounts)
        XCTAssertEqual(accounts, start)
        ManualOrder.move(["d"], by: -1, in: &accounts)
        XCTAssertEqual(shown(accounts), ["d", "c", "a", "b"])
        XCTAssertEqual(orders(accounts), ["d": 1, "c": 2, "a": 3, "b": 4, "e": 0])
        ManualOrder.move(["c", "b"], by: -1, in: &accounts)
        XCTAssertEqual(shown(accounts), ["c", "d", "b", "a"])
    }

    func testSortAutomaticallyClearsLiveOrderAndUndoRestoresIt() throws {
        var accounts = vault()
        accounts[4].order = 9
        try drag("d", to: 0, in: &accounts)
        let ordered = accounts
        let previous = ManualOrder.clear(in: &accounts)
        XCTAssertEqual(previous, ["d": 1, "a": 2, "b": 3, "c": 4])
        XCTAssertEqual(orders(accounts), ["a": 0, "b": 0, "c": 0, "d": 0, "e": 9])
        XCTAssertEqual(stamps(accounts), stamps(ordered))
        XCTAssertEqual(shown(accounts), ["a", "b", "c", "d"])
        ManualOrder.reapply(previous, in: &accounts)
        XCTAssertEqual(accounts, ordered)
        XCTAssertEqual(shown(accounts), ["d", "a", "b", "c"])
    }

    func testClearingAnUnorderedVaultReturnsNothing() {
        var accounts = vault()
        XCTAssertEqual(ManualOrder.clear(in: &accounts), [:])
        XCTAssertEqual(accounts, vault())
    }

    func testUndoKeepsAPositionSetSinceTheClear() {
        var accounts = vault(orders: ["a": 4, "b": 3, "c": 2, "d": 1])
        let previous = ManualOrder.clear(in: &accounts)
        accounts[0].order = 7
        accounts[3].deletedAt = trashedAt
        ManualOrder.reapply(previous, in: &accounts)
        XCTAssertEqual(orders(accounts), ["a": 7, "b": 3, "c": 2, "d": 0, "e": 0])
    }

    func testOrderSurvivesAnEncodeAndDecode() throws {
        var accounts = vault(pinned: ["b"])
        try drag("d", to: 0, in: &accounts)
        let decoded = try CanonicalJSON.decode(CanonicalJSON.encode(accounts))
        XCTAssertEqual(orders(decoded), orders(accounts))
        XCTAssertEqual(shown(decoded), shown(accounts))
        XCTAssertEqual(shown(decoded), ["b", "d", "a", "c"])
    }
}
