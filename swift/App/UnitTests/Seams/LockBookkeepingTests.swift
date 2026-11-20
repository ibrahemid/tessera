import XCTest
import TesseraCore
@testable import Tessera

@MainActor
final class LockBookkeepingTests: XCTestCase {
    private let allReasons: [LockReason] = [.manual, .idle, .screenLocked, .sleep, .sessionResigned, .quit, .keyChanged]

    private func preserveAuthFlag() {
        let key = AppModel.lockRequiresAuthPrefKey
        let saved = UserDefaults.standard.object(forKey: key) as? Bool
        UserDefaults.standard.removeObject(forKey: key)
        addTeardownBlock {
            if let saved { UserDefaults.standard.set(saved, forKey: key) }
            else { UserDefaults.standard.removeObject(forKey: key) }
        }
    }

    func testEveryLockIncrementsTheGeneration() {
        preserveAuthFlag()
        let model = AppModel(demo: .populated)
        let start = model.lockGeneration
        model.lock(reason: .sleep)
        XCTAssertEqual(model.lockGeneration, start + 1)
        model.lock(reason: .quit)
        XCTAssertEqual(model.lockGeneration, start + 2)
    }

    func testLockClearsTheUnlockedState() {
        preserveAuthFlag()
        let model = AppModel(demo: .populated)
        model.dek = Data(repeating: 1, count: 32)
        model.appKey = Data(repeating: 2, count: 32)
        model.notice = .info("Copied")
        model.lock(reason: .screenLocked)
        XCTAssertTrue(model.isLocked)
        XCTAssertTrue(model.accounts.isEmpty)
        XCTAssertNil(model.dek)
        XCTAssertNil(model.appKey)
        XCTAssertNil(model.envelope)
        XCTAssertNil(model.notice)
        XCTAssertTrue(model.codeCache.isEmpty)
    }

    func testOnlyManualAndIdleRequireOwnerAuth() {
        preserveAuthFlag()
        XCTAssertEqual(allReasons.filter(\.requiresOwnerAuth), [.manual, .idle])
        for reason in allReasons {
            UserDefaults.standard.removeObject(forKey: AppModel.lockRequiresAuthPrefKey)
            let model = AppModel(demo: .populated)
            model.lock(reason: reason)
            XCTAssertEqual(model.isOwnerAuthRequired, reason.requiresOwnerAuth, "\(reason)")
            XCTAssertEqual(UserDefaults.standard.bool(forKey: AppModel.lockRequiresAuthPrefKey),
                           reason.requiresOwnerAuth, "\(reason)")
        }
    }

    func testDefaultLockIsManual() {
        preserveAuthFlag()
        let model = AppModel(demo: .populated)
        let posted = XCTNSNotificationExpectation(name: .tesseraVaultDidLock, object: model)
        posted.handler = { $0.userInfo?["reason"] as? String == LockReason.manual.rawValue }
        model.lock()
        wait(for: [posted], timeout: 1)
        XCTAssertTrue(model.isOwnerAuthRequired)
    }

    func testOwnerAuthFlagIsKeptAcrossALaterSilentLock() {
        preserveAuthFlag()
        let model = AppModel(demo: .populated)
        model.lock(reason: .idle)
        model.lock(reason: .sleep)
        XCTAssertTrue(model.isOwnerAuthRequired)
    }

    func testNotificationIsPostedOnceWithTheReason() {
        preserveAuthFlag()
        let model = AppModel(demo: .populated)
        let posted = XCTNSNotificationExpectation(name: .tesseraVaultDidLock, object: model)
        posted.expectedFulfillmentCount = 1
        posted.assertForOverFulfill = true
        let withReason = XCTNSNotificationExpectation(name: .tesseraVaultDidLock, object: model)
        withReason.handler = { $0.userInfo?["reason"] as? String == LockReason.idle.rawValue }
        model.lock(reason: .idle)
        model.lock(reason: .idle)
        model.lock(reason: .manual)
        wait(for: [posted, withReason], timeout: 1)
    }

    func testLockingALockedVaultPostsNothing() {
        preserveAuthFlag()
        let model = AppModel(demo: .locked)
        let posted = XCTNSNotificationExpectation(name: .tesseraVaultDidLock, object: model)
        posted.isInverted = true
        model.lock(reason: .sleep)
        wait(for: [posted], timeout: 0.2)
    }
}
