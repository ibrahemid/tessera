import XCTest
import ServiceManagement
@testable import Tessera

@MainActor
private final class FakeLoginItems: LoginItemService {
    var status: SMAppService.Status
    var failure: Error?
    var onRegister: SMAppService.Status = .enabled

    init(_ status: SMAppService.Status) { self.status = status }

    func register() throws {
        if let failure { throw failure }
        status = onRegister
    }

    func unregister() throws {
        if let failure { throw failure }
        status = .notRegistered
    }
}

private struct RegistrationRefused: Error {}

@MainActor
final class SheetsLaunchAtLoginTests: XCTestCase {
    func testReadsSystemStatus() {
        XCTAssertTrue(LaunchAtLogin(service: FakeLoginItems(.enabled)).isEnabled)
        XCTAssertFalse(LaunchAtLogin(service: FakeLoginItems(.notRegistered)).isEnabled)
        XCTAssertFalse(LaunchAtLogin(service: FakeLoginItems(.notFound)).isEnabled)
        let pending = LaunchAtLogin(service: FakeLoginItems(.requiresApproval))
        XCTAssertTrue(pending.isEnabled)
        XCTAssertTrue(pending.needsApproval)
    }

    func testTurningOnRegistersAndFollowsStatus() {
        let service = FakeLoginItems(.notRegistered)
        service.onRegister = .requiresApproval
        let login = LaunchAtLogin(service: service)
        login.setEnabled(true)
        XCTAssertTrue(login.isEnabled)
        XCTAssertTrue(login.needsApproval)
        XCTAssertNil(login.errorText)
    }

    func testFailedRegistrationRevertsAndExplains() {
        let service = FakeLoginItems(.notRegistered)
        service.failure = RegistrationRefused()
        let login = LaunchAtLogin(service: service)
        login.setEnabled(true)
        XCTAssertFalse(login.isEnabled)
        XCTAssertEqual(login.errorText,
                       "Couldn’t turn on launch at login. Add Tessera in System Settings > General > Login Items.")
    }

    func testStatusChangedOutsideTheAppIsPickedUpOnRefresh() {
        let service = FakeLoginItems(.enabled)
        let login = LaunchAtLogin(service: service)
        service.status = .notRegistered
        login.refresh()
        XCTAssertFalse(login.isEnabled)
    }

    func testSuccessClearsEarlierError() {
        let service = FakeLoginItems(.notRegistered)
        service.failure = RegistrationRefused()
        let login = LaunchAtLogin(service: service)
        login.setEnabled(true)
        service.failure = nil
        login.setEnabled(true)
        XCTAssertNil(login.errorText)
        XCTAssertTrue(login.isEnabled)
    }
}
