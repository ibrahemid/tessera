import XCTest
import TesseraCore
@testable import Tessera

final class InterfaceMenuStateTests: XCTestCase {
    func testPinUsesTheRowStarSymbol() {
        let state = AccountMenuState(targets: [VaultFixture.account("a")])
        XCTAssertEqual(state.pinTitle, "Pin")
        XCTAssertEqual(state.pinSymbol, "star")
    }

    func testUnpinUsesTheSlashedStar() {
        let state = AccountMenuState(targets: [VaultFixture.account("a", pinned: true)])
        XCTAssertEqual(state.pinTitle, "Unpin")
        XCTAssertEqual(state.pinSymbol, "star.slash")
    }

    func testMixedSelectionOffersPin() {
        let state = AccountMenuState(targets: [VaultFixture.account("a", pinned: true), VaultFixture.account("b")])
        XCTAssertEqual(state.pinTitle, "Pin")
        XCTAssertEqual(state.pinSymbol, "star")
    }
}
