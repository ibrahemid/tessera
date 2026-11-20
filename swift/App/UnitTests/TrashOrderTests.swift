import XCTest
import TesseraCore
@testable import Tessera

final class TrashOrderTests: XCTestCase {
    func testTrashListsTrashedAccountsNewestFirst() {
        XCTAssertEqual(VaultFixture.ids(TrashOrder.newestFirst(TrashFixture.vault())), ["c", "b"])
    }

    func testTiesFollowTheCoreOrderWithoutGroupingPinned() {
        let stamp: Int64 = 1_750_000_000
        var ordered = TrashFixture.trashed(VaultFixture.account("z", issuer: "Mid"), at: stamp)
        ordered.order = 1
        let accounts = [TrashFixture.trashed(VaultFixture.account("x", issuer: "Zeta", pinned: true), at: stamp),
                        TrashFixture.trashed(VaultFixture.account("y", issuer: "alpha"), at: stamp),
                        ordered,
                        TrashFixture.trashed(VaultFixture.account("w", issuer: "Beta"), at: stamp + 9),
                        TrashFixture.trashed(VaultFixture.account("v", issuer: "Service10"), at: stamp),
                        TrashFixture.trashed(VaultFixture.account("u", issuer: "service2"), at: stamp),
                        VaultFixture.account("t", issuer: "Aaa")]
        XCTAssertEqual(VaultFixture.ids(TrashOrder.newestFirst(accounts)), ["w", "z", "y", "u", "v", "x"])
    }

    func testAnEmptyTrashListsNothing() {
        XCTAssertTrue(TrashOrder.newestFirst(Account.live(TrashFixture.vault())).isEmpty)
        XCTAssertTrue(TrashOrder.newestFirst([]).isEmpty)
    }
}
