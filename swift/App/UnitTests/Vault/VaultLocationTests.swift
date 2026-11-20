import Darwin
import XCTest
@testable import Tessera

final class VaultLocationTests: XCTestCase {
    private func makeDefaults() throws -> UserDefaults {
        let suite = "tessera.tests.\(UUID().uuidString)"
        let defaults = try XCTUnwrap(UserDefaults(suiteName: suite))
        addTeardownBlock { UserDefaults(suiteName: suite)?.removePersistentDomain(forName: suite) }
        return defaults
    }

    private func makeStore(defaults: UserDefaults, environment: [String: String] = [:],
                           in dir: URL) -> VaultStore {
        VaultStore(defaults: defaults, environment: environment,
                   containerVaultURL: dir.appendingPathComponent("container/vault.json"))
    }

    private func makeFile(in dir: URL, folder: String = "cli") throws -> URL {
        let parent = dir.appendingPathComponent(folder, isDirectory: true)
        try FileManager.default.createDirectory(at: parent, withIntermediateDirectories: true)
        let file = parent.appendingPathComponent("vault.json")
        try VaultFiles.write(Data("{}".utf8), to: file, mode: 0o600)
        return file
    }

    private func assertOnContainer(_ store: VaultStore, dir: URL, file: StaticString = #filePath, line: UInt = #line) {
        XCTAssertEqual(store.location, VaultLocation(fileURL: dir.appendingPathComponent("container/vault.json"),
                                                     access: .container), file: file, line: line)
        XCTAssertFalse(store.isExternal, file: file, line: line)
    }

    func testTheContainerIsTheDefault() throws {
        let dir = try makeScratchDirectory()
        let store = makeStore(defaults: try makeDefaults(), in: dir)

        assertOnContainer(store, dir: dir)
        XCTAssertFalse(store.hasBookmark)
        XCTAssertEqual(store.containerVaultURL, store.vaultURL)
        XCTAssertFalse(store.exists)
    }

    #if DEBUG
    func testTheDebugEnvironmentPointsAtAFolder() throws {
        let dir = try makeScratchDirectory()
        let vault = dir.appendingPathComponent("cli/vault.json")
        let store = makeStore(defaults: try makeDefaults(), environment: [VaultStore.environmentKey: vault.path], in: dir)

        XCTAssertEqual(store.location, VaultLocation(fileURL: vault, access: .folder))
        XCTAssertFalse(store.isExternal)
        XCTAssertFalse(store.hasBookmark)
    }

    func testAnEmptyEnvironmentValueIsIgnored() throws {
        let dir = try makeScratchDirectory()
        let store = makeStore(defaults: try makeDefaults(), environment: [VaultStore.environmentKey: ""], in: dir)

        assertOnContainer(store, dir: dir)
    }
    #endif

    func testAFolderGrantPointsAtTheFileInsideIt() throws {
        let dir = try makeScratchDirectory()
        let defaults = try makeDefaults()
        let file = try makeFile(in: dir)
        let store = makeStore(defaults: defaults, in: dir)
        XCTAssertTrue(store.setExternal(file))

        XCTAssertTrue(store.setFolder(file.deletingLastPathComponent(), fileName: file.lastPathComponent))

        XCTAssertEqual(store.location.access, .folder)
        XCTAssertEqual(VaultFiles.realPath(store.location.fileURL).path, file.path)
        XCTAssertTrue(store.isExternal)
        XCTAssertTrue(store.hasBookmark)
        XCTAssertNil(defaults.data(forKey: VaultStore.bookmarkKey))
        XCTAssertNotNil(defaults.data(forKey: VaultStore.folderBookmarkKey))
        XCTAssertEqual(defaults.string(forKey: VaultStore.fileNameKey), "vault.json")

        let reopened = makeStore(defaults: defaults, in: dir)
        XCTAssertEqual(reopened.location.access, .folder)
        XCTAssertEqual(VaultFiles.realPath(reopened.location.fileURL).path, file.path)
    }

    func testAFileGrantIsFileOnly() throws {
        let dir = try makeScratchDirectory()
        let defaults = try makeDefaults()
        let file = try makeFile(in: dir)
        let store = makeStore(defaults: defaults, in: dir)
        XCTAssertTrue(store.setFolder(file.deletingLastPathComponent(), fileName: file.lastPathComponent))

        XCTAssertTrue(store.setExternal(file))

        XCTAssertEqual(store.location.access, .fileOnly)
        XCTAssertFalse(store.location.hasDirectoryAccess)
        XCTAssertEqual(VaultFiles.realPath(store.location.fileURL).path, file.path)
        XCTAssertTrue(store.isExternal)
        XCTAssertNil(defaults.data(forKey: VaultStore.folderBookmarkKey))
        XCTAssertNil(defaults.string(forKey: VaultStore.fileNameKey))
        XCTAssertNotNil(defaults.data(forKey: VaultStore.bookmarkKey))
    }

    func testAnInvalidFileNameIsRefused() throws {
        let dir = try makeScratchDirectory()
        let defaults = try makeDefaults()
        let store = makeStore(defaults: defaults, in: dir)

        for name in ["", ".", "..", "../vault.json", "a/vault.json", "vault\0.json"] {
            XCTAssertFalse(store.setFolder(dir, fileName: name), name)
        }

        assertOnContainer(store, dir: dir)
        XCTAssertNil(defaults.data(forKey: VaultStore.folderBookmarkKey))
    }

