import AppKit
import SwiftUI
import XCTest
@testable import Tessera

@MainActor
private final class FakeIdleTime: IdleTimeSource {
    var seconds: TimeInterval = 0
    func secondsSinceLastInput() -> TimeInterval { seconds }
}

@MainActor
final class LockPolicyTests: XCTestCase {
    private let workspace = NotificationCenter()
    private let distributed = NotificationCenter()
    private let appCenter = NotificationCenter()
    private lazy var idle = FakeIdleTime()
    private var isVaultUnlocked = true
    private var locks: [LockReason] = []

    private func makeDefaults() throws -> UserDefaults {
        let suite = "tessera.tests.\(UUID().uuidString)"
        let defaults = try XCTUnwrap(UserDefaults(suiteName: suite))
        addTeardownBlock { UserDefaults().removePersistentDomain(forName: suite) }
        return defaults
    }

    private func makePolicy(_ defaults: UserDefaults) -> LockPolicy {
        let policy = LockPolicy(defaults: defaults, idle: idle,
                                workspace: workspace, distributed: distributed, appCenter: appCenter,
                                pollInterval: .seconds(3600),
                                isVaultUnlocked: { [unowned self] in isVaultUnlocked },
                                onLock: { [unowned self] in locks.append($0) })
        addTeardownBlock { @MainActor in policy.stop() }
        return policy
    }

    func testScreenLockLocksAtOnce() throws {
        _ = makePolicy(try makeDefaults())
        distributed.post(name: LockPolicy.screenIsLocked, object: nil)
        XCTAssertEqual(locks, [.screenLocked])
    }

    func testSleepLocksAtOnce() throws {
        _ = makePolicy(try makeDefaults())
        workspace.post(name: NSWorkspace.willSleepNotification, object: nil)
        XCTAssertEqual(locks, [.sleep])
    }

    func testUserSwitchLocksAtOnce() throws {
        _ = makePolicy(try makeDefaults())
        workspace.post(name: NSWorkspace.sessionDidResignActiveNotification, object: nil)
        XCTAssertEqual(locks, [.sessionResigned])
    }

    func testQuitLocksAtOnce() throws {
        _ = makePolicy(try makeDefaults())
        appCenter.post(name: NSApplication.willTerminateNotification, object: nil)
        XCTAssertEqual(locks, [.quit])
    }

    func testEachCenterOnlyDrivesItsOwnReason() throws {
        _ = makePolicy(try makeDefaults())
        workspace.post(name: LockPolicy.screenIsLocked, object: nil)
        distributed.post(name: NSWorkspace.willSleepNotification, object: nil)
        workspace.post(name: NSApplication.willTerminateNotification, object: nil)
        XCTAssertEqual(locks, [])
    }

    func testIdleBelowTheLimitKeepsTheVaultOpen() throws {
        let defaults = try makeDefaults()
        defaults.set(5, forKey: AutoLock.minutesPrefKey)
        let policy = makePolicy(defaults)
        idle.seconds = 5 * 60 - 1
        policy.checkIdle()
        XCTAssertEqual(locks, [])
    }

    func testIdleAtTheLimitLocks() throws {
        let defaults = try makeDefaults()
        defaults.set(5, forKey: AutoLock.minutesPrefKey)
        let policy = makePolicy(defaults)
        idle.seconds = 5 * 60
        policy.checkIdle()
        XCTAssertEqual(locks, [.idle])
    }

    func testUnsetLimitIsFifteenMinutes() throws {
        let policy = makePolicy(try makeDefaults())
        idle.seconds = 15 * 60 - 1
        policy.checkIdle()
        XCTAssertEqual(locks, [])
        idle.seconds = 15 * 60
        policy.checkIdle()
        XCTAssertEqual(locks, [.idle])
    }

    func testNeverDisablesTheIdleLock() throws {
        let defaults = try makeDefaults()
        defaults.set(AutoLock.never, forKey: AutoLock.minutesPrefKey)
        let policy = makePolicy(defaults)
        idle.seconds = 24 * 60 * 60
        policy.checkIdle()
        XCTAssertEqual(locks, [])
    }

