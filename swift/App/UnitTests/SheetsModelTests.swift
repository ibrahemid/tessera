import XCTest
import TesseraCore
import TesseraArgon2
@testable import Tessera

final class SheetsPasswordRuleTests: XCTestCase {
    func testShortPasswordIsRejected() {
        XCTAssertEqual(PasswordRule.issue(password: "1234567", confirm: "1234567"), .tooShort)
    }

    func testMinimumLengthPasswordIsAccepted() {
        XCTAssertNil(PasswordRule.issue(password: "12345678", confirm: "12345678"))
    }

    func testMismatchIsReportedAfterLength() {
        XCTAssertEqual(PasswordRule.issue(password: "12345678", confirm: "12345679"), .mismatch)
        XCTAssertEqual(PasswordRule.issue(password: "12345678", confirm: ""), .mismatch)
    }

    func testNoVisibleIssueUntilConfirmationIsTyped() {
        XCTAssertNil(PasswordRule.visibleIssue(password: "abc", confirm: ""))
        XCTAssertEqual(PasswordRule.visibleIssue(password: "abc", confirm: "abc"), .tooShort)
    }

    func testIssueText() {
        XCTAssertEqual(PasswordRule.text(for: .tooShort, noun: "password"), "Use at least 8 characters.")
        XCTAssertEqual(PasswordRule.text(for: .mismatch, noun: "passphrase"), "The passphrases don’t match.")
    }
}

final class SheetsDuplicateScanTests: XCTestCase {
    private func account(_ id: String, _ issuer: String, _ name: String, secret: String = "12345678901234567890") -> Account {
        Account(id: id, type: .totp, issuer: issuer, account: name, secret: Data(secret.utf8))
    }

    private func key(_ a: Account) -> String { "\(a.issuer)|\(a.account)|\(a.secret.base64EncodedString())" }

    func testKeepsFirstCopyAndReturnsLaterOnes() {
        let list = [account("1", "GitHub", "a"), account("2", "AWS", "b"),
                    account("3", "GitHub", "a"), account("4", "GitHub", "a")]
        XCTAssertEqual(DuplicateScan.redundant(in: list, key: key).map(\.id), ["3", "4"])
    }

    func testDifferentSecretIsNotADuplicate() {
        let list = [account("1", "GitHub", "a"), account("2", "GitHub", "a", secret: "abcdefghij1234567890")]
        XCTAssertTrue(DuplicateScan.redundant(in: list, key: key).isEmpty)
    }

    func testConfirmationNamesSingleAccount() {
        let c = DuplicateScan.confirmation(for: [account("3", "GitHub", "octocat")])
        XCTAssertEqual(c.title, "Remove 1 duplicate account?")
        XCTAssertEqual(c.action, "Remove duplicate")
        XCTAssertTrue(c.message.hasPrefix("Removes the later copy of GitHub (octocat)."))
    }

    func testConfirmationCountsAccountsBeyondTheList() {
        let removed = (1...6).map { account("\($0)", "Issuer\($0)", "") }
        let c = DuplicateScan.confirmation(for: removed)
        XCTAssertEqual(c.title, "Remove 6 duplicate accounts?")
        XCTAssertEqual(c.action, "Remove 6 duplicates")
        XCTAssertTrue(c.message.contains("Issuer4"))
        XCTAssertFalse(c.message.contains("Issuer5"))
        XCTAssertTrue(c.message.contains("2 more"))
    }

    @MainActor
    func testModelPreviewMatchesRemoval() throws {
        let model = AppModel(demo: .empty)
        model.accounts = [account("1", "GitHub", "a"), account("2", "GitHub", "A")]
        XCTAssertEqual(model.duplicatesToRemove.map(\.id), ["2"])
    }
}

final class SheetsResetConfirmationTests: XCTestCase {
    func testExternalVaultDisconnectsWithoutDeleting() {
        let c = ResetConfirmation(isExternal: true, vaultPath: "~/vault.json", accountCount: 3, lastBackup: nil)
        XCTAssertEqual(c.action, "Disconnect vault")
        XCTAssertTrue(c.message.contains("~/vault.json"))
        XCTAssertTrue(c.message.contains("isn’t deleted"))
    }

    func testBuiltInVaultNamesWhatIsDeletedAndMissingBackup() {
        let c = ResetConfirmation(isExternal: false, vaultPath: "~/Library/vault.json", accountCount: 2, lastBackup: nil)
        XCTAssertEqual(c.title, "Reset Tessera?")
        XCTAssertEqual(c.action, "Delete vault")
        XCTAssertTrue(c.message.hasPrefix("Deletes 2 accounts, the vault file at ~/Library/vault.json and its key in this Mac’s Keychain."))
        XCTAssertTrue(c.message.contains("No backup of this vault is on record. Export one first"))
    }

    func testLockedVaultWithoutBackupSaysAccountsCannotBeRecovered() {
        let c = ResetConfirmation(isExternal: false, vaultPath: "v.json", accountCount: nil, lastBackup: nil)
        XCTAssertTrue(c.message.hasPrefix("Deletes the vault file at v.json"))
        XCTAssertTrue(c.message.hasSuffix("Without one, its accounts can’t be recovered."))
    }

