import XCTest
@testable import Tessera

final class MigrationRollbackStoreTests: XCTestCase {
    private var defaults: UserDefaults!
    private var dir: URL!

    override func setUpWithError() throws {
        defaults = try makeThrowawayDefaults()
        dir = try makeTemporaryDirectory("tessera-rollback").appendingPathComponent("store")
    }

    private var vaultPath: String { vault("vault.json") }

    private func vault(_ name: String) -> String {
        dir.deletingLastPathComponent().appendingPathComponent(name).path
    }

    func testSaveWritesTheBytesAtMode0600UnderTheVaultsHashedName() throws {
        let store = MigrationRollbackStore(directory: dir, defaults: defaults)
        let bytes = Data("{\"version\":1}".utf8)
        try store.save(bytes, for: vaultPath)
        let url = store.fileURL(for: vaultPath)
        XCTAssertEqual(try Data(contentsOf: url), bytes)
        let attributes = try FileManager.default.attributesOfItem(atPath: url.path)
        XCTAssertEqual((attributes[.posixPermissions] as? NSNumber)?.intValue, 0o600)
        let dirAttributes = try FileManager.default.attributesOfItem(atPath: dir.path)
        XCTAssertEqual((dirAttributes[.posixPermissions] as? NSNumber)?.intValue, 0o700)
        let leftovers = try FileManager.default.contentsOfDirectory(atPath: dir.path)
        XCTAssertEqual(leftovers, [MigrationRollbackStore.fileName(for: SEKeyRegistry.normalizedPath(vaultPath))])
        XCTAssertEqual(store.pending(for: vaultPath), url)
    }

    func testFileNameDependsOnlyOnThePath() {
        let name = MigrationRollbackStore.fileName(for: "/a/vault.json")
        XCTAssertEqual(name, MigrationRollbackStore.fileName(for: "/a/vault.json"))
        XCTAssertNotEqual(name, MigrationRollbackStore.fileName(for: "/b/vault.json"))
        XCTAssertTrue(name.hasPrefix("rollback-"))
        XCTAssertTrue(name.hasSuffix(".json"))
        XCTAssertFalse(name.contains("vault"))
    }

    func testPendingSurvivesANewInstance() throws {
        try MigrationRollbackStore(directory: dir, defaults: defaults).save(Data("a".utf8), for: vaultPath)
        let url = try XCTUnwrap(MigrationRollbackStore(directory: dir, defaults: defaults).pending(for: vaultPath))
        XCTAssertEqual(try Data(contentsOf: url), Data("a".utf8))
    }

    func testSaveReplacesTheEarlierCopyOfTheSameVault() throws {
        let store = MigrationRollbackStore(directory: dir, defaults: defaults)
        try store.save(Data("first".utf8), for: vaultPath)
        try store.save(Data("second".utf8), for: vaultPath)
        let url = try XCTUnwrap(store.pending(for: vaultPath))
        XCTAssertEqual(try Data(contentsOf: url), Data("second".utf8))
        XCTAssertEqual(try FileManager.default.contentsOfDirectory(atPath: dir.path).count, 1)
    }

    func testTwoVaultsKeepTheirOwnCopies() throws {
        let store = MigrationRollbackStore(directory: dir, defaults: defaults)
        let other = vault("other.json")
        try store.save(Data("first".utf8), for: vaultPath)
        try store.save(Data("second".utf8), for: other)
        XCTAssertEqual(try Data(contentsOf: XCTUnwrap(store.pending(for: vaultPath))), Data("first".utf8))
        XCTAssertEqual(try Data(contentsOf: XCTUnwrap(store.pending(for: other))), Data("second".utf8))

        try store.discard(for: vaultPath)
        XCTAssertNil(store.pending(for: vaultPath))
        XCTAssertEqual(try Data(contentsOf: XCTUnwrap(store.pending(for: other))), Data("second".utf8))
    }

    func testAPathThroughASymlinkNamesTheSameCopy() throws {
        let store = MigrationRollbackStore(directory: dir, defaults: defaults)
        let real = dir.deletingLastPathComponent().appendingPathComponent("real")
        try FileManager.default.createDirectory(at: real, withIntermediateDirectories: true)
        let link = dir.deletingLastPathComponent().appendingPathComponent("link")
        try FileManager.default.createSymbolicLink(at: link, withDestinationURL: real)
        try Data("{}".utf8).write(to: real.appendingPathComponent("vault.json"))
        try store.save(Data("a".utf8), for: real.appendingPathComponent("vault.json").path)
        XCTAssertNotNil(store.pending(for: link.appendingPathComponent("vault.json").path))
    }

