import AppKit
import XCTest
import TesseraCore
@testable import Tessera

final class VaultTimingTests: XCTestCase {
    private let windowStart: TimeInterval = 1_800_000_000

    private func at(_ seconds: TimeInterval) -> Date { Date(timeIntervalSince1970: windowStart + seconds) }

    func testTicksLandJustAfterTheNextWallClockSecond() {
        XCTAssertEqual(TickSchedule.nextFire(after: at(0.4)).timeIntervalSince1970, windowStart + 1 + TickSchedule.lead,
                       accuracy: 0.0001)
        XCTAssertEqual(TickSchedule.nextFire(after: at(0)).timeIntervalSince1970, windowStart + 1 + TickSchedule.lead,
                       accuracy: 0.0001)
        XCTAssertEqual(TickSchedule.nextFire(after: at(0.999)).timeIntervalSince1970, windowStart + 1 + TickSchedule.lead,
                       accuracy: 0.0001)
        XCTAssertLessThan(TickSchedule.lead, 0.1)
    }

    func testWindowEndIsTheNextPeriodBoundary() {
        let a = VaultFixture.account("a")
        XCTAssertEqual(CodeTiming.windowEnd(a, at: at(12)), at(30))
        XCTAssertEqual(CodeTiming.windowEnd(a, at: at(0)), at(30))
        XCTAssertEqual(CodeTiming.windowEnd(a, at: at(29.9)), at(30))
        let fast = VaultFixture.account("f", period: 15)
        XCTAssertEqual(CodeTiming.windowEnd(fast, at: at(16)), at(30))
    }

    func testSteamAlwaysUsesAThirtySecondWindow() {
        let steam = VaultFixture.account("s", type: .steam, period: 60)
        XCTAssertEqual(CodeTiming.period(of: steam), 30)
        XCTAssertEqual(CodeTiming.windowEnd(steam, at: at(40)), at(60))
    }

    func testCopyTakesTheNextCodeOnlyInTheLastThreeSeconds() {
        let a = VaultFixture.account("a")
        XCTAssertFalse(CodeTiming.shouldCopyNext(a, at: at(27)))
        XCTAssertTrue(CodeTiming.shouldCopyNext(a, at: at(27.5)))
        XCTAssertTrue(CodeTiming.shouldCopyNext(a, at: at(29.9)))
        XCTAssertFalse(CodeTiming.shouldCopyNext(a, at: at(0)))
        XCTAssertFalse(CodeTiming.shouldCopyNext(VaultFixture.account("h", type: .hotp), at: at(29.9)))
        XCTAssertFalse(CodeTiming.shouldCopyNext(VaultFixture.account("z", period: 0), at: at(29.9)))
    }

    func testNextCodeShowsWhenSelectedOrInTheLastFiveSeconds() {
        let a = VaultFixture.account("a")
        XCTAssertFalse(CodeTiming.showsNext(a, at: at(24), selected: false))
        XCTAssertTrue(CodeTiming.showsNext(a, at: at(25), selected: false))
        XCTAssertTrue(CodeTiming.showsNext(a, at: at(2), selected: true))
        XCTAssertFalse(CodeTiming.showsNext(VaultFixture.account("h", type: .hotp), at: at(29), selected: true))
    }

    func testRingAnimatesOnlyWhileDepleting() {
        XCTAssertTrue(RingMotion.animates(from: 0.5, to: 0.45, reduceMotion: false))
        XCTAssertFalse(RingMotion.animates(from: 0.05, to: 1, reduceMotion: false))
        XCTAssertFalse(RingMotion.animates(from: nil, to: 0.5, reduceMotion: false))
        XCTAssertFalse(RingMotion.animates(from: 0.5, to: 0.45, reduceMotion: true))
    }
}

@MainActor
final class VaultCodeTests: XCTestCase {
    private let windowStart: TimeInterval = 1_800_000_000

