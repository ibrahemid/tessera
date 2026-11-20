import XCTest
@testable import Tessera
import TesseraCore

final class FeaturesTextCleaningTests: XCTestCase {
    func testControlAndDirectionCharactersAreRemoved() {
        XCTAssertEqual(TextCleaning.clean("a\u{0}b\u{7}c\u{7F}d\u{85}e"), "abcde")
        XCTAssertEqual(TextCleaning.clean("\u{202E}moc.liame\u{202C}"), "moc.liame")
        XCTAssertEqual(TextCleaning.clean("\u{2066}x\u{2069}\u{FEFF}"), "x")
    }

    func testOuterWhitespaceIsTrimmedAndInnerKept() {
        XCTAssertEqual(TextCleaning.clean("  two words \n"), "two words")
        XCTAssertEqual(TextCleaning.clean("\u{FEFF}  \u{202A}"), "")
        XCTAssertEqual(TextCleaning.clean("Café"), "Café")
    }
}

final class FeaturesLinkIntakeTests: XCTestCase {
    private let otpauth = "otpauth://totp/Example:alice@example.com?secret=JBSWY3DPEHPK3PXP&issuer=Example"
    private let migration = "otpauth-migration://offline?data=CjEKCkhlbGxvId6tvu8SGEV4YW1wbGU6YWxpY2VAZ29vZ2xlLmNvbRoHRXhhbXBsZSABKAEwAhABGAEgACjr4JKkBg%3D%3D"

    private func url(_ s: String) throws -> URL { try XCTUnwrap(URL(string: s)) }

    func testOtpauthAndMigrationLinksPrefillTheSheet() throws {
        XCTAssertEqual(LinkIntake.prefill(for: [try url(otpauth)])?.text, otpauth)
        XCTAssertEqual(LinkIntake.prefill(for: [try url(migration)])?.text, migration)
    }

    func testSeveralLinksArePrefilledOnePerLine() throws {
        let prefill = LinkIntake.prefill(for: [try url(otpauth), try url("https://example.com"), try url(migration)])
        XCTAssertEqual(prefill?.text, otpauth + "\n" + migration)
    }

    func testOtherSchemesAreIgnored() throws {
        XCTAssertNil(LinkIntake.prefill(for: [try url("https://example.com/otpauth://totp/x")]))
        XCTAssertNil(LinkIntake.prefill(for: [try url("file:///tmp/otpauth")]))
        XCTAssertNil(LinkIntake.prefill(for: []))
    }

    @MainActor
    func testReceivedLinkWaitsForTheUser() throws {
        let model = AppModel(demo: .populated)
        let before = model.accounts
        XCTAssertTrue(model.receiveLinks([try url(otpauth)]))
        XCTAssertEqual(model.pendingLink?.text, otpauth)
        XCTAssertEqual(model.accounts, before)
    }

    @MainActor
    func testUnrelatedLinkLeavesNothingPending() throws {
        let model = AppModel(demo: .populated)
        XCTAssertFalse(model.receiveLinks([try url("https://example.com")]))
        XCTAssertNil(model.pendingLink)
    }

    @MainActor
    func testAdvancingHOTPAtTheMaximumCounterDoesNotWrap() {
        let model = AppModel(demo: .populated)
        var account = VaultFixture.account("h", issuer: "Bank", type: .hotp, period: 0)
        account.counter = Account.maxCounter
        model.accounts = [account]

        model.advanceHOTP(account)

        XCTAssertEqual(model.accounts.first?.counter, Account.maxCounter)
        guard case .failure = model.notice else { return XCTFail("no failure notice") }
    }

    @MainActor
    func testLinkReceivedWhileLockedStaysPendingAndLocked() throws {
        let model = AppModel(demo: .locked)
        XCTAssertTrue(model.receiveLinks([try url(otpauth)]))
        XCTAssertNotNil(model.pendingLink)
        XCTAssertTrue(model.isLocked)
    }
}

final class FeaturesTagTests: XCTestCase {
    private let accounts = [
        VaultFixture.account("1", issuer: "GitHub", tags: ["dev", "work"]),
        VaultFixture.account("2", issuer: "AWS", tags: ["work", "work"]),
        VaultFixture.account("3", issuer: "Bank"),
    ]

    func testTagScopeShowsOnlyTaggedAccounts() {
        XCTAssertEqual(VaultFixture.ids(VaultList.visible(accounts, scope: .tag("work"), query: "")), ["1", "2"])
        XCTAssertEqual(VaultFixture.ids(VaultList.visible(accounts, scope: .tag("dev"), query: "")), ["1"])
        XCTAssertEqual(VaultFixture.ids(VaultList.visible(accounts, scope: .tag("work"), query: "aws")), ["2"])
        XCTAssertTrue(VaultList.visible(accounts, scope: .tag("none"), query: "").isEmpty)
    }

