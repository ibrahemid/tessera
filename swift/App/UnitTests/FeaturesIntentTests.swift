import XCTest
@testable import Tessera
import TesseraCore

@MainActor
final class FeaturesIntentLog {
    var events: [String] = []
}

@MainActor
struct FeaturesAuthenticator: IntentAuthenticating {
    let log: FeaturesIntentLog
    var failure: CopyCodeError?

    func authenticate(reason: String) async throws {
        log.events.append("auth:\(reason)")
        if let failure { throw failure }
    }
}

@MainActor
final class FeaturesCopyTarget: CodeCopyTarget {
    let log: FeaturesIntentLog
    var isLocked: Bool
    var opens: Bool
    var copies = true
    var accounts: [Account]

    init(log: FeaturesIntentLog, locked: Bool = false, opens: Bool = true, accounts: [Account]) {
        self.log = log
        isLocked = locked
        self.opens = opens
        self.accounts = accounts
    }

    func openForIntent() async -> Bool {
        log.events.append("open")
        if opens { isLocked = false }
        return opens
    }

    func account(withID id: String) -> Account? { accounts.first { $0.id == id } }

    func copyCodeForIntent(_ account: Account) -> Bool {
        log.events.append("copy:\(account.id)")
        return copies
    }
}

@MainActor
final class FeaturesCopyCodeGateTests: XCTestCase {
    private let github = VaultFixture.account("g", issuer: "GitHub")

    private func expectFailure(_ expected: CopyCodeError, target: FeaturesCopyTarget,
                               authenticator: FeaturesAuthenticator, id: String = "g") async {
        do {
            _ = try await CopyCodeGate.run(accountID: id, target: target, authenticator: authenticator)
            XCTFail("expected \(expected)")
        } catch {
            XCTAssertEqual(error as? CopyCodeError, expected)
        }
    }

    func testCopiesOnceAfterAuthenticationAndReturnsOnlyTheName() async throws {
        let log = FeaturesIntentLog()
        let target = FeaturesCopyTarget(log: log, accounts: [github])
        let name = try await CopyCodeGate.run(accountID: "g", target: target,
                                              authenticator: FeaturesAuthenticator(log: log))
        XCTAssertEqual(name, "GitHub")
        XCTAssertEqual(log.events, ["auth:copy a code", "copy:g"])
    }

    func testAuthenticationRunsEvenWhenTheVaultIsOpen() async throws {
        let log = FeaturesIntentLog()
        let target = FeaturesCopyTarget(log: log, locked: false, accounts: [github])
        await expectFailure(.notAuthenticated, target: target,
                            authenticator: FeaturesAuthenticator(log: log, failure: .notAuthenticated))
        XCTAssertEqual(log.events, ["auth:copy a code"])
    }

    func testUnavailableAuthenticationCopiesNothing() async {
        let log = FeaturesIntentLog()
        let target = FeaturesCopyTarget(log: log, locked: true, accounts: [github])
        await expectFailure(.authenticationUnavailable, target: target,
                            authenticator: FeaturesAuthenticator(log: log, failure: .authenticationUnavailable))
        XCTAssertEqual(log.events, ["auth:copy a code"])
        XCTAssertTrue(target.isLocked)
    }

    func testLockedVaultOpensAfterAuthentication() async throws {
        let log = FeaturesIntentLog()
        let target = FeaturesCopyTarget(log: log, locked: true, accounts: [github])
        _ = try await CopyCodeGate.run(accountID: "g", target: target, authenticator: FeaturesAuthenticator(log: log))
        XCTAssertEqual(log.events, ["auth:copy a code", "open", "copy:g"])
    }

    func testVaultThatCannotOpenSaysSo() async {
        let log = FeaturesIntentLog()
        let target = FeaturesCopyTarget(log: log, locked: true, opens: false, accounts: [github])
        await expectFailure(.vaultUnavailable, target: target, authenticator: FeaturesAuthenticator(log: log))
        XCTAssertEqual(log.events, ["auth:copy a code", "open"])
    }

    func testMissingAccountCopiesNothing() async {
        let log = FeaturesIntentLog()
        let target = FeaturesCopyTarget(log: log, accounts: [github])
        await expectFailure(.accountMissing, target: target, authenticator: FeaturesAuthenticator(log: log), id: "x")
        XCTAssertEqual(log.events, ["auth:copy a code"])
    }

    func testFailedCopyIsReported() async {
        let log = FeaturesIntentLog()
        let target = FeaturesCopyTarget(log: log, accounts: [github])
        target.copies = false
        await expectFailure(.copyFailed, target: target, authenticator: FeaturesAuthenticator(log: log))
    }

    func testDemoModelNeverOpensForAnIntent() async {
        let model = AppModel(demo: .locked)
        let opened = await model.openForIntent()
        XCTAssertFalse(opened)
        XCTAssertTrue(model.isLocked)
    }

    func testErrorsExplainWhatHappened() {
        XCTAssertEqual(CopyCodeError.vaultUnavailable.text, "Tessera can’t open the vault. Open Tessera to unlock it.")
        XCTAssertEqual(CopyCodeError.notAuthenticated.text, "Tessera didn’t copy the code because authentication didn’t finish.")
    }
}
