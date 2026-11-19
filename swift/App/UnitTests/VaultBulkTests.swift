import XCTest
import TesseraCore
@testable import Tessera

final class VaultBulkTests: XCTestCase {
    private let stamp: Int64 = 1_900_000_000

    private func sample() -> [Account] {
        [VaultFixture.account("a", folder: "Work", tags: ["dev"], pinned: true, updatedAt: 1),
         VaultFixture.account("b", folder: "Home", updatedAt: 1),
         VaultFixture.account("c", updatedAt: 1)]
    }

    private func byID(_ accounts: [Account]) -> [String: Account] {
        Dictionary(uniqueKeysWithValues: accounts.map { ($0.id, $0) })
    }

    func testPinChangesOnlyTheTargetsThatNeedIt() {
        var accounts = sample()
        XCTAssertEqual(VaultEdit.setPinned(true, ids: ["a", "b"], in: &accounts, at: stamp), 1)
        let out = byID(accounts)
        XCTAssertEqual(out["a"]?.updatedAt, 1)
        XCTAssertEqual(out["b"]?.pinned, true)
        XCTAssertEqual(out["b"]?.updatedAt, stamp)
        XCTAssertEqual(out["c"]?.pinned, false)
        XCTAssertEqual(VaultEdit.setPinned(false, ids: ["a", "b", "c"], in: &accounts, at: stamp), 2)
        XCTAssertTrue(accounts.allSatisfy { !$0.pinned })
    }

    func testMoveToFolderTrimsAndSkipsAccountsAlreadyThere() {
        var accounts = sample()
        XCTAssertEqual(VaultEdit.setFolder("  Work ", ids: ["a", "b", "c"], in: &accounts, at: stamp), 2)
        XCTAssertTrue(accounts.allSatisfy { $0.folder == "Work" })
        XCTAssertEqual(VaultEdit.setFolder("", ids: ["a"], in: &accounts, at: stamp), 1)
        XCTAssertEqual(byID(accounts)["a"]?.folder, "")
    }

    func testAddTagNormalizesAndSkipsDuplicates() {
        var accounts = sample()
        XCTAssertEqual(VaultEdit.addTag(" #dev ", ids: ["a", "b"], in: &accounts, at: stamp), 1)
        XCTAssertEqual(byID(accounts)["a"]?.tags, ["dev"])
        XCTAssertEqual(byID(accounts)["b"]?.tags, ["dev"])
        XCTAssertEqual(VaultEdit.addTag("  ", ids: ["c"], in: &accounts, at: stamp), 0)
        XCTAssertEqual(VaultEdit.addTag("#", ids: ["c"], in: &accounts, at: stamp), 0)
    }

    func testRemoveTagTouchesOnlyAccountsCarryingIt() {
        var accounts = sample()
        XCTAssertEqual(VaultEdit.removeTag("dev", ids: ["a", "b", "c"], in: &accounts, at: stamp), 1)
        XCTAssertTrue(accounts.allSatisfy { $0.tags.isEmpty })
        XCTAssertEqual(byID(accounts)["b"]?.updatedAt, 1)
    }

    func testRenameFolderMovesEveryMember() {
        var accounts = sample()
        XCTAssertEqual(VaultEdit.renameFolder("Work", to: " Office ", in: &accounts, at: stamp), 1)
        XCTAssertEqual(byID(accounts)["a"]?.folder, "Office")
        XCTAssertEqual(VaultEdit.renameFolder("Office", to: "Office", in: &accounts, at: stamp), 0)
        XCTAssertEqual(VaultEdit.renameFolder("Office", to: "  ", in: &accounts, at: stamp), 0)
        XCTAssertEqual(VaultEdit.renameFolder("", to: "X", in: &accounts, at: stamp), 0)
    }

    func testRemoveFolderEmptiesItsMembers() {
        var accounts = sample()
        XCTAssertEqual(VaultEdit.removeFolder("Home", in: &accounts, at: stamp), 1)
        XCTAssertEqual(byID(accounts)["b"]?.folder, "")
        XCTAssertEqual(byID(accounts)["a"]?.folder, "Work")
        XCTAssertEqual(VaultEdit.removeFolder("", in: &accounts, at: stamp), 0)
    }

    func testNormalizedTag() {
        XCTAssertEqual(VaultEdit.normalizedTag("##work "), "work")
        XCTAssertEqual(VaultEdit.normalizedTag("two words"), "two words")
        XCTAssertNil(VaultEdit.normalizedTag(" # "))
    }

    func testMenuStateForMixedSelection() {
        let state = AccountMenuState(targets: sample())
        XCTAssertTrue(state.pins)
        XCTAssertNil(state.single)
        XCTAssertNil(state.sharedFolder)
        XCTAssertTrue(state.hasFolder)
        XCTAssertEqual(state.removeFromFolderTitle, "Remove from Folder")
        XCTAssertEqual(state.removableTags, ["dev"])
        XCTAssertFalse(state.hasTagOnAll("dev"))
        XCTAssertFalse(state.canAdvance)
    }

    func testMenuStateForOneAccount() {
        let pinned = AccountMenuState(targets: [sample()[0]])
        XCTAssertFalse(pinned.pins)
        XCTAssertEqual(pinned.sharedFolder, "Work")
        XCTAssertEqual(pinned.removeFromFolderTitle, "Remove from Work")
        XCTAssertTrue(pinned.hasTagOnAll("dev"))
        let counter = AccountMenuState(targets: [VaultFixture.account("h", type: .hotp)])
        XCTAssertTrue(counter.canAdvance)
        XCTAssertFalse(counter.hasFolder)
    }

    func testEmptyMenuStateDisablesTargetedActions() {
        let state = AccountMenuState(targets: [])
        XCTAssertTrue(state.isEmpty)
        XCTAssertNil(state.single)
        XCTAssertFalse(state.hasTagOnAll("dev"))
        XCTAssertTrue(state.removableTags.isEmpty)
    }
}

@MainActor
final class VaultBulkModelTests: XCTestCase {
    func testFailedSaveLeavesAccountsUnchangedAndSaysSo() {
        let model = AppModel(demo: .populated)
        let before = model.accounts
        model.setPinned(true, ids: ["3", "5"])
        XCTAssertEqual(model.accounts, before)
        guard case .failure = model.notice else { return XCTFail("expected a failure notice") }
    }

    func testNoOpEditSavesNothingAndStaysQuiet() {
        let model = AppModel(demo: .populated)
        model.setPinned(true, ids: ["1", "2"])
        XCTAssertNil(model.notice)
        model.addTag("   ", ids: ["1"])
        XCTAssertNil(model.notice)
        model.renameFolder("Nope", to: "Other")
        XCTAssertNil(model.notice)
    }

    func testAllTagsReadsTheVault() {
        let model = AppModel(demo: .populated)
        model.accounts[0].tags = ["ops"]
        model.accounts[1].tags = ["dev", "ops"]
        XCTAssertEqual(model.allTags, ["dev", "ops"])
    }
}
