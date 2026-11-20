import XCTest
import TesseraCore
@testable import Tessera

@MainActor
final class LegacyVaultTransactionTests: XCTestCase {
    private let github = VaultFixture.account("g", issuer: "GitHub", account: "me@example.com")
    private let aws = VaultFixture.account("a", issuer: "AWS", account: "root")

    private func makeLocation() throws -> VaultLocation {
        let dir = FileManager.default.temporaryDirectory.appendingPathComponent("legacy-vault-\(UUID().uuidString)")
        try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        addTeardownBlock { try? FileManager.default.removeItem(at: dir) }
        return VaultLocation(fileURL: dir.appendingPathComponent("vault.json"), access: .fileOnly)
    }

    private func makeVault(at location: VaultLocation, accounts: [Account] = []) async throws -> (LegacyVaultTransaction, CommittedVault) {
        let (envelope, dek) = try Envelope.createUnwrapped(accounts: accounts)
        let transaction = LegacyVaultTransaction(location: location)
        let created = try await transaction.create(VaultCommit(envelope: envelope, accounts: accounts, dek: dek, backupPolicy: .keep))
        return (transaction, created)
    }

    private func appending(_ account: Account, dek: Data) -> @Sendable (OpenedVault) throws -> VaultCommit {
        { opened in
            let next = opened.accounts + [account]
            return VaultCommit(envelope: try opened.snapshot.envelope.reseal(accounts: next, dek: dek),
                               accounts: next, dek: dek, backupPolicy: .copyCurrent)
        }
    }

    func testCommitRoundTripsThroughTheFile() async throws {
        let location = try makeLocation()
        let (transaction, created) = try await makeVault(at: location)
        let dek = created.dek

        let committed = try await transaction.commit(heldDEK: dek, appending(github, dek: dek))

        XCTAssertEqual(committed.accounts, [github])
        XCTAssertEqual(committed.dek, dek)
        XCTAssertEqual(try Data(contentsOf: location.fileURL), committed.bytes)
        let snapshot = try await transaction.read()
        XCTAssertEqual(snapshot.bytes, committed.bytes)
        XCTAssertEqual(try snapshot.envelope.open(dek: dek), [github])
        XCTAssertNotNil(snapshot.modifiedAt)
        XCTAssertEqual(snapshot.modifiedAt, committed.modifiedAt)
    }

    func testCommitStartsFromTheFileNotFromAnEarlierRead() async throws {
        let location = try makeLocation()
        let (transaction, created) = try await makeVault(at: location)
        let dek = created.dek
        let outside = try created.envelope.reseal(accounts: [aws], dek: dek)
        try await transaction.restore(verbatim: try outside.encoded())

        let committed = try await transaction.commit(heldDEK: dek, appending(github, dek: dek))

        XCTAssertEqual(committed.accounts, [aws, github])
        let after = try await transaction.read()
        XCTAssertEqual(try after.envelope.open(dek: dek), [aws, github])
    }

    func testRestoreWritesTheBytesVerbatim() async throws {
        let location = try makeLocation()
        let (transaction, created) = try await makeVault(at: location)
        let bytes = try created.envelope.reseal(accounts: [aws], dek: created.dek).encoded()

        try await transaction.restore(verbatim: bytes)

        XCTAssertEqual(try Data(contentsOf: location.fileURL), bytes)
    }

    func testCreateRefusesAnExistingFile() async throws {
        let location = try makeLocation()
        let (transaction, created) = try await makeVault(at: location)
        do {
            _ = try await transaction.create(VaultCommit(envelope: created.envelope, accounts: [], dek: created.dek, backupPolicy: .keep))
            XCTFail("create overwrote an existing vault")
        } catch {
            XCTAssertEqual(error as? VaultWriteError, .alreadyExists(location.fileURL))
        }
        XCTAssertEqual(try Data(contentsOf: location.fileURL), created.bytes)
    }

    func testAnotherKeyIsReportedAsKeyChanged() async throws {
        let location = try makeLocation()
        let (transaction, created) = try await makeVault(at: location)
        let other = Data(repeating: 7, count: 32)
        do {
            _ = try await transaction.commit(heldDEK: other, appending(github, dek: other))
            XCTFail("commit accepted a key that doesn't open the vault")
        } catch {
            XCTAssertEqual(error as? VaultWriteError, .keyChanged)
        }
        XCTAssertEqual(try Data(contentsOf: location.fileURL), created.bytes)
    }

    func testMissingFileIsReported() async throws {
        let location = try makeLocation()
        do {
            _ = try await LegacyVaultTransaction(location: location).read()
            XCTFail("read succeeded without a file")
        } catch {
            XCTAssertEqual(error as? VaultWriteError, .missingFile(location.fileURL))
        }
    }

    func testUnparseableFileIsReported() async throws {
        let location = try makeLocation()
        try Data("{}".utf8).write(to: location.fileURL)
        do {
            _ = try await LegacyVaultTransaction(location: location).read()
            XCTFail("read parsed an empty object")
        } catch {
            XCTAssertEqual(error as? VaultWriteError, .unparseable(location.fileURL))
        }
    }

    func testThereIsNoBackupFile() async throws {
        let location = try makeLocation()
        let (transaction, created) = try await makeVault(at: location)
        _ = try await transaction.commit(heldDEK: created.dek, appending(github, dek: created.dek))

        let backup = try await transaction.readBackup()
        XCTAssertNil(backup)
        try await transaction.deleteBackup()
        XCTAssertFalse(FileManager.default.fileExists(atPath: VaultSidecar.backupURL(for: location.fileURL).path))
    }

    func testAdoptTakesTheCommittedVault() async throws {
        let location = try makeLocation()
        let (transaction, created) = try await makeVault(at: location)
        let committed = try await transaction.commit(heldDEK: created.dek, appending(github, dek: created.dek))
        let model = AppModel(demo: .empty)

        model.adopt(committed)

        XCTAssertEqual(model.accounts, [github])
        XCTAssertEqual(model.dek, committed.dek)
        XCTAssertEqual(try model.envelope?.encoded(), try committed.envelope.encoded())
        XCTAssertEqual(model.lastVaultModified, committed.modifiedAt)
    }

    func testSidecarNamesFollowTheVaultPath() {
        let vault = URL(fileURLWithPath: "/tmp/x/vault.json")
        XCTAssertEqual(VaultSidecar.backupURL(for: vault).path, "/tmp/x/vault.json.bak")
        XCTAssertEqual(VaultSidecar.lockURL(for: vault).path, "/tmp/x/vault.json.lock")
        XCTAssertFalse(VaultLocation(fileURL: vault, access: .fileOnly).hasDirectoryAccess)
        XCTAssertTrue(VaultLocation(fileURL: vault, access: .folder).hasDirectoryAccess)
        XCTAssertTrue(VaultLocation(fileURL: vault, access: .container).hasDirectoryAccess)
    }

    func testLiveDependenciesUseTheGivenLocation() throws {
        let location = try makeLocation()
        XCTAssertEqual(AppDependencies.live().makeTransaction(location).location, location)
    }
}
