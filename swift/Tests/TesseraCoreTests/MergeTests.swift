import XCTest
import Foundation
@testable import TesseraCore

final class MergeTests: XCTestCase {

    private func account(_ id: String, issuer: String = "Mail", account: String = "me",
                         secret: String = "12345678901234567890", handle: String = "",
                         updatedAt: Int64 = 100, deletedAt: Int64 = 0) -> Account {
        Account(id: id, type: .totp, issuer: issuer, account: account, secret: Data(secret.utf8),
                handle: handle, updatedAt: updatedAt, deletedAt: deletedAt)
    }

    private func counts(_ node: VectorNode) throws -> Merge.Counts {
        Merge.Counts(added: Int(try node.get("added").int()), updated: Int(try node.get("updated").int()),
                     trashed: Int(try node.get("trashed").int()), unchanged: Int(try node.get("unchanged").int()),
                     duplicate: Int(try node.get("duplicate").int()), invalid: Int(try node.get("invalid").int()))
    }

    private func written(_ accounts: [Account], _ counts: Merge.Counts, input: Data) -> Data {
        guard counts.changed else { return input }
        var merged = accounts
        Merge.dedupeHandles(&merged)
        Handles.assign(&merged)
        return CanonicalJSON.encode(merged)
    }

    /// Each `merge` case merged both ways gives the Go sides, written flags
    /// and counts.
    func testMergeVectorsMatchGo() throws {
        let cases = try VectorNode.root().get("merge").get("cases").items
        XCTAssertFalse(cases.isEmpty)
        for c in cases {
            let name = try c.get("name").text
            let localIn = try c.get("local").data
            let sourceIn = try c.get("source").data
            var local = try CanonicalJSON.decode(localIn)
            let source = try CanonicalJSON.decode(sourceIn)
            XCTAssertFalse(Handles.assign(&local), name)
            let r = Merge.twoWay(local: local, source: source)
            XCTAssertEqual(r.localCounts, try counts(c.get("local_counts")), name)
            XCTAssertEqual(r.sourceCounts, try counts(c.get("source_counts")), name)
            XCTAssertEqual(r.localCounts.changed, try c.get("local_written").raw == Array("true".utf8), name)
            XCTAssertEqual(r.sourceCounts.changed, try c.get("source_written").raw == Array("true".utf8), name)
            XCTAssertEqual(written(r.local, r.localCounts, input: localIn), try c.get("local_after").data, name)
            XCTAssertEqual(written(r.source, r.sourceCounts, input: sourceIn), try c.get("source_after").data, name)
        }
    }

    func testNewerAccountWinsAndKeepsTheCurrentHandle() {
        let cur = account("a", handle: "mail", updatedAt: 100)
        var newer = account("a", issuer: "Post", handle: "post", updatedAt: 200)
        let r = Merge.into([cur], [newer])
        XCTAssertEqual(r.counts, Merge.Counts(updated: 1))
        newer.handle = "mail"
        XCTAssertEqual(r.accounts, [newer])
    }

    func testOlderOrEqualAccountLeavesTheDestination() {
        let cur = account("a", updatedAt: 200)
        let r = Merge.into([cur], [account("a", issuer: "Post", updatedAt: 200), account("a", updatedAt: 100)])
        XCTAssertEqual(r.counts, Merge.Counts(unchanged: 2))
        XCTAssertEqual(r.accounts, [cur])
        XCTAssertFalse(r.counts.changed)
    }

    func testTrashingALiveAccountCountsAsTrashed() {
        let r = Merge.into([account("a", updatedAt: 100)], [account("a", updatedAt: 200, deletedAt: 200)])
        XCTAssertEqual(r.counts, Merge.Counts(trashed: 1))
        XCTAssertTrue(r.counts.changed)
    }

    func testSameContentUnderANewIDIsADuplicate() {
        let r = Merge.into([account("a", issuer: "MAIL")], [account("b", issuer: "mail")])
        XCTAssertEqual(r.counts, Merge.Counts(duplicate: 1))
        XCTAssertEqual(r.accounts.map(\.id), ["a"])
    }

    func testTrashedDestinationContentDoesNotBlockAnAdd() {
        let r = Merge.into([account("a", deletedAt: 50)], [account("b")])
        XCTAssertEqual(r.counts, Merge.Counts(added: 1))
        XCTAssertEqual(r.accounts.map(\.id), ["a", "b"])
    }

    func testInvalidAccountIsRefused() {
        var bad = account("b", issuer: "Other")
        bad.digits = 9
        var badUpdate = account("a", updatedAt: 200)
        badUpdate.digits = 9
        let r = Merge.into([account("a")], [bad, badUpdate])
        XCTAssertEqual(r.counts, Merge.Counts(invalid: 2))
        XCTAssertEqual(r.accounts, [account("a")])
    }

