import XCTest

/// Drives the real windowed app through the flows that were broken in the
/// status-bar-only version: the window opens, onboarding + recovery work, the
/// Add sheet opens without dismissing the app, import works, and Settings opens.
final class FlowUITests: XCTestCase {
    func testOnboardingAddAndSettings() throws {
        let app = XCUIApplication()
        app.launchArguments += ["-uitest"]
        app.launchEnvironment["TESSERA_VAULT"] =
            NSTemporaryDirectory() + "tessera-uitest-\(UUID().uuidString)/vault.json"
        app.launch()

        // A real window opens with onboarding.
        let create = app.buttons["Create vault"]
        XCTAssertTrue(create.waitForExistence(timeout: 15), "onboarding window did not appear")
        create.click()

        // Recovery key screen — must save before continuing.
        let cont = app.buttons["Continue"]
        XCTAssertTrue(cont.waitForExistence(timeout: 8), "recovery key screen missing")
        let toggle = app.checkBoxes.firstMatch
        XCTAssertTrue(toggle.waitForExistence(timeout: 5))
        toggle.click()
        cont.click()

        // Vault, empty state.
        let addAccount = app.buttons["Add account"]
        XCTAssertTrue(addAccount.waitForExistence(timeout: 8), "vault empty state missing")
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
        let settingsShown = app.staticTexts["Theme"].waitForExistence(timeout: 8)
            || app.checkBoxes["Show in menu bar"].waitForExistence(timeout: 3)
        XCTAssertTrue(settingsShown, "settings did not open")
    }
}