    func testTagCountsCountEachAccountOnce() {
        XCTAssertEqual(VaultList.counts(accounts).tags, ["dev": 1, "work": 2])
    }

    func testTagsAreListedInNaturalOrder() {
        let tagged = [VaultFixture.account("1", tags: ["b", "item10"]), VaultFixture.account("2", tags: ["A", "item2", "b"])]
        XCTAssertEqual(VaultList.tags(in: tagged), ["A", "b", "item2", "item10"])
        XCTAssertEqual(VaultList.tags(in: []), [])
    }

    func testTagTextIsCleaned() {
        XCTAssertEqual(VaultEdit.normalizedTag("  #dev "), "dev")
        XCTAssertEqual(VaultEdit.normalizedTag("\u{202E}ops\u{0}"), "ops")
        XCTAssertEqual(VaultEdit.normalizedTag("two words"), "two words")
        XCTAssertNil(VaultEdit.normalizedTag("#"))
        XCTAssertNil(VaultEdit.normalizedTag(" \u{FEFF} "))
    }

    func testEditedTagsDropBlanksAndDuplicates() {
        XCTAssertEqual(TagDraft.normalized(["#dev", " dev ", "Ops", "", "\u{2066}x\u{2069}"]), ["dev", "Ops", "x"])
    }

    func testAddingATag() {
        XCTAssertEqual(TagDraft.adding("#new", to: ["dev"]), ["dev", "new"])
        XCTAssertEqual(TagDraft.adding(" dev", to: ["dev"]), ["dev"])
        XCTAssertEqual(TagDraft.adding("  ", to: ["dev"]), ["dev"])
    }
}

final class FeaturesParameterEditTests: XCTestCase {
    private let setupKey = "JBSWY3DPEHPK3PXP"
    private let setupKeyBytes = Data("Hello!".utf8) + Data([0xDE, 0xAD, 0xBE, 0xEF])
    private let link = "otpauth://totp/Example:alice@example.com?secret=JBSWY3DPEHPK3PXP&issuer=Example&digits=8&period=60&algorithm=SHA256"

    private func totp() -> Account { VaultFixture.account("t", issuer: "GitHub") }

    private func hotp() -> Account {
        var a = VaultFixture.account("h", issuer: "Bank", type: .hotp, period: 0)
        a.counter = 4
        return a
    }

    func testSecretInputReadsKeysAndLinks() throws {
        XCTAssertEqual(ParameterEdit.secretInput(""), .keep)
        XCTAssertEqual(ParameterEdit.secretInput(" \n"), .keep)
        XCTAssertEqual(ParameterEdit.secretInput(setupKey), .key(setupKeyBytes))
        XCTAssertEqual(ParameterEdit.secretInput("\u{202E}\(setupKey)\u{FEFF}"), .key(setupKeyBytes))
        XCTAssertEqual(ParameterEdit.secretInput(link), .link(try OTPAuth.parse(link)))
    }

    func testSecretInputRejectsOtherText() {
        XCTAssertEqual(ParameterEdit.secretInput("not a key!"), .invalid)
        XCTAssertEqual(ParameterEdit.secretInput("ABC"), .invalid)
        XCTAssertEqual(ParameterEdit.secretInput("otpauth://totp/x"), .invalid)
        XCTAssertEqual(ParameterEdit.secretInput("otpauth-migration://offline?data=AA%3D%3D"), .invalid)
        XCTAssertEqual(ParameterEdit.secretInput("[]"), .invalid)
    }

    func testUnchangedDraftKeepsTheAccount() throws {
        let a = totp()
        XCTAssertEqual(try ParameterEdit.updated(a, with: ParameterDraft(a)), a)
        XCTAssertFalse(ParameterEdit.changesCodes(a, draft: ParameterDraft(a)))
        XCTAssertFalse(ParameterEdit.needsConfirmation(a, draft: ParameterDraft(a)))
    }

    func testDigitsOutsideTheRangeAreRejected() throws {
        let a = totp()
        var draft = ParameterDraft(a)
        draft.digits = 9
        XCTAssertThrowsError(try ParameterEdit.updated(a, with: draft))
        XCTAssertTrue(ParameterEdit.changesCodes(a, draft: draft))
        draft.digits = 8
        XCTAssertEqual(try ParameterEdit.updated(a, with: draft).digits, 8)
    }