    func testDiscardRemovesTheFileAndTheEntry() throws {
        let store = MigrationRollbackStore(directory: dir, defaults: defaults)
        try store.save(Data("a".utf8), for: vaultPath)
        try store.discard(for: vaultPath)
        XCTAssertNil(store.pending(for: vaultPath))
        XCTAssertFalse(FileManager.default.fileExists(atPath: store.fileURL(for: vaultPath).path))
        XCTAssertNil(defaults.object(forKey: MigrationRollbackStore.copiesKey))
        try store.discard(for: vaultPath)
    }

    func testPendingNeedsTheFile() throws {
        let store = MigrationRollbackStore(directory: dir, defaults: defaults)
        XCTAssertNil(store.pending(for: vaultPath))
        try store.save(Data("a".utf8), for: vaultPath)
        try FileManager.default.removeItem(at: store.fileURL(for: vaultPath))
        XCTAssertNil(store.pending(for: vaultPath))
    }

    func testPendingNeedsTheEntry() throws {
        let store = MigrationRollbackStore(directory: dir, defaults: defaults)
        try store.save(Data("a".utf8), for: vaultPath)
        defaults.removeObject(forKey: MigrationRollbackStore.copiesKey)
        XCTAssertNil(store.pending(for: vaultPath))
    }

    func testAnEntryNamingAnotherFileIsIgnored() throws {
        let store = MigrationRollbackStore(directory: dir, defaults: defaults)
        let other = vault("other.json")
        try store.save(Data("a".utf8), for: other)
        let normalized = SEKeyRegistry.normalizedPath(vaultPath)
        defaults.set([normalized: MigrationRollbackStore.fileName(for: SEKeyRegistry.normalizedPath(other))],
                     forKey: MigrationRollbackStore.copiesKey)
        XCTAssertNil(store.pending(for: vaultPath))
    }

    func testTheSingleCopyOfAnEarlierVersionMovesToItsVault() throws {
        try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        let legacy = dir.appendingPathComponent(MigrationRollbackStore.legacyFileName)
        try Data("legacy".utf8).write(to: legacy)
        defaults.set(SEKeyRegistry.normalizedPath(vaultPath), forKey: MigrationRollbackStore.legacyVaultPathKey)

        let store = MigrationRollbackStore(directory: dir, defaults: defaults)
        let url = try XCTUnwrap(store.pending(for: vaultPath))
        XCTAssertEqual(url, store.fileURL(for: vaultPath))
        XCTAssertEqual(try Data(contentsOf: url), Data("legacy".utf8))
        XCTAssertFalse(FileManager.default.fileExists(atPath: legacy.path))
        XCTAssertNil(defaults.object(forKey: MigrationRollbackStore.legacyVaultPathKey))

        try store.save(Data("other".utf8), for: vault("other.json"))
        XCTAssertEqual(try Data(contentsOf: XCTUnwrap(store.pending(for: vaultPath))), Data("legacy".utf8))
    }

    func testAnEarlierCopyWithoutItsPathIsRemoved() throws {
        try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        let legacy = dir.appendingPathComponent(MigrationRollbackStore.legacyFileName)
        try Data("legacy".utf8).write(to: legacy)

        let store = MigrationRollbackStore(directory: dir, defaults: defaults)
        XCTAssertNil(store.pending(for: vaultPath))
        XCTAssertFalse(FileManager.default.fileExists(atPath: legacy.path))
    }

    func testAnEarlierPathWithoutItsCopyIsDropped() throws {
        defaults.set(SEKeyRegistry.normalizedPath(vaultPath), forKey: MigrationRollbackStore.legacyVaultPathKey)
        let store = MigrationRollbackStore(directory: dir, defaults: defaults)
        XCTAssertNil(store.pending(for: vaultPath))
        XCTAssertNil(defaults.object(forKey: MigrationRollbackStore.legacyVaultPathKey))
        XCTAssertNil(defaults.object(forKey: MigrationRollbackStore.copiesKey))
    }

    func testSaveIntoAnUnwritableDirectoryThrows() throws {
        let parent = dir.deletingLastPathComponent().appendingPathComponent("locked")
        try FileManager.default.createDirectory(at: parent, withIntermediateDirectories: true,
                                                attributes: [.posixPermissions: 0o500])
        defer { try? FileManager.default.setAttributes([.posixPermissions: 0o700], ofItemAtPath: parent.path) }
        let store = MigrationRollbackStore(directory: parent, defaults: defaults)
        XCTAssertThrowsError(try store.save(Data("a".utf8), for: vaultPath)) {
            guard case .io? = $0 as? MigrationRollbackError else { return XCTFail("\($0)") }
        }
        XCTAssertNil(store.pending(for: vaultPath))
    }
}
