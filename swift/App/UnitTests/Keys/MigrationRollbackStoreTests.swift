import XCTest
@testable import Tessera

final class MigrationRollbackStoreTests: XCTestCase {
    private var defaults: UserDefaults!
    private var dir: URL!

    override func setUpWithError() throws {
        defaults = try makeThrowawayDefaults()
        dir = try makeTemporaryDirectory("tessera-rollback").appendingPathComponent("store")
    }

    private var vaultPath: String { dir.deletingLastPathComponent().appendingPathComponent("vault.json").path }

    func testSaveWritesTheBytesAtMode0600() throws {
        let store = MigrationRollbackStore(directory: dir, defaults: defaults)
        let bytes = Data("{\"version\":1}".utf8)
        try store.save(bytes, for: vaultPath)
        XCTAssertEqual(try Data(contentsOf: store.fileURL), bytes)
        let attributes = try FileManager.default.attributesOfItem(atPath: store.fileURL.path)
        XCTAssertEqual((attributes[.posixPermissions] as? NSNumber)?.intValue, 0o600)
        let dirAttributes = try FileManager.default.attributesOfItem(atPath: dir.path)
        XCTAssertEqual((dirAttributes[.posixPermissions] as? NSNumber)?.intValue, 0o700)
        let leftovers = try FileManager.default.contentsOfDirectory(atPath: dir.path)
        XCTAssertEqual(leftovers, [MigrationRollbackStore.fileName])
    }

    func testPendingSurvivesANewInstance() throws {
        try MigrationRollbackStore(directory: dir, defaults: defaults).save(Data("a".utf8), for: vaultPath)
        let pending = try XCTUnwrap(MigrationRollbackStore(directory: dir, defaults: defaults).pending())
        XCTAssertEqual(pending.vaultPath, SEKeyRegistry.normalizedPath(vaultPath))
        XCTAssertEqual(try Data(contentsOf: pending.url), Data("a".utf8))
    }

    func testSaveReplacesTheEarlierCopy() throws {
        let store = MigrationRollbackStore(directory: dir, defaults: defaults)
        try store.save(Data("first".utf8), for: vaultPath)
        let other = dir.deletingLastPathComponent().appendingPathComponent("other.json").path
        try store.save(Data("second".utf8), for: other)
        XCTAssertEqual(store.pending()?.vaultPath, SEKeyRegistry.normalizedPath(other))
        XCTAssertEqual(try Data(contentsOf: store.fileURL), Data("second".utf8))
    }

    func testDiscardRemovesFileAndPath() throws {
        let store = MigrationRollbackStore(directory: dir, defaults: defaults)
        try store.save(Data("a".utf8), for: vaultPath)
        try store.discard()
        XCTAssertNil(store.pending())
        XCTAssertFalse(FileManager.default.fileExists(atPath: store.fileURL.path))
        XCTAssertNil(defaults.object(forKey: MigrationRollbackStore.vaultPathKey))
        try store.discard()
    }

    func testPendingNeedsTheFile() throws {
        let store = MigrationRollbackStore(directory: dir, defaults: defaults)
        XCTAssertNil(store.pending())
        try store.save(Data("a".utf8), for: vaultPath)
        try FileManager.default.removeItem(at: store.fileURL)
        XCTAssertNil(store.pending())
    }

    func testPendingNeedsThePath() throws {
        let store = MigrationRollbackStore(directory: dir, defaults: defaults)
        try store.save(Data("a".utf8), for: vaultPath)
        defaults.removeObject(forKey: MigrationRollbackStore.vaultPathKey)
        XCTAssertNil(store.pending())
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
        XCTAssertNil(store.pending())
    }
}
