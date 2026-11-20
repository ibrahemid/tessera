import XCTest

/// Settings opens as a tabbed window, and the reset confirmation names what it
/// deletes and leaves Cancel as the default button.
final class SheetsUITests: XCTestCase {
    private var vaultURL: URL!

    override func setUpWithError() throws {
        try super.setUpWithError()
        continueAfterFailure = false
        let base = ProcessInfo.processInfo.environment["TESSERA_UITEST_DIR"] ?? NSTemporaryDirectory()
        vaultURL = URL(fileURLWithPath: base)
            .appendingPathComponent("tessera-uitest-\(UUID().uuidString)")
            .appendingPathComponent("vault.json")
    }

    @MainActor
    private func launch() -> XCUIApplication {
        let app = XCUIApplication()
        app.launchArguments += ["-uitest"]
        app.launchEnvironment["TESSERA_VAULT"] = vaultURL.path
        app.launch()
        return app
    }

    @MainActor
    private func openSettings(_ app: XCUIApplication) {
        app.typeKey(",", modifierFlags: .command)
        XCTAssertTrue(app.staticTexts["Theme"].waitForExistence(timeout: 8), "settings did not open")
    }

    @MainActor
    func testSettingsTabs() throws {
        let app = launch()
        openSettings(app)

        let expected: [(tab: String, element: XCUIElement)] = [
            ("Security", app.staticTexts["Recovery passphrase"]),
            ("Vault", app.buttons["Reset Tessera…"]),
            ("Backup", app.buttons["Export encrypted backup…"]),
            ("General", app.staticTexts["Theme"]),
        ]
        for (tab, element) in expected {
            let button = app.toolbars.buttons[tab]
            XCTAssertTrue(button.waitForExistence(timeout: 5), "no \(tab) tab")
            button.click()
            XCTAssertTrue(element.waitForExistence(timeout: 5), "\(tab) tab content did not show")
        }
    }

    @MainActor
    func testResetConfirmationDefaultsToCancel() throws {
        let app = launch()
        XCTAssertTrue(app.buttons["Add account"].firstMatch.waitForExistence(timeout: 15), "vault did not open")
        openSettings(app)
        app.toolbars.buttons["Vault"].click()

        app.buttons["Reset Tessera…"].click()
        let delete = app.buttons["Delete vault"]
        XCTAssertTrue(delete.waitForExistence(timeout: 5), "reset confirmation did not show")
        XCTAssertTrue(app.buttons["Cancel"].exists)

        app.typeKey(.return, modifierFlags: [])
        XCTAssertTrue(delete.waitForNonExistence(timeout: 5), "Return did not dismiss the confirmation")
        XCTAssertTrue(app.buttons["Reset Tessera…"].exists)
        XCTAssertTrue(FileManager.default.fileExists(atPath: vaultURL.path), "Return reset the vault")
    }
}
