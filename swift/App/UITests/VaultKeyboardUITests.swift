import XCTest

@MainActor
final class VaultKeyboardUITests: XCTestCase {
    private static let accounts = [
        ("GitHub", "otpauth://totp/GitHub:me?secret=JBSWY3DPEHPK3PXP&issuer=GitHub"),
        ("Cloudflare", "otpauth://totp/Cloudflare:ops?secret=GEZDGNBVGY3TQOJQ&issuer=Cloudflare"),
    ]

    private func launchSeeded() -> XCUIApplication {
        continueAfterFailure = false
        let app = XCUIApplication()
        let root = ProcessInfo.processInfo.environment["TESSERA_UITEST_DIR"] ?? NSTemporaryDirectory()
        app.launchArguments += ["-uitest", "-tessera.compact", "NO", "-ApplePersistenceIgnoreState", "YES"]
        app.launchEnvironment["TESSERA_VAULT"] = (root as NSString)
            .appendingPathComponent("tessera-uitest-\(UUID().uuidString)/vault.json")
        app.launch()

        let empty = app.buttons["Add account"].firstMatch
        XCTAssertTrue(empty.waitForExistence(timeout: 15), "vault empty state did not appear")
        for (index, (name, uri)) in Self.accounts.enumerated() {
            if index == 0 { empty.click() } else { app.typeKey("n", modifierFlags: .command) }
            let field = app.textViews.firstMatch
            XCTAssertTrue(field.waitForExistence(timeout: 8), "add sheet did not open")
            field.click()
            field.typeText(uri)
            app.buttons["Add"].click()
            XCTAssertTrue(row(app, named: name).waitForExistence(timeout: 8), "\(name) was not added")
        }
        XCTAssertTrue(search(app).waitForExistence(timeout: 5))
        waitForNoticeToClear(app)
        return app
    }

    private func rows(_ app: XCUIApplication) -> XCUIElementQuery {
        app.buttons.matching(identifier: "AccountRow")
    }

    private func row(_ app: XCUIApplication, named name: String) -> XCUIElement {
        rows(app).matching(NSPredicate(format: "label BEGINSWITH %@", name)).firstMatch
    }

    private func search(_ app: XCUIApplication) -> XCUIElement {
        app.textFields["VaultSearch"]
    }

    private func notice(_ app: XCUIApplication, containing text: String) -> XCUIElement {
        app.descendants(matching: .any)
            .matching(NSPredicate(format: "label BEGINSWITH 'Copied' AND label CONTAINS %@", text)).firstMatch
    }

    private func waitForNoticeToClear(_ app: XCUIApplication) {
        let anyNotice = app.descendants(matching: .any).matching(NSPredicate(format: "label BEGINSWITH 'Added'")).firstMatch
        let gone = XCTNSPredicateExpectation(predicate: NSPredicate(format: "exists == false"), object: anyNotice)
        _ = XCTWaiter.wait(for: [gone], timeout: 8)
    }

    private func name(of element: XCUIElement) -> String {
        String(element.label.split(separator: ",").first ?? "")
    }

    private func hiddenCount(_ app: XCUIApplication) -> Int {
        rows(app).allElementsBoundByIndex.filter { ($0.value as? String) == "Code hidden" }.count
    }

    func testArrowKeysMoveTheSelectionAndReturnCopies() {
        let app = launchSeeded()
        XCTAssertEqual(rows(app).count, 2)

        app.typeKey(.downArrow, modifierFlags: [])
        XCTAssertTrue(rows(app).element(boundBy: 0).isSelected, "first row not selected")
        app.typeKey(.downArrow, modifierFlags: [])
        let second = rows(app).element(boundBy: 1)
        XCTAssertTrue(second.isSelected, "selection did not move down")
        XCTAssertFalse(rows(app).element(boundBy: 0).isSelected)

        app.typeKey(.return, modifierFlags: [])
        XCTAssertTrue(notice(app, containing: name(of: second)).waitForExistence(timeout: 3), "Return did not copy")

        app.typeKey(.upArrow, modifierFlags: [])
        XCTAssertTrue(rows(app).element(boundBy: 0).isSelected, "selection did not move up")
    }

