import XCTest
import TesseraCore
@testable import Tessera

enum TrashFixture {
    static let acmeTrashedAt: Int64 = 1_700_000_000
    static let dropboxTrashedAt: Int64 = 1_750_000_000

    static func trashed(_ a: Account, at stamp: Int64) -> Account {
        var a = a
        a.deletedAt = stamp
        a.updatedAt = stamp
        return a
    }

    static func vault() -> [Account] {
        [VaultFixture.account("a", issuer: "GitHub", account: "me", handle: "gi", updatedAt: 1),
         trashed(VaultFixture.account("b", issuer: "Acme", account: "ops", handle: "ac"), at: acmeTrashedAt),
         trashed(VaultFixture.account("c", issuer: "Dropbox", account: "me", handle: "dr"), at: dropboxTrashedAt)]
    }

    static func byID(_ accounts: [Account]) -> [String: Account] {
        Dictionary(uniqueKeysWithValues: accounts.map { ($0.id, $0) })
    }
}

final class TrashEditTests: XCTestCase {
    private let now: Int64 = 1_800_000_000

    func testTrashStampsBothTimestampsAndKeepsTheAccount() {
        var accounts = [VaultFixture.account("a", issuer: "GitHub", account: "me", handle: "gi", updatedAt: 1)]
        XCTAssertEqual(VaultEdit.trash(["a"], at: now, in: &accounts), 1)
        XCTAssertEqual(accounts.count, 1)
        XCTAssertEqual(accounts[0].deletedAt, now)
        XCTAssertEqual(accounts[0].updatedAt, now)
        XCTAssertEqual(accounts[0].handle, "gi")
        XCTAssertTrue(accounts[0].isTrashed)
    }

    func testTrashSkipsTrashedAndUnknownAccounts() {
        var accounts = TrashFixture.vault()
        XCTAssertEqual(VaultEdit.trash(["b", "zz"], at: now, in: &accounts), 0)
        XCTAssertEqual(accounts, TrashFixture.vault())
    }

    func testTrashWithoutAPositiveStampChangesNothing() {
        var accounts = TrashFixture.vault()
        XCTAssertEqual(VaultEdit.trash(["a"], at: 0, in: &accounts), 0)
        XCTAssertEqual(VaultEdit.trash(["a"], at: -1, in: &accounts), 0)
        XCTAssertEqual(accounts, TrashFixture.vault())
    }

    func testRestoreBringsAnAccountBack() {
        var accounts = TrashFixture.vault()
        XCTAssertEqual(VaultEdit.restore(["c"], at: now, in: &accounts), 1)
        let c = TrashFixture.byID(accounts)["c"]
        XCTAssertEqual(c?.deletedAt, 0)
        XCTAssertEqual(c?.updatedAt, now)
        XCTAssertEqual(c?.handle, "dr")
    }

    func testRestoreMatchesTrashedAccountsOnly() {
        var accounts = TrashFixture.vault()
        XCTAssertEqual(VaultEdit.restore(["a"], at: now, in: &accounts), 0)
        XCTAssertEqual(accounts, TrashFixture.vault())
    }

    func testSeveralTargetsApplyToEveryAccount() {
        var accounts = TrashFixture.vault()
        XCTAssertEqual(VaultEdit.restore(["c", "b"], at: now, in: &accounts), 2)
        XCTAssertTrue(Account.trashed(accounts).isEmpty)
        XCTAssertEqual(VaultEdit.trash(["a", "c"], at: now, in: &accounts), 2)
        XCTAssertEqual(VaultFixture.ids(Account.live(accounts)), ["b"])
    }

    func testHandlesStayUniqueAcrossTheTrash() throws {
        var accounts = TrashFixture.vault()
        accounts.append(VaultFixture.account("d", issuer: "Dropbox", account: "work"))
        Handles.assign(&accounts)
        XCTAssertEqual(TrashFixture.byID(accounts)["d"]?.handle, "dr2")
        VaultEdit.restore(["c"], at: now, in: &accounts)
        XCTAssertNoThrow(try Handles.checkUniqueness(accounts))
    }

    func testUndoRestoresAccountsStillCarryingItsStamp() {
        var accounts = TrashFixture.vault()
        VaultEdit.trash(["a"], at: now, in: &accounts)
        XCTAssertEqual(VaultEdit.restoreIfStamped(["a": now], at: now + 3, in: &accounts), 1)
        let a = TrashFixture.byID(accounts)["a"]
        XCTAssertEqual(a?.deletedAt, 0)
        XCTAssertEqual(a?.updatedAt, now + 3)
    }

