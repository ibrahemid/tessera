import XCTest

/// Drives the real windowed app: first launch opens straight into an empty vault
/// with no prompts, the Add sheet opens without dismissing the app, import works,
/// and Settings opens.
final class FlowUITests: XCTestCase {
    func testLaunchAddAndSettings() throws {
        let app = XCUIApplication()
        app.launchArguments += ["-uitest"]
        app.launchEnvironment["TESSERA_VAULT"] =
            NSTemporaryDirectory() + "tessera-uitest-\(UUID().uuidString)/vault.json"
        app.launch()

        // First launch goes straight to the empty vault (no onboarding, no prompts).
        let addAccount = app.buttons["Add account"]
        XCTAssertTrue(addAccount.waitForExistence(timeout: 15), "vault empty state did not appear")
        addAccount.click()

        // Add sheet appears AND the app stays alive (the old dismiss bug).
        let field = app.textViews.firstMatch
        XCTAssertTrue(field.waitForExistence(timeout: 8), "add sheet did not open")
        XCTAssertGreaterThanOrEqual(app.windows.count, 1, "app window vanished when opening Add")
        field.click()
        field.typeText("otpauth://totp/GitHub:me?secret=JBSWY3DPEHPK3PXP&issuer=GitHub")
        app.buttons["Add"].click()

        // Imported row shows.
        XCTAssertTrue(app.staticTexts["GitHub"].waitForExistence(timeout: 8), "added account not shown")

        // Settings opens in its own window without killing the app.
        app.typeKey(",", modifierFlags: .command)
        XCTAssertTrue(app.staticTexts["Theme"].waitForExistence(timeout: 8), "settings did not open")
    }
}