    func testLockedVaultIsNotLockedAgainForIdle() throws {
        let policy = makePolicy(try makeDefaults())
        isVaultUnlocked = false
        idle.seconds = 24 * 60 * 60
        policy.checkIdle()
        XCTAssertEqual(locks, [])
    }

    func testStoppedPolicyIgnoresEvents() throws {
        let policy = makePolicy(try makeDefaults())
        policy.stop()
        distributed.post(name: LockPolicy.screenIsLocked, object: nil)
        workspace.post(name: NSWorkspace.willSleepNotification, object: nil)
        workspace.post(name: NSWorkspace.sessionDidResignActiveNotification, object: nil)
        appCenter.post(name: NSApplication.willTerminateNotification, object: nil)
        idle.seconds = 24 * 60 * 60
        policy.checkIdle()
        XCTAssertEqual(locks, [])
    }

    func testPollChecksIdleOnItsInterval() async throws {
        let defaults = try makeDefaults()
        defaults.set(1, forKey: AutoLock.minutesPrefKey)
        idle.seconds = 60
        let policy = LockPolicy(defaults: defaults, idle: idle,
                                workspace: workspace, distributed: distributed, appCenter: appCenter,
                                pollInterval: .milliseconds(10),
                                isVaultUnlocked: { true },
                                onLock: { [unowned self] in locks.append($0) })
        defer { policy.stop() }
        for _ in 0..<200 where locks.isEmpty {
            try await Task.sleep(for: .milliseconds(10))
        }
        XCTAssertEqual(locks.first, .idle)
    }
}

@MainActor
final class AutoLockTests: XCTestCase {
    private func makeDefaults() throws -> UserDefaults {
        let suite = "tessera.tests.\(UUID().uuidString)"
        let defaults = try XCTUnwrap(UserDefaults(suiteName: suite))
        addTeardownBlock { UserDefaults().removePersistentDomain(forName: suite) }
        return defaults
    }

    func testChoicesAndLabels() {
        XCTAssertEqual(AutoLock.choices, [1, 5, 15, 30, 60, 0])
        XCTAssertEqual(AutoLock.choices.map(AutoLock.label(for:)),
                       ["1 minute", "5 minutes", "15 minutes", "30 minutes", "1 hour", "Never"])
    }

    func testStoredMinutesWinOverTheDefault() throws {
        let defaults = try makeDefaults()
        XCTAssertEqual(AutoLock.minutes(in: defaults), 15)
        defaults.set(AutoLock.never, forKey: AutoLock.minutesPrefKey)
        XCTAssertEqual(AutoLock.minutes(in: defaults), 0)
        defaults.set(60, forKey: AutoLock.minutesPrefKey)
        XCTAssertEqual(AutoLock.minutes(in: defaults), 60)
    }

    func testSectionShowsTheValueFromItsOwnStore() throws {
        let half = try makeDefaults()
        half.set(30, forKey: AutoLock.minutesPrefKey)
        let hour = try makeDefaults()
        hour.set(60, forKey: AutoLock.minutesPrefKey)
        let first = try render(half)
        XCTAssertEqual(try render(half), first)
        XCTAssertNotEqual(try render(hour), first)
        XCTAssertEqual(half.integer(forKey: AutoLock.minutesPrefKey), 30)
        XCTAssertEqual(hour.integer(forKey: AutoLock.minutesPrefKey), 60)
    }

    private func render(_ defaults: UserDefaults) throws -> Data {
        let view = NSHostingView(rootView: Form { AutoLockSection(defaults: defaults) }.formStyle(.grouped))
        view.frame = NSRect(x: 0, y: 0, width: 480, height: 160)
        view.appearance = NSAppearance(named: .aqua)
        view.layoutSubtreeIfNeeded()
        let bitmap = try XCTUnwrap(view.bitmapImageRepForCachingDisplay(in: view.bounds))
        view.cacheDisplay(in: view.bounds, to: bitmap)
        return try XCTUnwrap(bitmap.representation(using: .png, properties: [:]))
    }
}