    func testTypingFiltersAndEscapeClearsSearchThenSelection() {
        let app = launchSeeded()
        app.outlines.staticTexts["All"].click()
        app.typeKey("c", modifierFlags: [])
        app.typeKey("l", modifierFlags: [])
        XCTAssertEqual(search(app).value as? String, "cl", "typing outside the field did not search")
        XCTAssertEqual(rows(app).count, 1)
        XCTAssertTrue(row(app, named: "Cloudflare").isSelected, "first match not selected")

        app.typeKey(.escape, modifierFlags: [])
        XCTAssertEqual(rows(app).count, 2, "Escape did not clear the search")
        XCTAssertTrue(row(app, named: "Cloudflare").isSelected, "first Escape cleared the selection")

        app.typeKey(.escape, modifierFlags: [])
        XCTAssertFalse(rows(app).allElementsBoundByIndex.contains { $0.isSelected }, "second Escape kept the selection")
    }

    func testAccountMenuCopiesTheSelectedCode() {
        let app = launchSeeded()
        app.typeKey(.downArrow, modifierFlags: [])
        let first = name(of: rows(app).element(boundBy: 0))

        app.menuBars.menuBarItems["Account"].click()
        app.menuItems["Copy Code"].click()
        XCTAssertTrue(notice(app, containing: first).waitForExistence(timeout: 3), "Account > Copy Code did not copy")

        waitForCopiedNoticeToClear(app, first)
        app.typeKey("c", modifierFlags: .command)
        XCTAssertTrue(notice(app, containing: first).waitForExistence(timeout: 3), "Command-C did not copy the code")
    }

    private func waitForCopiedNoticeToClear(_ app: XCUIApplication, _ name: String) {
        let gone = XCTNSPredicateExpectation(predicate: NSPredicate(format: "exists == false"),
                                             object: notice(app, containing: name))
        XCTAssertEqual(XCTWaiter.wait(for: [gone], timeout: 6), .completed)
    }

    func testMenuBarCarriesTheVaultCommands() {
        let app = launchSeeded()
        let bar = app.menuBars.firstMatch
        for title in ["File", "Edit", "View", "Account", "Window", "Help"] {
            XCTAssertTrue(bar.menuBarItems[title].exists, "\(title) menu missing")
        }
        let expected = [
            "File": ["Add Account…", "Scan Screen for QR Code", "Import from Files…", "Export Encrypted Backup…", "Lock Vault"],
            "Edit": ["Find…"],
            "View": ["Hide Codes", "Window Layout", "Compact Layout"],
            "Account": ["Copy Code", "Edit…", "Pin", "Move to Folder", "Add Tag", "Delete"],
            "Help": ["Tessera Help"],
        ]
        for (menu, items) in expected {
            bar.menuBarItems[menu].click()
            for item in items {
                XCTAssertTrue(bar.menuBarItems[menu].menuItems[item].waitForExistence(timeout: 2), "\(menu) > \(item) missing")
            }
            app.typeKey(.escape, modifierFlags: [])
        }
    }

    func testHideCodesToggleMasksAndRevealsCodes() {
        let app = launchSeeded()
        let before = hiddenCount(app)

        app.typeKey("h", modifierFlags: [.command, .shift])
        let flipped = hiddenCount(app)
        XCTAssertNotEqual(flipped > 0, before > 0, "Hide Codes shortcut did not change the rows")

        app.menuBars.menuBarItems["View"].click()
        app.menuItems["Hide Codes"].click()
        XCTAssertEqual(hiddenCount(app) > 0, before > 0, "View > Hide Codes did not restore the rows")

        app.typeKey(.downArrow, modifierFlags: [])
        if before == 0 {
            app.typeKey("h", modifierFlags: [.command, .shift])
            XCTAssertNotEqual(rows(app).element(boundBy: 0).value as? String, "Code hidden", "selected row stayed masked")
            app.typeKey("h", modifierFlags: [.command, .shift])
        }
    }
}