    private func at(_ seconds: TimeInterval) -> Date { Date(timeIntervalSince1970: windowStart + seconds) }

    private func makeFixture() -> (AppModel, NSPasteboard) {
        (AppModel(demo: .populated), NSPasteboard(name: NSPasteboard.Name("tessera.tests.\(UUID().uuidString)")))
    }

    private func release(_ model: AppModel, _ pasteboard: NSPasteboard) {
        model.clipboard.pendingClear?.cancel()
        pasteboard.releaseGlobally()
    }

    private func totp(_ model: AppModel) -> Account { model.accounts.first { $0.type == .totp }! }

    func testFrozenTickerRefreshReturnsTheFrozenInstant() {
        let ticker = Ticker()
        ticker.freeze(at: at(12))
        XCTAssertEqual(ticker.refresh(), at(12))
        XCTAssertEqual(ticker.now, at(12))
    }

    func testRefreshReadsTheWallClock() {
        let ticker = Ticker()
        let before = Date()
        let now = ticker.refresh()
        XCTAssertGreaterThanOrEqual(now, before)
        XCTAssertEqual(ticker.now, now)
    }

    func testCopiedCodeEqualsTheCodeOnScreen() throws {
        let (model, pasteboard) = makeFixture()
        defer { release(model, pasteboard) }
        let a = totp(model)
        let shown = model.code(for: a, at: at(12))
        let copied = model.copyCode(a, at: at(12), to: pasteboard)
        XCTAssertEqual(copied, shown)
        XCTAssertEqual(shown, try OTP.code(for: a, at: at(12)))
        XCTAssertEqual(pasteboard.string(forType: .string), shown)
        XCTAssertEqual(model.notice, .info("Copied \(a.displayName)"))
    }

    func testCopyInTheLastSecondsTakesTheNextCodeAndSaysSo() throws {
        let (model, pasteboard) = makeFixture()
        defer { release(model, pasteboard) }
        let a = totp(model)
        let next = try OTP.code(for: a, at: at(30))
        let copied = model.copyCode(a, at: at(28), to: pasteboard)
        XCTAssertEqual(copied, next)
        XCTAssertEqual(model.nextCode(for: a, at: at(28)), next)
        XCTAssertEqual(pasteboard.string(forType: .string), next)
        XCTAssertEqual(model.notice, .info("Copied the next code for \(a.displayName)"))
    }

    func testCopiedCodeIsMarkedTransientForClipboardManagers() {
        let (model, pasteboard) = makeFixture()
        defer { release(model, pasteboard) }
        model.copyCode(totp(model), at: at(12), to: pasteboard)
        let types = pasteboard.types ?? []
        XCTAssertTrue(types.contains(.nsTransient))
        XCTAssertTrue(types.contains(.nsConcealed))
    }

    func testCopyFailureLeavesThePasteboardAloneAndSaysSo() {
        let (model, pasteboard) = makeFixture()
        defer { release(model, pasteboard) }
        pasteboard.clearContents()
        pasteboard.setString("keep", forType: .string)
        let broken = VaultFixture.account("x", issuer: "Broken", period: 0)
        XCTAssertNil(model.copyCode(broken, at: at(12), to: pasteboard))
        XCTAssertEqual(pasteboard.string(forType: .string), "keep")
        guard case .failure = model.notice else { return XCTFail("expected a failure notice") }
    }

    func testNextCodeIsEmptyForCounterAccounts() {
        let model = AppModel(demo: .populated)
        let hotp = model.accounts.first { $0.type == .hotp }!
        XCTAssertEqual(model.nextCode(for: hotp, at: at(12)), "")
    }

    func testRemainingMatchesTheCore() {
        let model = AppModel(demo: .populated)
        let a = totp(model)
        XCTAssertEqual(model.remaining(for: a, at: at(12)), OTP.remainingSeconds(time: at(12), period: a.period))
    }
}