    func testAddedAccountWithAHeldHandleLosesIt() {
        let r = Merge.into([account("a", handle: "mail")],
                           [account("b", issuer: "Other", handle: "mail")])
        XCTAssertEqual(r.counts, Merge.Counts(added: 1))
        XCTAssertEqual(r.accounts.map(\.handle), ["mail", ""])
    }

    func testTwoWayMergesTheOriginalLocalIntoTheSource() {
        let local = [account("a", updatedAt: 200)]
        let source = [account("a", issuer: "Old", updatedAt: 100), account("b", issuer: "New")]
        let r = Merge.twoWay(local: local, source: source)
        XCTAssertEqual(r.localCounts, Merge.Counts(added: 1, unchanged: 1))
        XCTAssertEqual(r.sourceCounts, Merge.Counts(updated: 1))
        XCTAssertEqual(r.local.map(\.id), ["a", "b"])
        XCTAssertEqual(r.source.map(\.issuer), ["Mail", "New"])
    }

    func testDedupeHandlesKeepsTheFirstHolder() {
        var accounts = [account("a", handle: "x"), account("b", handle: ""), account("c", handle: "x"),
                        account("d", handle: "y")]
        Merge.dedupeHandles(&accounts)
        XCTAssertEqual(accounts.map(\.handle), ["x", "", "", "y"])
    }

    func testContentKeyComparesTypeLowercasedNamesAndSecret() {
        XCTAssertEqual(Merge.ContentKey(account("a", issuer: "Mail")), Merge.ContentKey(account("b", issuer: "MAIL")))
        XCTAssertNotEqual(Merge.ContentKey(account("a")), Merge.ContentKey(account("a", secret: "other secret bytes")))
        var hotp = account("a")
        hotp.type = .hotp
        XCTAssertNotEqual(Merge.ContentKey(account("a")), Merge.ContentKey(hotp))
    }

    func testContentKeyKeepsFieldBoundaries() {
        XCTAssertNotEqual(Merge.ContentKey(account("a", issuer: "a|b", account: "c")),
                          Merge.ContentKey(account("b", issuer: "a", account: "b|c")))
        XCTAssertNotEqual(Merge.ContentKey(account("a", issuer: "ab", account: "")),
                          Merge.ContentKey(account("b", issuer: "a", account: "b")))
    }

    private func hotp(_ id: String, counter: Int64, updatedAt: Int64, secret: String = "12345678901234567890") -> Account {
        var a = account(id, secret: secret, updatedAt: updatedAt)
        a.type = .hotp
        a.counter = counter
        return a
    }

    func testOlderHOTPCopyWithALargerCounterRaisesTheDestinationCounter() {
        let cur = hotp("a", counter: 3, updatedAt: 200)
        let r = Merge.into([cur], [hotp("a", counter: 9, updatedAt: 100)])
        XCTAssertEqual(r.counts, Merge.Counts(updated: 1))
        var want = cur
        want.counter = 9
        XCTAssertEqual(r.accounts, [want])
    }

    func testNewerHOTPCopyKeepsTheLargerDestinationCounter() {
        let newer = hotp("a", counter: 2, updatedAt: 300)
        let r = Merge.into([hotp("a", counter: 7, updatedAt: 100)], [newer])
        XCTAssertEqual(r.counts, Merge.Counts(updated: 1))
        XCTAssertEqual(r.accounts.map(\.counter), [7])
        XCTAssertEqual(r.accounts.map(\.updatedAt), [300])
    }

    func testHOTPCounterRuleNeedsEqualSecrets() {
        let cur = hotp("a", counter: 3, updatedAt: 200)
        let older = Merge.into([cur], [hotp("a", counter: 9, updatedAt: 100, secret: "another secret value")])
        XCTAssertEqual(older.counts, Merge.Counts(unchanged: 1))
        XCTAssertEqual(older.accounts, [cur])
        let newer = Merge.into([hotp("a", counter: 9, updatedAt: 100)],
                               [hotp("a", counter: 3, updatedAt: 200, secret: "another secret value")])
        XCTAssertEqual(newer.accounts.map(\.counter), [3])
    }

    func testChangedCountsOnlyWrites() {
        XCTAssertFalse(Merge.Counts(unchanged: 1, duplicate: 1, invalid: 1).changed)
        XCTAssertTrue(Merge.Counts(added: 1).changed)
        XCTAssertTrue(Merge.Counts(updated: 1).changed)
        XCTAssertTrue(Merge.Counts(trashed: 1).changed)
    }
}
