import AppKit
import XCTest
import TesseraCore
@testable import Tessera

@MainActor
final class PersistFacadeTests: XCTestCase {
    private func makeHost(_ accounts: [Account] = [VaultFixture.account("a", issuer: "Alpha")]) throws
        -> (PersistHost, InMemoryVaultFile, Data) {
        let (envelope, dek) = try VaultFiles.makeEnvelope(accounts: accounts)
        let file = InMemoryVaultFile(envelope: envelope)
        return (try PersistHost(file: file, dek: dek), file, dek)
    }

    private func adding(_ account: Account) -> AccountChange { { $0.append(account) } }

    private func ids(_ accounts: [Account]) -> Set<String> { Set(accounts.map(\.id)) }

    private func waitForHeldCommit(_ file: InMemoryVaultFile) async {
        for _ in 0..<2_000 where file.waitingCommits == 0 { try? await Task.sleep(for: .milliseconds(1)) }
        XCTAssertEqual(file.waitingCommits, 1)
    }

    func testExternalAddBetweenTwoEditsSurvives() async throws {
        let (host, file, dek) = try makeHost()
        XCTAssertTrue(host.mutate(adding(VaultFixture.account("b", issuer: "Beta"))))
        await host.settlePendingCommits()
        try file.externalAdd(VaultFixture.account("cli", issuer: "Terminal"), dek: dek)
        XCTAssertTrue(host.mutate(adding(VaultFixture.account("c", issuer: "Gamma"))))
        await host.settlePendingCommits()

        XCTAssertEqual(ids(try file.accounts(dek: dek)), ["a", "b", "cli", "c"])
        XCTAssertEqual(ids(host.accounts), ["a", "b", "cli", "c"])
        XCTAssertFalse(host.persistence.hasPendingCommits)
    }

    func testTwoQuickEditsBothLandWithoutAnIntermediateRevert() async throws {
        let (host, file, dek) = try makeHost()
        file.hold()
        host.mutate(adding(VaultFixture.account("b", issuer: "Beta")))
        host.mutate(adding(VaultFixture.account("c", issuer: "Gamma")))
        XCTAssertEqual(ids(host.accounts), ["a", "b", "c"])
        await waitForHeldCommit(file)
        file.release()
        await host.settlePendingCommits()

        XCTAssertEqual(ids(try file.accounts(dek: dek)), ["a", "b", "c"])
        let afterBoth = try XCTUnwrap(host.history.firstIndex { ids($0) == ["a", "b", "c"] })
        for state in host.history[afterBoth...] { XCTAssertEqual(ids(state), ["a", "b", "c"]) }
        XCTAssertNil(host.notice)
    }

    func testFailedCommitShowsANoticeAndReloadsFromDisk() async throws {
        let (host, file, dek) = try makeHost()
        try file.externalAdd(VaultFixture.account("cli", issuer: "Terminal"), dek: dek)
        let failure = VaultWriteError.concurrentChange(file.location.fileURL)
        file.failNextCommit(with: failure)
        host.mutate(adding(VaultFixture.account("b", issuer: "Beta")))
        XCTAssertEqual(ids(host.accounts), ["a", "b"])
        await host.settlePendingCommits()

        XCTAssertEqual(host.notice, .failure(UserMessage.text(for: failure, context: .save)))
        XCTAssertEqual(ids(host.accounts), ["a", "cli"])
        XCTAssertEqual(ids(try file.accounts(dek: dek)), ["a", "cli"])
        XCTAssertEqual(host.envelope?.encodedForComparison, file.envelope.encodedForComparison)
    }

    func testKeyChangedLocksTheVault() async throws {
        let (host, file, _) = try makeHost()
        let (rekeyed, _) = try VaultFiles.makeEnvelope(accounts: [VaultFixture.account("a", issuer: "Alpha")])
        file.replace(with: rekeyed)
        host.mutate(adding(VaultFixture.account("b", issuer: "Beta")))
        await host.settlePendingCommits()

        XCTAssertEqual(host.lockReasons, [.keyChanged])
        XCTAssertTrue(host.isLocked)
        XCTAssertTrue(host.accounts.isEmpty)
    }

