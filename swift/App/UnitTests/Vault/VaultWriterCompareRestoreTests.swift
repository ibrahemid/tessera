import XCTest
import TesseraCore
@testable import Tessera

final class VaultWriterCompareRestoreTests: XCTestCase {
    private let github = VaultFixture.account("g", issuer: "GitHub", account: "me@example.com")
    private let aws = VaultFixture.account("a", issuer: "AWS", account: "root")
    private let allAccess: [VaultLocation.Access] = [.container, .folder, .fileOnly]

    private struct HookFailure: Error, Equatable {}

    /// A vault this flow committed over an earlier file, with both byte strings.
    private struct Committed {
        let location: VaultLocation
        let before: Data
        let committed: Data
        let outside: Data
    }

    private func makeQueue() -> VaultIOQueue {
        VaultIOQueue(label: "tests.vault-io.\(UUID().uuidString)")
    }

    private func makeCommitted(_ access: VaultLocation.Access) async throws -> Committed {
        let location = VaultLocation(fileURL: try makeScratchDirectory().appendingPathComponent("vault.json"),
                                     access: access)
        let (envelope, dek) = try VaultFiles.makeEnvelope()
        let created = try await VaultWriter(location: location, queue: makeQueue())
            .create(VaultCommit(envelope: envelope, accounts: [], dek: dek, backupPolicy: .keep))
        let committed = try await VaultWriter(location: location, queue: makeQueue())
            .commit(heldDEK: dek, VaultFiles.appending(github, dek: dek, policy: .keep))
        let outside = try created.envelope.reseal(accounts: [aws], dek: dek).encoded()
        return Committed(location: location, before: created.bytes, committed: committed.bytes, outside: outside)
    }

    private func writer(_ vault: Committed, beforeReplace: (@Sendable () throws -> Void)? = nil) -> VaultWriter {
        VaultWriter(location: vault.location, queue: makeQueue(), beforeReplace: beforeReplace)
    }

    private func assertNoTemps(_ vault: Committed, _ access: VaultLocation.Access,
                               file: StaticString = #filePath, line: UInt = #line) {
        XCTAssertEqual(VaultFiles.tempNames(in: vault.location.fileURL.deletingLastPathComponent()), [],
                       "\(access)", file: file, line: line)
    }

    // MARK: - restore(verbatim:ifStill:)

    func testRestoreWritesTheBytesWhenTheFileIsStillTheCommittedOne() async throws {
        for access in allAccess {
            let vault = try await makeCommitted(access)
            let outcome = try await writer(vault).restore(verbatim: vault.before, ifStill: vault.committed)
            XCTAssertEqual(outcome, .restored, "\(access)")
            XCTAssertEqual(try Data(contentsOf: vault.location.fileURL), vault.before, "\(access)")
            XCTAssertEqual(VaultFiles.mode(of: vault.location.fileURL), 0o600, "\(access)")
            assertNoTemps(vault, access)
        }
    }

    func testRestoreLeavesAFileThatChangedSince() async throws {
        for access in allAccess {
            let vault = try await makeCommitted(access)
            try vault.outside.write(to: vault.location.fileURL)
            let outcome = try await writer(vault).restore(verbatim: vault.before, ifStill: vault.committed)
            XCTAssertEqual(outcome, .changedSince, "\(access)")
            XCTAssertEqual(try Data(contentsOf: vault.location.fileURL), vault.outside, "\(access)")
            assertNoTemps(vault, access)
        }
    }

    func testRestoreDoesNotRecreateARemovedFile() async throws {
        for access in allAccess {
            let vault = try await makeCommitted(access)
            try FileManager.default.removeItem(at: vault.location.fileURL)
            let outcome = try await writer(vault).restore(verbatim: vault.before, ifStill: vault.committed)
            XCTAssertEqual(outcome, .changedSince, "\(access)")
            XCTAssertFalse(VaultFiles.exists(vault.location.fileURL), "\(access)")
            assertNoTemps(vault, access)
        }
    }

