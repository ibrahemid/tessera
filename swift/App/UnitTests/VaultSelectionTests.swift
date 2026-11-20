import XCTest
@testable import Tessera

final class VaultSelectionTests: XCTestCase {
    private let order = ["a", "b", "c", "d", "e"]

    func testSelectReplacesSelectionAndSetsCursor() {
        var s = VaultSelection()
        s.select("b")
        s.select("d")
        XCTAssertEqual(s.ids, ["d"])
        XCTAssertEqual(s.cursor, "d")
        XCTAssertEqual(s.single, "d")
        XCTAssertFalse(s.isMultiple)
    }

    func testToggleAddsAndRemovesWithoutLosingTheCursor() {
        var s = VaultSelection()
        s.toggle("b", in: order)
        s.toggle("d", in: order)
        XCTAssertEqual(s.ids, ["b", "d"])
        XCTAssertEqual(s.cursor, "d")
        XCTAssertTrue(s.isMultiple)
        s.toggle("d", in: order)
        XCTAssertEqual(s.ids, ["b"])
        XCTAssertEqual(s.cursor, "b")
        s.toggle("b", in: order)
        XCTAssertTrue(s.isEmpty)
        XCTAssertNil(s.cursor)
    }

    func testExtendSelectsTheRangeFromTheAnchorInBothDirections() {
        var s = VaultSelection()
        s.select("c")
        s.extend(to: "e", in: order)
        XCTAssertEqual(s.ordered(in: order), ["c", "d", "e"])
        s.extend(to: "a", in: order)
        XCTAssertEqual(s.ordered(in: order), ["a", "b", "c"])
        XCTAssertEqual(s.cursor, "a")
    }

    func testExtendWithNothingSelectedSelectsJustTheRow() {
        var s = VaultSelection()
        s.extend(to: "c", in: order)
        XCTAssertEqual(s.ids, ["c"])
    }

    func testExtendIgnoresRowsThatAreNotShown() {
        var s = VaultSelection()
        s.select("a")
        s.extend(to: "zz", in: order)
        XCTAssertEqual(s.ids, ["a"])
    }

    func testMoveStartsAtTheEdgesAndClamps() {
        var s = VaultSelection()
        s.move(by: 1, in: order, extending: false)
        XCTAssertEqual(s.ids, ["a"])
        s.move(by: -1, in: order, extending: false)
        XCTAssertEqual(s.ids, ["a"])
        var up = VaultSelection()
        up.move(by: -1, in: order, extending: false)
        XCTAssertEqual(up.ids, ["e"])
        up.move(by: 1, in: order, extending: false)
        XCTAssertEqual(up.ids, ["e"])
    }

    func testMoveStepsTheCursor() {
        var s = VaultSelection()
        s.select("b")
        s.move(by: 1, in: order, extending: false)
        XCTAssertEqual(s.ids, ["c"])
        s.move(by: -1, in: order, extending: false)
        XCTAssertEqual(s.ids, ["b"])
    }

    func testShiftMoveGrowsAndShrinksTheRange() {
        var s = VaultSelection()
        s.select("b")
        s.move(by: 1, in: order, extending: true)
        s.move(by: 1, in: order, extending: true)
        XCTAssertEqual(s.ordered(in: order), ["b", "c", "d"])
        s.move(by: -1, in: order, extending: true)
        XCTAssertEqual(s.ordered(in: order), ["b", "c"])
    }

    func testMoveOnAnEmptyListDoesNothing() {
        var s = VaultSelection()
        s.move(by: 1, in: [], extending: false)
        XCTAssertTrue(s.isEmpty)
    }

    func testPruneDropsRowsThatLeftTheList() {
        var s = VaultSelection()
        s.toggle("b", in: order)
        s.toggle("d", in: order)
        s.prune(to: ["a", "b", "c"])
        XCTAssertEqual(s.ids, ["b"])
        XCTAssertEqual(s.cursor, "b")
        s.prune(to: ["a"])
        XCTAssertTrue(s.isEmpty)
        XCTAssertNil(s.cursor)
    }

    func testClearEmptiesEverything() {
        var s = VaultSelection()
        s.toggle("a", in: order)
        s.toggle("b", in: order)
        s.clear()
        XCTAssertTrue(s.isEmpty)
        XCTAssertNil(s.cursor)
        XCTAssertNil(s.anchor)
    }

    func testClickRoutesByKind() {
        var s = VaultSelection()
        s.click("b", .activate, in: order)
        s.click("d", .toggle, in: order)
        XCTAssertEqual(s.ids, ["b", "d"])
        s.click("e", .extend, in: order)
        XCTAssertEqual(s.ordered(in: order), ["d", "e"])
        s.click("a", .activate, in: order)
        XCTAssertEqual(s.ids, ["a"])
    }

    func testTargetsUseTheSelectionOnlyWhenTheClickedRowIsPartOfIt() {
        var s = VaultSelection()
        s.toggle("d", in: order)
        s.toggle("b", in: order)
        XCTAssertEqual(s.targets(clicked: "d", in: order), ["b", "d"])
        XCTAssertEqual(s.targets(clicked: "a", in: order), ["a"])
        XCTAssertEqual(s.targets(clicked: nil, in: order), ["b", "d"])
        var one = VaultSelection()
        one.select("c")
        XCTAssertEqual(one.targets(clicked: "c", in: order), ["c"])
    }

    func testActivationTargetIsTheCursorOrTheTopRow() {
        var s = VaultSelection()
        XCTAssertEqual(s.activationTarget(in: order), "a")
        XCTAssertNil(s.activationTarget(in: []))
        s.select("c")
        XCTAssertEqual(s.activationTarget(in: order), "c")
        XCTAssertEqual(s.activationTarget(in: ["a", "b"]), "a")
    }
}
