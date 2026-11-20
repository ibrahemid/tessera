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

    func testLockDuringACommitDoesNotRepopulateAccounts() async throws {
        let (host, file, _) = try makeHost()
        file.hold()
        host.mutate(adding(VaultFixture.account("b", issuer: "Beta")))
        await waitForHeldCommit(file)
        host.lock(reason: .manual)
        file.release()
        await host.settlePendingCommits()

        XCTAssertTrue(host.isLocked)
        XCTAssertTrue(host.accounts.isEmpty)
        XCTAssertNil(host.envelope)
        XCTAssertNil(host.dek)
        XCTAssertEqual(host.lockReasons, [.manual])
    }

    func testEditWhileLockedIsRefusedWithANotice() throws {
        let (host, file, dek) = try makeHost()
        host.lock(reason: .manual)
        XCTAssertFalse(host.mutate(adding(VaultFixture.account("b", issuer: "Beta"))))
        if case .failure = host.notice {} else { XCTFail("no failure notice") }
        XCTAssertFalse(host.persistence.hasPendingCommits)
        XCTAssertEqual(ids(try file.accounts(dek: dek)), ["a"])
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

private extension Envelope {
    var encodedForComparison: Data? { try? encoded() }
}