    func testRestoreSeesAChangeMadeRightBeforeTheReplace() async throws {
        for access in allAccess {
            let vault = try await makeCommitted(access)
            let url = vault.location.fileURL
            let outside = vault.outside
            let restorer = writer(vault, beforeReplace: { try outside.write(to: url) })
            let outcome = try await restorer.restore(verbatim: vault.before, ifStill: vault.committed)
            XCTAssertEqual(outcome, .changedSince, "\(access)")
            XCTAssertEqual(try Data(contentsOf: url), outside, "\(access)")
            assertNoTemps(vault, access)
        }
    }

    func testRestorePassesOnAFailedCheckAndLeavesTheFile() async throws {
        for access in allAccess {
            let vault = try await makeCommitted(access)
            let restorer = writer(vault, beforeReplace: { throw HookFailure() })
            do {
                _ = try await restorer.restore(verbatim: vault.before, ifStill: vault.committed)
                XCTFail("expected the hook failure, \(access)")
            } catch {
                XCTAssertEqual(error as? HookFailure, HookFailure(), "\(access)")
            }
            XCTAssertEqual(try Data(contentsOf: vault.location.fileURL), vault.committed, "\(access)")
            assertNoTemps(vault, access)
        }
    }

    // MARK: - removeCreated(ifStill:)

    func testRemoveCreatedRemovesTheFileWhenItIsStillTheCommittedOne() async throws {
        for access in allAccess {
            let vault = try await makeCommitted(access)
            let outcome = try await writer(vault).removeCreated(ifStill: vault.committed)
            XCTAssertEqual(outcome, .restored, "\(access)")
            XCTAssertFalse(VaultFiles.exists(vault.location.fileURL), "\(access)")
            assertNoTemps(vault, access)
        }
    }

    func testRemoveCreatedLeavesAFileThatChangedSince() async throws {
        for access in allAccess {
            let vault = try await makeCommitted(access)
            try vault.outside.write(to: vault.location.fileURL)
            let outcome = try await writer(vault).removeCreated(ifStill: vault.committed)
            XCTAssertEqual(outcome, .changedSince, "\(access)")
            XCTAssertEqual(try Data(contentsOf: vault.location.fileURL), vault.outside, "\(access)")
        }
    }

    func testRemoveCreatedOfAMissingFileIsChangedSince() async throws {
        for access in allAccess {
            let vault = try await makeCommitted(access)
            try FileManager.default.removeItem(at: vault.location.fileURL)
            let outcome = try await writer(vault).removeCreated(ifStill: vault.committed)
            XCTAssertEqual(outcome, .changedSince, "\(access)")
            XCTAssertFalse(VaultFiles.exists(vault.location.fileURL), "\(access)")
        }
    }

    func testRemoveCreatedSeesAChangeMadeRightBeforeTheRemoval() async throws {
        for access in allAccess {
            let vault = try await makeCommitted(access)
            let url = vault.location.fileURL
            let outside = vault.outside
            let remover = writer(vault, beforeReplace: { try outside.write(to: url) })
            let outcome = try await remover.removeCreated(ifStill: vault.committed)
            XCTAssertEqual(outcome, .changedSince, "\(access)")
            XCTAssertEqual(try Data(contentsOf: url), outside, "\(access)")
        }
    }

    func testRemoveCreatedLeavesALinkInPlace() async throws {
        for access in allAccess {
            let vault = try await makeCommitted(access)
            let url = vault.location.fileURL
            let elsewhere = url.deletingLastPathComponent().appendingPathComponent("elsewhere.json")
            try vault.committed.write(to: elsewhere)
            try FileManager.default.removeItem(at: url)
            try VaultFiles.symlink(url, to: elsewhere)
            let outcome = try await writer(vault).removeCreated(ifStill: vault.committed)
            XCTAssertEqual(outcome, .changedSince, "\(access)")
            XCTAssertEqual(try Data(contentsOf: elsewhere), vault.committed, "\(access)")
            XCTAssertTrue(VaultFiles.exists(url), "\(access)")
        }
    }
}
