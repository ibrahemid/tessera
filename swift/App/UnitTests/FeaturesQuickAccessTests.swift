import AppKit
import SwiftUI
import XCTest
@testable import Tessera
import TesseraCore

final class FeaturesQuickAccessListTests: XCTestCase {
    private let order = ["a", "b", "c"]

    func testNothingIsTargetedUntilTheUserTypesOrMoves() {
        let list = QuickAccessList()
        XCTAssertNil(list.target(in: order))
        XCTAssertFalse(list.isHighlighted("a", in: order))
    }

    func testTypingTargetsTheFirstMatch() {
        var list = QuickAccessList()
        list.query = "g"
        XCTAssertEqual(list.target(in: order), "a")
        XCTAssertNil(list.target(in: []))
    }

    func testMovingFromNothingStartsAtTheMatchingEnd() {
        var down = QuickAccessList()
        down.move(by: 1, in: order)
        XCTAssertEqual(down.cursor, "a")
        var up = QuickAccessList()
        up.move(by: -1, in: order)
        XCTAssertEqual(up.cursor, "c")
    }

    func testMovingStopsAtTheEnds() {
        var list = QuickAccessList()
        list.point(at: "c")
        list.move(by: 1, in: order)
        XCTAssertEqual(list.cursor, "c")
        list.move(by: -5, in: order)
        XCTAssertEqual(list.cursor, "a")
        list.move(by: 1, in: [])
        XCTAssertEqual(list.cursor, "a")
    }

    func testMovingAfterTypingStartsFromTheFirstMatch() {
        var list = QuickAccessList()
        list.query = "x"
        list.move(by: 1, in: order)
        XCTAssertEqual(list.cursor, "b")
    }

    func testChangingTheQueryClearsTheCursor() {
        var list = QuickAccessList()
        list.point(at: "b")
        list.query = ""
        XCTAssertEqual(list.cursor, "b")
        list.query = "g"
        XCTAssertNil(list.cursor)
    }

    func testCursorOutsideTheListFallsBack() {
        var list = QuickAccessList()
        list.point(at: "z")
        XCTAssertNil(list.target(in: order))
        list.query = "q"
        list.point(at: "z")
        XCTAssertEqual(list.target(in: order), "a")
    }

    func testOnlyBareArrowsMove() {
        XCTAssertEqual(QuickAccessList.moveDelta(key: .downArrow, modifiers: []), 1)
        XCTAssertEqual(QuickAccessList.moveDelta(key: .upArrow, modifiers: []), -1)
        XCTAssertNil(QuickAccessList.moveDelta(key: .downArrow, modifiers: [.shift]))
        XCTAssertNil(QuickAccessList.moveDelta(key: .upArrow, modifiers: [.command]))
        XCTAssertNil(QuickAccessList.moveDelta(key: .return, modifiers: []))
    }

    func testVisibleAccountsFollowTheQuery() {
        let accounts = [VaultFixture.account("1", issuer: "GitHub"), VaultFixture.account("2", issuer: "AWS")]
        var list = QuickAccessList()
        XCTAssertEqual(VaultFixture.ids(list.visible(accounts)), ["1", "2"])
        list.query = "git"
        XCTAssertEqual(VaultFixture.ids(list.visible(accounts)), ["1"])
    }
}

@MainActor
final class FeaturesQuickAccessClockTests: XCTestCase {
    func testClockRunsOnlyBetweenStartAndStop() {
        let clock = QuickAccessClock()
        XCTAssertFalse(clock.isRunning)
        clock.start()
        XCTAssertTrue(clock.isRunning)
        clock.stop()
        XCTAssertFalse(clock.isRunning)
    }

    func testFrozenClockNeverStarts() {
        let clock = QuickAccessClock()
        let instant = Date(timeIntervalSince1970: 1_800_000_012)
        clock.freeze(at: instant)
        clock.start()
        XCTAssertFalse(clock.isRunning)
        XCTAssertEqual(clock.refresh(), instant)
    }
}

@MainActor
final class FeaturesQuickAccessModelTests: XCTestCase {
    private let time = Date(timeIntervalSince1970: 1_800_000_012)

    private func pasteboard() -> NSPasteboard {
        let board = NSPasteboard(name: NSPasteboard.Name("tessera.tests.\(UUID().uuidString)"))
        addTeardownBlock { @MainActor in board.releaseGlobally() }
        return board
    }

    private func totp(_ model: AppModel) throws -> Account {
        try XCTUnwrap(model.accounts.first { $0.type == .totp })
    }

    func testLockStateFollowsTheModel() {
        XCTAssertEqual(QuickAccessLockState(AppModel(demo: .populated)), .open)
        let locked = AppModel(demo: .locked)
        XCTAssertEqual(QuickAccessLockState(locked), .locked(touchID: true))
        locked.requireBiometrics = false
        XCTAssertEqual(QuickAccessLockState(locked), .locked(touchID: false))
        locked.needsPassphrase = true
        XCTAssertEqual(QuickAccessLockState(locked), .needsWindow)
        let unreachable = AppModel(demo: .populated)
        unreachable.vaultUnreachable = true
        XCTAssertEqual(QuickAccessLockState(unreachable), .needsWindow)
    }

    func testCopyPutsTheShownCodeOnThePasteboard() throws {
        let model = AppModel(demo: .populated)
        defer { model.clipboard.pendingClear?.cancel() }
        let board = pasteboard()
        let account = try totp(model)
        XCTAssertTrue(model.copyFromQuickAccess(account, at: time, to: board))
        XCTAssertEqual(board.string(forType: .string), model.code(for: account, at: time))
    }

    func testCopyNearExpiryUsesTheNextCode() throws {
        let model = AppModel(demo: .populated)
        defer { model.clipboard.pendingClear?.cancel() }
        let board = pasteboard()
        let account = try totp(model)
        let late = CodeTiming.windowEnd(account, at: time).addingTimeInterval(-1)
        XCTAssertTrue(model.copyFromQuickAccess(account, at: late, to: board))
        XCTAssertEqual(board.string(forType: .string),
                       try OTP.code(for: account, at: CodeTiming.windowEnd(account, at: late)))
    }

    func testLockedModelCopiesNothing() throws {
        let model = AppModel(demo: .populated)
        let account = try totp(model)
        let board = pasteboard()
        model.isLocked = true
        XCTAssertFalse(model.copyFromQuickAccess(account, at: time, to: board))
        XCTAssertNil(board.string(forType: .string))
    }
}
