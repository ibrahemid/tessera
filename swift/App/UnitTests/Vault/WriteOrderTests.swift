import Foundation
import XCTest
import TesseraCore
@testable import Tessera

@MainActor
final class WriteOrderTests: XCTestCase {
    private func makeHost() throws -> (PersistHost, InMemoryVaultFile, Data) {
        let (envelope, dek) = try VaultFiles.makeEnvelope(accounts: [VaultFixture.account("a", issuer: "Alpha")])
        let file = InMemoryVaultFile(envelope: envelope)
        return (try PersistHost(file: file, dek: dek), file, dek)
    }

    private func adding(_ id: String) -> AccountChange {
        let account = VaultFixture.account(id, issuer: "Issuer \(id)")
        return { $0.append(account) }
    }

    private func renaming(_ id: String, to issuer: String) -> AccountChange {
        { accounts in
            if let i = accounts.firstIndex(where: { $0.id == id }) { accounts[i].issuer = issuer }
        }
    }

    func testChangeStagedDuringAKeyRotationCommitsUnderTheNewKey() async throws {
        let (host, file, oldKey) = try makeHost()
        file.hold()
        let rotation = Task { await host.runInWriteOrder { await host.rotateKey() } }
        await waitUntil { file.waitingCommits == 1 }
        XCTAssertTrue(host.mutate(adding("b")))
        XCTAssertEqual(host.accounts.map(\.id), ["a", "b"])
        file.release()
        let rotated = await rotation.value
        let newKey = try XCTUnwrap(rotated)

        XCTAssertEqual(host.dek, newKey)
        XCTAssertEqual(host.accounts.map(\.id), ["a", "b"])
        await host.settlePendingCommits()

        XCTAssertNotEqual(newKey, oldKey)
        XCTAssertEqual(try file.accounts(dek: newKey).map(\.id), ["a", "b"])
        XCTAssertThrowsError(try file.accounts(dek: oldKey))
        XCTAssertFalse(host.isLocked)
        XCTAssertTrue(host.lockReasons.isEmpty)
        XCTAssertNil(host.notice)
        XCTAssertEqual(host.accounts.map(\.id), ["a", "b"])
        let shown = try XCTUnwrap(host.history.firstIndex { $0.map(\.id) == ["a", "b"] })
        for state in host.history[shown...] { XCTAssertEqual(state.map(\.id), ["a", "b"]) }
    }

    func testChangeStagedBeforeALockLandsUnderTheKeyItWasStagedWith() async throws {
        let (host, file, key) = try makeHost()
        file.hold()
        XCTAssertTrue(host.mutate(adding("b")))
        await waitUntil { file.waitingCommits == 1 }
        host.lock(reason: .manual)
        let (_, otherKey) = try VaultFiles.makeEnvelope()
        host.unlock(dek: otherKey)
        file.release()
        await host.settlePendingCommits()

        XCTAssertEqual(try file.accounts(dek: key).map(\.id), ["a", "b"])
        XCTAssertEqual(host.lockReasons, [.manual])
        XCTAssertFalse(host.isLocked)
        XCTAssertEqual(host.dek, otherKey)
        XCTAssertNil(host.notice)
    }

    func testChangesAroundAnExclusiveWriteKeepTheirOrder() async throws {
        let (host, file, oldKey) = try makeHost()
        let gate = TestGate()
        file.hold()
        XCTAssertTrue(host.mutate(renaming("a", to: "1")))
        await waitUntil { file.waitingCommits == 1 }
        let exclusive = Task {
            await host.runInWriteOrder { () -> ExclusiveRun in
                let before = (try? file.accounts(dek: oldKey).map(\.issuer)) ?? []
                await gate.wait()
                let key = await host.rotateKey()
                return ExclusiveRun(diskBefore: before, shownAfterAdopt: host.accounts.map(\.issuer), key: key)
            }
        }
        await waitUntil { host.persistence.issued == 2 }
        XCTAssertTrue(host.mutate(renaming("a", to: "2")))
        file.release()
        await waitUntil { gate.isWaiting }
        XCTAssertTrue(host.mutate(renaming("a", to: "3")))
        XCTAssertEqual(host.accounts.map(\.issuer), ["3"])
        XCTAssertTrue(host.hasPendingCommits)
        gate.open()
        let run = await exclusive.value
        await host.settlePendingCommits()

        let newKey = try XCTUnwrap(run.key)
        XCTAssertEqual(run.diskBefore, ["1"])
        XCTAssertEqual(run.shownAfterAdopt, ["3"])
        XCTAssertEqual(try file.accounts(dek: newKey).map(\.issuer), ["3"])
        XCTAssertEqual(host.accounts.map(\.issuer), ["3"])
        XCTAssertFalse(host.hasPendingCommits)
        XCTAssertTrue(host.lockReasons.isEmpty)
        let shown = try XCTUnwrap(host.history.firstIndex { $0.map(\.issuer) == ["3"] })
        for state in host.history[shown...] { XCTAssertEqual(state.map(\.issuer), ["3"]) }
    }

    func testExclusiveWriteRunsAtOnceWhenNothingIsPending() async throws {
        let (host, _, _) = try makeHost()

        let pendingInside = await host.runInWriteOrder { host.hasPendingCommits }

        XCTAssertTrue(pendingInside)
        XCTAssertFalse(host.hasPendingCommits)
        XCTAssertEqual(host.persistence.settled, host.persistence.issued)
    }

    func testShownEditsReapplyInStagingOrderAfterAnAdopt() async throws {
        let (host, file, _) = try makeHost()
        file.hold()
        let rotation = Task { await host.runInWriteOrder { await host.rotateKey() } }
        await waitUntil { file.waitingCommits == 1 }
        XCTAssertTrue(host.mutate(renaming("a", to: "First")))
        XCTAssertTrue(host.mutate(renaming("a", to: "Second")))
        file.release()
        let rotated = await rotation.value
        let key = try XCTUnwrap(rotated)

        XCTAssertEqual(host.accounts.map(\.issuer), ["Second"])
        await host.settlePendingCommits()
        XCTAssertEqual(host.accounts.map(\.issuer), ["Second"])
        XCTAssertEqual(try file.accounts(dek: key).map(\.issuer), ["Second"])
    }
}

private struct ExclusiveRun: Sendable {
    let diskBefore: [String]
    let shownAfterAdopt: [String]
    let key: Data?
}
