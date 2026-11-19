import AppKit
import XCTest
@testable import Tessera

@MainActor
final class VaultLaunchTests: XCTestCase {
    func testXCTestEnvironmentIsDetected() {
        XCTAssertTrue(AppLaunch.isUnitTestHost(environment: ["XCTestConfigurationFilePath": "/x.xctestconfiguration"],
                                               arguments: ["Tessera"]))
        XCTAssertTrue(AppLaunch.isUnitTestHost(environment: ["XCTestSessionIdentifier": "1"], arguments: ["Tessera"]))
        XCTAssertTrue(AppLaunch.isUnitTestHost(environment: ["XCTestBundlePath": "/t.xctest"], arguments: ["Tessera"]))
    }

    func testNormalLaunchRunsTheApp() {
        XCTAssertFalse(AppLaunch.isUnitTestHost(environment: [:], arguments: ["Tessera"]))
        XCTAssertEqual(AppLaunch.mode(environment: [:], arguments: ["Tessera"]), .app)
    }

    func testUITestLaunchRunsTheApp() {
        let env = ["XCTestConfigurationFilePath": "/x.xctestconfiguration"]
        XCTAssertFalse(AppLaunch.isUnitTestHost(environment: env, arguments: ["Tessera", "-uitest"]))
        XCTAssertEqual(AppLaunch.mode(environment: env, arguments: ["Tessera", "-uitest"]), .app)
    }

    func testThisHostLaunchedInert() {
        XCTAssertTrue(AppLaunch.isUnitTestHost())
        XCTAssertEqual(AppLaunch.current, .inertTestHost)
        XCTAssertTrue(NSApp.windows.filter(\.isVisible).isEmpty)
    }
}