    func testRecordedBackupIsNamed() {
        let date = Date(timeIntervalSince1970: 1_800_000_000)
        let c = ResetConfirmation(isExternal: false, vaultPath: "v.json", accountCount: 1, lastBackup: date)
        XCTAssertTrue(c.message.contains("Deletes 1 account,"))
        XCTAssertTrue(c.message.contains("Last backup exported \(date.formatted(date: .abbreviated, time: .shortened))."))
    }
}

final class SheetsBackupLedgerTests: XCTestCase {
    private var suite = ""
    private var defaults: UserDefaults!

    override func setUp() {
        super.setUp()
        suite = "tessera.tests.\(UUID().uuidString)"
        defaults = UserDefaults(suiteName: suite)
    }

    override func tearDown() {
        defaults.removePersistentDomain(forName: suite)
        super.tearDown()
    }

    func testRecordsAndForgetsPerVaultPath() {
        let ledger = BackupLedger(defaults: defaults)
        XCTAssertNil(ledger.lastExport(forVaultAt: "/a.json"))
        let when = Date(timeIntervalSince1970: 1_800_000_000)
        ledger.recordExport(forVaultAt: "/a.json", at: when)
        XCTAssertEqual(ledger.lastExport(forVaultAt: "/a.json"), when)
        XCTAssertNil(ledger.lastExport(forVaultAt: "/b.json"))
        ledger.forget(vaultAt: "/a.json")
        XCTAssertNil(ledger.lastExport(forVaultAt: "/a.json"))
    }
}

final class SheetsBackupFileTests: XCTestCase {
    private let sample = Account(id: "1", type: .totp, issuer: "GitHub", account: "a",
                                 secret: Data("12345678901234567890".utf8))

    func testRejectsNonBackupData() {
        XCTAssertEqual(BackupFile.problem(in: Data("{\"accounts\":[]}".utf8)),
                       "That file isn’t a Tessera backup. Choose a file made with Export encrypted backup.")
    }

    func testRejectsEnvelopeWithoutPasswordWrap() throws {
        let data = try Envelope.createUnwrapped(accounts: [sample]).0.encoded()
        XCTAssertNotNil(BackupFile.problem(in: data))
    }

    func testAcceptsPasswordWrappedBackup() throws {
        let data = try Envelope.create(accounts: [sample], passphrase: "correct-horse", argon2: Argon2Reference()).encoded()
        XCTAssertNil(BackupFile.problem(in: data))
    }

    func testExportWriteFailureSaysWhatToDo() {
        let url = URL(fileURLWithPath: "/tmp/tessera-backup.json")
        let export = UserMessage.text(for: CocoaError(.fileWriteNoPermission), context: .export, url: url)
        XCTAssertTrue(export.hasPrefix("Couldn’t save to tessera-backup.json:"))
        XCTAssertTrue(export.hasSuffix(" Choose another folder."))
        let save = UserMessage.text(for: CocoaError(.fileWriteNoPermission), context: .save, url: url)
        XCTAssertFalse(save.hasSuffix(" Choose another folder."))
    }

    func testRestoreOutcomeLines() {
        XCTAssertEqual(BackupRestoreOutcome.restored(1).line, "Restored 1 account.")
        XCTAssertEqual(BackupRestoreOutcome.restored(3).line, "Restored 3 accounts.")
        XCTAssertEqual(BackupRestoreOutcome.nothingNew(1).line, "The account in this backup is already in your vault.")
        XCTAssertEqual(BackupRestoreOutcome.nothingNew(4).line, "All 4 accounts in this backup are already in your vault.")
        XCTAssertNil(BackupRestoreOutcome.failed("x").line)
    }
}

final class SheetsSettingsTabHeightTests: XCTestCase {
    func testTabGrowsByEachVisibleResultLine() {
        XCTAssertEqual(SettingsTabHeight.height(base: 380, showing: []), 380)
        XCTAssertEqual(SettingsTabHeight.height(base: 380, showing: [.info("a"), .info("b")]),
                       380 + 2 * SettingsTabHeight.infoLine)
        XCTAssertEqual(SettingsTabHeight.height(base: 380, showing: [.info("a"), .failure("b")]),
                       380 + SettingsTabHeight.infoLine + SettingsTabHeight.failureLine)
    }

    func testFailureLineGetsRoomForWrappedText() {
        XCTAssertGreaterThan(SettingsTabHeight.failureLine, SettingsTabHeight.infoLine)
    }
}

final class SheetsVaultReachTests: XCTestCase {
    @MainActor
    func testVaultWithoutRecoveryPassphraseOpensOnThisMacOnly() {
        XCTAssertTrue(AppModel(demo: .populated).opensOnThisMacOnly)
    }

    @MainActor
    func testLockedVaultMakesNoClaim() {
        XCTAssertFalse(AppModel(demo: .locked).opensOnThisMacOnly)
    }
}