    func testUndoSkipsAccountsWhoseDeletedAtChanged() {
        var accounts = TrashFixture.vault()
        VaultEdit.trash(["a"], at: now, in: &accounts)
        VaultEdit.restore(["a"], at: now + 1, in: &accounts)
        VaultEdit.trash(["a"], at: now + 2, in: &accounts)
        let before = accounts
        XCTAssertEqual(VaultEdit.restoreIfStamped(["a": now, "c": now], at: now + 3, in: &accounts), 0)
        XCTAssertEqual(accounts, before)
    }

    func testUndoSkipsAccountsAlreadyRestored() {
        var accounts = TrashFixture.vault()
        VaultEdit.trash(["a"], at: now, in: &accounts)
        VaultEdit.restore(["a"], at: now + 1, in: &accounts)
        XCTAssertEqual(VaultEdit.restoreIfStamped(["a": now], at: now + 3, in: &accounts), 0)
        XCTAssertEqual(TrashFixture.byID(accounts)["a"]?.updatedAt, now + 1)
    }

    func testEmptyTrashDeletesOnlyTheTrash() {
        var accounts = TrashFixture.vault()
        let result = VaultEdit.purge(unchangedSince: TrashOrder.newestFirst(accounts), in: &accounts)
        XCTAssertEqual(result.removed, 2)
        XCTAssertEqual(result.changed, 0)
        XCTAssertEqual(VaultFixture.ids(accounts), ["a"])
        let again = VaultEdit.purge(unchangedSince: TrashOrder.newestFirst(accounts), in: &accounts)
        XCTAssertEqual(again.removed, 0)
        XCTAssertEqual(again.changed, 0)
        XCTAssertEqual(VaultFixture.ids(accounts), ["a"])
    }

    func testPurgeReachesTheTrashAndLiveAccounts() {
        var accounts = TrashFixture.vault()
        let byID = TrashFixture.byID(accounts)
        let snapshot = [byID["c"], byID["a"]].compactMap { $0 }
        let result = VaultEdit.purge(unchangedSince: snapshot, in: &accounts)
        XCTAssertEqual(result.removed, 2)
        XCTAssertEqual(VaultFixture.ids(accounts), ["b"])
    }

    func testARestoreSinceTheSnapshotCancelsThePurge() {
        for (snapshotIDs, restored) in [(["c"], "c"), (["c", "b"], "b")] {
            var accounts = TrashFixture.vault()
            let byID = TrashFixture.byID(accounts)
            let snapshot = snapshotIDs.compactMap { byID[$0] }
            VaultEdit.restore([restored], at: now, in: &accounts)
            let result = VaultEdit.purge(unchangedSince: snapshot, in: &accounts)
            XCTAssertEqual(result.removed, 0, restored)
            XCTAssertEqual(result.changed, 1, restored)
            XCTAssertEqual(accounts.count, 3, restored)
        }
    }

    func testPurgeCountsAccountsThatAreGoneOrEdited() {
        var accounts = TrashFixture.vault()
        let snapshot = TrashOrder.newestFirst(accounts)
        accounts.removeAll { $0.id == "c" }
        accounts[accounts.firstIndex { $0.id == "b" }!].note = "kept"
        let before = accounts
        let result = VaultEdit.purge(unchangedSince: snapshot, in: &accounts)
        XCTAssertEqual(result.removed, 0)
        XCTAssertEqual(result.changed, 2)
        XCTAssertEqual(accounts, before)
    }

    func testARestoreInTheSameSecondStillCountsAsAChange() {
        var accounts = TrashFixture.vault()
        let snapshot = [TrashFixture.byID(accounts)["c"]].compactMap { $0 }
        VaultEdit.restore(["c"], at: TrashFixture.dropboxTrashedAt, in: &accounts)
        XCTAssertEqual(TrashFixture.byID(accounts)["c"]?.updatedAt, snapshot.first?.updatedAt)
        let result = VaultEdit.purge(unchangedSince: snapshot, in: &accounts)
        XCTAssertEqual(result.removed, 0)
        XCTAssertEqual(result.changed, 1)
        XCTAssertEqual(accounts.count, 3)
    }
}