    func testEditsStagedBeforeALockLandOnDiskWithoutRepopulatingAccounts() async throws {
        let (host, file, dek) = try makeHost()
        file.hold()
        host.mutate(adding(VaultFixture.account("b", issuer: "Beta")))
        host.mutate(adding(VaultFixture.account("c", issuer: "Gamma")))
        await waitForHeldCommit(file)
        host.lock(reason: .manual)
        let lockedAt = host.history.count
        file.release()
        await host.settlePendingCommits()

        XCTAssertEqual(ids(try file.accounts(dek: dek)), ["a", "b", "c"])
        XCTAssertTrue(host.isLocked)
        XCTAssertTrue(host.accounts.isEmpty)
        XCTAssertEqual(host.history.count, lockedAt)
        XCTAssertNil(host.envelope)
        XCTAssertNil(host.dek)
        XCTAssertNil(host.notice)
        XCTAssertEqual(host.lockReasons, [.manual])
    }

    func testEditWhileLockedIsRefusedAsAClosedVault() throws {
        let (host, file, dek) = try makeHost()
        host.lock(reason: .manual)

        XCTAssertThrowsError(try host.stage(adding(VaultFixture.account("b", issuer: "Beta")))) { error in
            XCTAssertEqual(error as? VaultEditError, .vaultClosed)
        }
        XCTAssertFalse(host.mutate(adding(VaultFixture.account("b", issuer: "Beta"))))
        XCTAssertEqual(host.notice, .failure("The vault is locked, so this change wasn’t saved."))
        XCTAssertEqual(UserMessage.text(for: VaultEditError.vaultClosed, context: .save),
                       UserMessage.text(for: VaultEditError.vaultClosed))
        XCTAssertFalse(host.persistence.hasPendingCommits)
        XCTAssertEqual(ids(try file.accounts(dek: dek)), ["a"])
    }

    func testEditWithoutAHeldKeyIsRefusedAsAClosedVault() throws {
        let (host, file, dek) = try makeHost()
        host.dek = nil

        XCTAssertThrowsError(try host.stage(adding(VaultFixture.account("b", issuer: "Beta")))) { error in
            XCTAssertEqual(error as? VaultEditError, .vaultClosed)
        }
        XCTAssertEqual(ids(host.accounts), ["a"])
        XCTAssertFalse(host.persistence.hasPendingCommits)
        XCTAssertEqual(ids(try file.accounts(dek: dek)), ["a"])
    }

    func testReloadRequestedDuringCommitsRunsOnceAfterTheLastSettles() async throws {
        let (host, file, dek) = try makeHost()
        let reloads = ReloadLog()
        let reload: @MainActor () -> Void = { [unowned host] in
            let disk = (try? file.accounts(dek: dek)) ?? []
            reloads.entries.append((host.persistence.hasPendingCommits, Set(disk.map(\.id))))
            host.accounts = disk
        }
        file.hold()
        host.mutate(adding(VaultFixture.account("b", issuer: "Beta")))
        host.whenCommitsSettle(reload)
        host.mutate(adding(VaultFixture.account("c", issuer: "Gamma")))
        host.whenCommitsSettle(reload)
        await waitForHeldCommit(file)
        XCTAssertTrue(reloads.entries.isEmpty)
        file.release()
        await host.settlePendingCommits()

        XCTAssertEqual(reloads.entries.count, 1)
        XCTAssertEqual(reloads.entries.first?.pending, false)
        XCTAssertEqual(reloads.entries.first?.onDisk, ["a", "b", "c"])
        let afterBoth = try XCTUnwrap(host.history.firstIndex { ids($0) == ["a", "b", "c"] })
        for state in host.history[afterBoth...] { XCTAssertEqual(ids(state), ["a", "b", "c"]) }
    }

    func testReloadWithNothingPendingRunsAtOnce() throws {
        let (host, _, _) = try makeHost()
        let reloads = ReloadLog()

        host.whenCommitsSettle { reloads.entries.append((false, [])) }

        XCTAssertEqual(reloads.entries.count, 1)
        XCTAssertNil(host.persistence.whenSettled)
    }

    func testHOTPCodeIsCopiedOnlyAfterTheCounterCommits() async throws {
        var hotp = VaultFixture.account("h", issuer: "Bank", type: .hotp, period: 0)
        hotp.counter = 4
        let (host, file, dek) = try makeHost([hotp])
        file.hold()
        let copy = try XCTUnwrap(host.advanceHOTP(hotp, to: DiscardingPasteboard()))
        await waitForHeldCommit(file)
        XCTAssertTrue(host.copied.isEmpty)
        XCTAssertEqual(host.accounts.first?.counter, 4)
        XCTAssertEqual(try file.accounts(dek: dek).first?.counter, 4)

        file.release()
        let copied = await copy.value

        XCTAssertTrue(copied)
        var advanced = hotp
        advanced.counter = 5
        XCTAssertEqual(host.copied, [try OTP.code(for: advanced, at: Date())])
        XCTAssertEqual(host.accounts.first?.counter, 5)
        XCTAssertEqual(try file.accounts(dek: dek).first?.counter, 5)
        XCTAssertEqual(host.notice, .info("Copied Bank"))
    }