    func testAnUnresolvableBookmarkFallsBackToTheContainer() throws {
        for key in [VaultStore.folderBookmarkKey, VaultStore.bookmarkKey] {
            let dir = try makeScratchDirectory()
            let defaults = try makeDefaults()
            defaults.set(Data("not a bookmark".utf8), forKey: key)
            defaults.set("vault.json", forKey: VaultStore.fileNameKey)

            let store = makeStore(defaults: defaults, in: dir)

            assertOnContainer(store, dir: dir)
            XCTAssertTrue(store.hasBookmark)
        }
    }

    func testAStoredFolderWithABadNameFallsBackToTheContainer() throws {
        let dir = try makeScratchDirectory()
        let defaults = try makeDefaults()
        let file = try makeFile(in: dir)
        makeStore(defaults: defaults, in: dir).setFolder(file.deletingLastPathComponent(), fileName: "vault.json")
        defaults.set("../vault.json", forKey: VaultStore.fileNameKey)

        let store = makeStore(defaults: defaults, in: dir)

        assertOnContainer(store, dir: dir)
        XCTAssertTrue(store.hasBookmark)
    }

    func testClearExternalReturnsToTheContainer() throws {
        let dir = try makeScratchDirectory()
        let defaults = try makeDefaults()
        let file = try makeFile(in: dir)
        let store = makeStore(defaults: defaults, in: dir)
        XCTAssertTrue(store.setFolder(file.deletingLastPathComponent(), fileName: file.lastPathComponent))

        store.clearExternal()

        assertOnContainer(store, dir: dir)
        XCTAssertFalse(store.hasBookmark)
        for key in [VaultStore.bookmarkKey, VaultStore.folderBookmarkKey, VaultStore.fileNameKey] {
            XCTAssertNil(defaults.object(forKey: key), key)
        }
    }

    func testSaveCreatesThePrivateContainerFolder() throws {
        let dir = try makeScratchDirectory()
        let store = makeStore(defaults: try makeDefaults(), in: dir)
        let (envelope, _) = try VaultFiles.makeEnvelope()

        try store.save(envelope)

        XCTAssertTrue(store.exists)
        XCTAssertEqual(try Data(contentsOf: store.vaultURL), try envelope.encoded())
        XCTAssertEqual(VaultFiles.mode(of: store.vaultURL), 0o600)
        XCTAssertEqual(VaultFiles.mode(of: store.vaultURL.deletingLastPathComponent()), 0o700)
        XCTAssertEqual(try store.load().encoded(), try envelope.encoded())
        XCTAssertNotNil(store.modifiedAt)
    }

    func testHomeRelativeUsesTheGivenHome() {
        let home = URL(fileURLWithPath: "/Users/tester", isDirectory: true)
        func relative(_ path: String) -> String { RealHome.homeRelative(URL(fileURLWithPath: path), home: home) }

        XCTAssertEqual(relative("/Users/tester"), "~")
        XCTAssertEqual(relative("/Users/tester/.local/share/tessera/vault.json"), "~/.local/share/tessera/vault.json")
        XCTAssertEqual(relative("/Users/tester/./notes/../vault.json"), "~/vault.json")
        XCTAssertEqual(relative("/Users/testers/vault.json"), "/Users/testers/vault.json")
        XCTAssertEqual(relative("/Volumes/Keys/vault.json"), "/Volumes/Keys/vault.json")
        XCTAssertEqual(RealHome.homeRelative(URL(fileURLWithPath: "/vault.json"), home: URL(fileURLWithPath: "/")),
                       "/vault.json")
    }

    func testTheCLIFolderIsUnderTheGivenHome() {
        let home = URL(fileURLWithPath: "/Users/tester", isDirectory: true)
        XCTAssertEqual(RealHome.cliVaultDirectory(home: home).path, "/Users/tester/.local/share/tessera")
    }

    func testTheRealHomeIsOutsideTheContainer() {
        let home = RealHome.url().path
        XCTAssertFalse(home.isEmpty)
        XCTAssertFalse(home.contains("/Library/Containers/"), home)
    }

    func testIsParentFolder() throws {
        let dir = try makeScratchDirectory()
        let file = try makeFile(in: dir)
        let folder = file.deletingLastPathComponent()
        let linked = dir.appendingPathComponent("linked")
        try VaultFiles.symlink(linked, to: folder)

        XCTAssertTrue(VaultStore.isParentFolder(folder, of: file))
        XCTAssertTrue(VaultStore.isParentFolder(URL(fileURLWithPath: folder.path + "/"), of: file))
        XCTAssertTrue(VaultStore.isParentFolder(linked, of: file))
        XCTAssertFalse(VaultStore.isParentFolder(dir, of: file))
        XCTAssertFalse(VaultStore.isParentFolder(folder.appendingPathComponent("vault.json"), of: file))
    }

    func testNearestExistingAncestor() throws {
        let dir = try makeScratchDirectory()
        let file = try makeFile(in: dir)

        XCTAssertEqual(RealHome.nearestExistingAncestor(of: dir.appendingPathComponent("a/b/c")).path, dir.path)
        XCTAssertEqual(RealHome.nearestExistingAncestor(of: dir).path, dir.path)
        XCTAssertEqual(RealHome.nearestExistingAncestor(of: file).path, file.deletingLastPathComponent().path)
        XCTAssertEqual(RealHome.nearestExistingAncestor(of: file.appendingPathComponent("x")).path,
                       file.deletingLastPathComponent().path)
    }
}