    func testInvalidSecretIsRejected() {
        let a = totp()
        var draft = ParameterDraft(a)
        draft.secretText = "not a key!"
        XCTAssertThrowsError(try ParameterEdit.updated(a, with: draft)) { error in
            XCTAssertEqual(error as? ParameterEditError, .invalidSecret)
        }
    }

    func testReplacingTheSecretFromAKeyOrALink() throws {
        let a = totp()
        var draft = ParameterDraft(a)
        draft.secretText = setupKey
        XCTAssertEqual(try ParameterEdit.updated(a, with: draft).secret, setupKeyBytes)
        draft.secretText = link
        let updated = try ParameterEdit.updated(a, with: draft)
        XCTAssertEqual(updated.secret, setupKeyBytes)
        XCTAssertEqual(updated.digits, a.digits)
        XCTAssertEqual(updated.issuer, a.issuer)
    }

    func testSteamForcesItsDigitsAndPeriod() throws {
        let a = totp()
        var draft = ParameterDraft(a)
        draft.type = .steam
        draft.digits = 8
        draft.period = 60
        let updated = try ParameterEdit.updated(a, with: draft)
        XCTAssertEqual(updated.type, .steam)
        XCTAssertEqual(updated.digits, 5)
        XCTAssertEqual(updated.period, 30)
    }

    func testCounterChangesOnlyForHOTP() throws {
        let h = hotp()
        var draft = ParameterDraft(h)
        draft.counter = 9
        XCTAssertEqual(try ParameterEdit.updated(h, with: draft).counter, 9)
        XCTAssertTrue(ParameterEdit.changesCodes(h, draft: draft))
        XCTAssertFalse(ParameterEdit.needsConfirmation(h, draft: draft))
        var t = totp()
        t.counter = 2
        var totpDraft = ParameterDraft(t)
        totpDraft.counter = 7
        XCTAssertEqual(try ParameterEdit.updated(t, with: totpDraft).counter, 2)
        XCTAssertFalse(ParameterEdit.changesCodes(t, draft: totpDraft))
    }

    func testNegativeCounterIsRejected() {
        let h = hotp()
        var draft = ParameterDraft(h)
        draft.counter = -1
        XCTAssertThrowsError(try ParameterEdit.updated(h, with: draft))
    }

    func testOnlyATypeOrSecretChangeNeedsConfirmation() {
        let a = totp()
        var draft = ParameterDraft(a)
        draft.digits = 8
        draft.algorithm = "SHA256"
        draft.period = 60
        XCTAssertTrue(ParameterEdit.changesCodes(a, draft: draft))
        XCTAssertFalse(ParameterEdit.needsConfirmation(a, draft: draft))
        var typed = ParameterDraft(a)
        typed.setType(.hotp)
        XCTAssertTrue(ParameterEdit.needsConfirmation(a, draft: typed))
        var keyed = ParameterDraft(a)
        keyed.secretText = setupKey
        XCTAssertTrue(ParameterEdit.needsConfirmation(a, draft: keyed))
    }

    func testConfirmationNamesTheAccount() {
        XCTAssertEqual(ParameterEdit.confirmationTitle(for: totp()), "Change the codes for GitHub?")
    }

    func testSwitchingToSteamSetsItsParameters() {
        var draft = ParameterDraft(totp())
        draft.algorithm = "SHA512"
        draft.setType(.steam)
        XCTAssertEqual(draft.type, .steam)
        XCTAssertEqual(draft.digits, 5)
        XCTAssertEqual(draft.period, 30)
        XCTAssertEqual(draft.algorithm, "SHA1")
        draft.setType(.totp)
        XCTAssertEqual(draft.digits, 6)
    }

    func testSwitchingFromHOTPGivesAPeriod() {
        var draft = ParameterDraft(hotp())
        XCTAssertEqual(draft.period, 0)
        draft.setType(.totp)
        XCTAssertEqual(draft.type, .totp)
        XCTAssertEqual(draft.period, 30)
        draft.digits = 8
        draft.setType(.totp)
        XCTAssertEqual(draft.digits, 8)
    }

    func testAdoptingALinkCopiesItsParameters() throws {
        var draft = ParameterDraft(totp())
        draft.adopt(try OTPAuth.parse(link))
        XCTAssertEqual(draft.digits, 8)
        XCTAssertEqual(draft.period, 60)
        XCTAssertEqual(draft.algorithm, "SHA256")
        XCTAssertEqual(draft.type, .totp)
    }
}