    func testHOTPCopyIsNotReportedWhenTheCommitFails() async throws {
        let hotp = VaultFixture.account("h", issuer: "Bank", type: .hotp, period: 0)
        let (host, file, _) = try makeHost([hotp])
        file.failNextCommit(with: VaultWriteError.concurrentChange(file.location.fileURL))
        let copy = try XCTUnwrap(host.advanceHOTP(hotp, to: DiscardingPasteboard()))

        let copied = await copy.value

        XCTAssertFalse(copied)
        XCTAssertTrue(host.copied.isEmpty)
        if case .failure = host.notice {} else { XCTFail("no failure notice") }
    }

    func testHOTPCopyFailureShowsTheCopyNotice() async throws {
        let hotp = VaultFixture.account("h", issuer: "Bank", type: .hotp, period: 0)
        let (host, _, dek) = try makeHost([hotp])
        host.copySucceeds = false
        let copy = try XCTUnwrap(host.advanceHOTP(hotp, to: DiscardingPasteboard()))

        let copied = await copy.value

        XCTAssertFalse(copied)
        XCTAssertEqual(host.notice, .failure(UserMessage.copyFailed))
        XCTAssertEqual(try host.file.accounts(dek: dek).first?.counter, 1)
    }

    func testHOTPAtTheMaximumCounterIsRefusedBeforeAnyCommit() throws {
        var hotp = VaultFixture.account("h", issuer: "Bank", type: .hotp, period: 0)
        hotp.counter = Account.maxCounter
        let (host, _, _) = try makeHost([hotp])

        XCTAssertNil(host.advanceHOTP(hotp, to: DiscardingPasteboard()))
        XCTAssertFalse(host.persistence.hasPendingCommits)
        if case .failure = host.notice {} else { XCTFail("no failure notice") }
    }

    func testCopyCodeFailureIsNotReportedAsCopied() throws {
        let (host, _, _) = try makeHost()
        host.copySucceeds = false

        XCTAssertNil(host.copyCode(host.accounts[0], at: Date(), to: DiscardingPasteboard()))
        XCTAssertEqual(host.notice, .failure(UserMessage.copyFailed))
    }

    func testAdoptTakesTheCommittedState() throws {
        let (host, _, _) = try makeHost()
        let next = [VaultFixture.account("z", issuer: "Zeta")]
        let (envelope, dek) = try VaultFiles.makeEnvelope(accounts: next)
        let stamp = Date(timeIntervalSince1970: 42)

        host.adopt(CommittedVault(envelope: envelope, accounts: next, dek: dek,
                                  bytes: try envelope.encoded(), modifiedAt: stamp))

        XCTAssertEqual(host.accounts, next)
        XCTAssertEqual(host.dek, dek)
        XCTAssertEqual(host.lastVaultModified, stamp)
        XCTAssertEqual(host.envelope?.encodedForComparison, envelope.encodedForComparison)
    }

    func testMergeAddsOnlyWhatTheFreshListLacks() async throws {
        let (host, file, dek) = try makeHost()
        let shared = VaultFixture.account("from-cli", issuer: "Shared")
        try file.externalAdd(shared, dek: dek)
        var duplicate = shared
        duplicate.id = "imported"
        let change = VaultChange.appendingNew([duplicate, VaultFixture.account("new", issuer: "New")])

        try host.stage(change)
        await host.settlePendingCommits()

        XCTAssertEqual(ids(try file.accounts(dek: dek)), ["a", "from-cli", "new"])
        XCTAssertEqual(ids(host.accounts), ["a", "from-cli", "new"])
    }

    func testEditCarriesOnlyChangedFieldsOntoTheFreshAccount() {
        var original = VaultFixture.account("h", issuer: "Bank", type: .hotp, period: 0)
        original.counter = 3
        var edited = original
        edited.issuer = "Bank Two"
        edited.updatedAt = 9
        var fresh = original
        fresh.counter = 7

        let merged = VaultChange.carryingEdits(from: original, to: edited, onto: fresh)

        XCTAssertEqual(merged.issuer, "Bank Two")
        XCTAssertEqual(merged.counter, 7)
        XCTAssertEqual(merged.updatedAt, 9)
    }

    func testHandleMigrationWritesOnceThenNoOps() async throws {
        let (host, file, dek) = try makeHost([VaultFixture.account("a", issuer: "Alpha")])
        XCTAssertEqual(host.accounts.first?.handle, "")

        host.migrateHandles()
        await host.settlePendingCommits()
        let issued = host.persistence.issued
        host.migrateHandles()

        XCTAssertEqual(issued, 1)
        XCTAssertEqual(host.persistence.issued, 1)
        XCTAssertFalse(try XCTUnwrap(try file.accounts(dek: dek).first).handle.isEmpty)
        XCTAssertEqual(host.accounts.first?.handle, try file.accounts(dek: dek).first?.handle)
    }
}

@MainActor
private final class ReloadLog {
    var entries: [(pending: Bool, onDisk: Set<String>)] = []
}

@MainActor
private final class HostCopyTarget: CodeCopyTarget {
    let host: PersistHost

    init(host: PersistHost) { self.host = host }

    var isLocked: Bool { host.isLocked }

    func openForIntent() async -> Bool { !host.isLocked }

    func account(withID id: String) -> Account? { host.accounts.first { $0.id == id } }

    func copyCodeForIntent(_ account: Account) async -> Bool {
        await host.copyConfirmed(account, at: Date(), to: DiscardingPasteboard())
    }
}

private struct PassingAuthenticator: IntentAuthenticating {
    func authenticate(reason: String) async throws {}
}

@MainActor
final class CopyIntentOutcomeTests: XCTestCase {
    private let hotp = VaultFixture.account("h", issuer: "Bank", type: .hotp, period: 0)

    private func makeTarget() throws -> (HostCopyTarget, InMemoryVaultFile, Data) {
        let (envelope, dek) = try VaultFiles.makeEnvelope(accounts: [hotp])
        let file = InMemoryVaultFile(envelope: envelope)
        return (HostCopyTarget(host: try PersistHost(file: file, dek: dek)), file, dek)
    }

    private func run(_ target: HostCopyTarget) async -> Result<String, CopyCodeError> {
        do {
            return .success(try await CopyCodeGate.run(accountID: "h", target: target,
                                                       authenticator: PassingAuthenticator()))
        } catch {
            return .failure(error as? CopyCodeError ?? .vaultUnavailable)
        }
    }

    func testIntentReportsSuccessOnlyAfterTheCounterCommits() async throws {
        let (target, file, dek) = try makeTarget()
        file.hold()
        let outcome = Task { await run(target) }
        for _ in 0..<2_000 where file.waitingCommits == 0 { try? await Task.sleep(for: .milliseconds(1)) }
        XCTAssertEqual(file.waitingCommits, 1)
        XCTAssertTrue(target.host.copied.isEmpty)
        file.release()
        let result = await outcome.value

        XCTAssertEqual(result, .success("Bank"))
        XCTAssertEqual(target.host.copied.count, 1)
        XCTAssertEqual(try file.accounts(dek: dek).first?.counter, 1)
    }

    func testIntentReportsFailureWhenTheCounterCommitFails() async throws {
        let (target, file, dek) = try makeTarget()
        file.failNextCommit(with: VaultWriteError.concurrentChange(file.location.fileURL))

        let result = await run(target)

        XCTAssertEqual(result, .failure(.copyFailed))
        XCTAssertTrue(target.host.copied.isEmpty)
        XCTAssertEqual(try file.accounts(dek: dek).first?.counter, 0)
    }

    func testIntentReportsFailureWhenTheCopyFails() async throws {
        let (target, file, dek) = try makeTarget()
        target.host.copySucceeds = false

        let result = await run(target)

        XCTAssertEqual(result, .failure(.copyFailed))
        XCTAssertEqual(target.host.notice, .failure(UserMessage.copyFailed))
        XCTAssertEqual(try file.accounts(dek: dek).first?.counter, 1)
    }

    func testTimeBasedCopyIsConfirmedAtOnce() async throws {
        let totp = VaultFixture.account("t", issuer: "Mail")
        let (envelope, dek) = try VaultFiles.makeEnvelope(accounts: [totp])
        let host = try PersistHost(file: InMemoryVaultFile(envelope: envelope), dek: dek)

        let copied = await host.copyConfirmed(totp, at: Date(), to: DiscardingPasteboard())

        XCTAssertTrue(copied)
        XCTAssertFalse(host.persistence.hasPendingCommits)
        XCTAssertEqual(host.copied.count, 1)
    }

    func testCopyIsRefusedWhileLocked() async throws {
        let (target, _, _) = try makeTarget()
        target.host.lock(reason: .manual)

        let copied = await target.host.copyConfirmed(hotp, at: Date(), to: DiscardingPasteboard())

        XCTAssertFalse(copied)
        XCTAssertFalse(target.host.persistence.hasPendingCommits)
    }
}

private extension Envelope {
    var encodedForComparison: Data? { try? encoded() }
}
