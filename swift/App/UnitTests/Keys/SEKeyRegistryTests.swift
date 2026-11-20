import XCTest
@testable import Tessera

final class SEKeyRegistryTests: XCTestCase {
    private var defaults: UserDefaults!
    private var dir: URL!

    override func setUpWithError() throws {
        defaults = try makeThrowawayDefaults()
        dir = try makeTemporaryDirectory("tessera-registry").resolvingSymlinksInPath()
    }

    func testRegisterIsIdempotentAndOrdered() {
        let registry = SEKeyRegistry(defaults: defaults)
        let path = dir.appendingPathComponent("vault.json").path
        let a = SEKeyID.random(), b = SEKeyID.random()
        registry.register(a, for: path)
        registry.register(b, for: path)
        registry.register(a, for: path)
        XCTAssertEqual(registry.ids(for: path), [a, b])
    }

    func testUnregisterRemovesTheEntryAndTheKeyWhenEmpty() {
        let registry = SEKeyRegistry(defaults: defaults)
        let path = dir.appendingPathComponent("vault.json").path
        let id = SEKeyID.random()
        registry.register(id, for: path)
        XCTAssertNotNil(defaults.dictionary(forKey: SEKeyRegistry.defaultsKey))
        registry.unregister(id, for: path)
        registry.unregister(id, for: path)
        XCTAssertEqual(registry.ids(for: path), [])
        XCTAssertNil(defaults.object(forKey: SEKeyRegistry.defaultsKey))
    }

    func testPathsAreSeparate() {
        let registry = SEKeyRegistry(defaults: defaults)
        let first = dir.appendingPathComponent("a.json").path
        let second = dir.appendingPathComponent("b.json").path
        let a = SEKeyID.random(), b = SEKeyID.random()
        registry.register(a, for: first)
        registry.register(b, for: second)
        registry.unregister(a, for: second)
        XCTAssertEqual(registry.ids(for: first), [a])
        XCTAssertEqual(registry.ids(for: second), [b])
    }

    func testSymlinkedPathResolvesToTheSameEntry() throws {
        let real = dir.appendingPathComponent("real")
        try FileManager.default.createDirectory(at: real, withIntermediateDirectories: true)
        let file = real.appendingPathComponent("vault.json")
        try Data("{}".utf8).write(to: file)
        let link = dir.appendingPathComponent("link")
        try FileManager.default.createSymbolicLink(at: link, withDestinationURL: real)

        let registry = SEKeyRegistry(defaults: defaults)
        let id = SEKeyID.random()
        registry.register(id, for: link.appendingPathComponent("vault.json").path)
        XCTAssertEqual(registry.ids(for: file.path), [id])
        XCTAssertEqual(SEKeyRegistry.normalizedPath(link.appendingPathComponent("vault.json").path), file.path)
        XCTAssertEqual(defaults.dictionary(forKey: SEKeyRegistry.defaultsKey)?.keys.sorted(), [file.path])
    }

    func testSurvivesANewInstance() {
        let path = dir.appendingPathComponent("vault.json").path
        let id = SEKeyID.random()
        SEKeyRegistry(defaults: defaults).register(id, for: path)
        XCTAssertEqual(SEKeyRegistry(defaults: defaults).ids(for: path), [id])
    }

    func testMalformedEntriesAreIgnored() {
        let path = dir.appendingPathComponent("vault.json").path
        let id = SEKeyID.random()
        defaults.set([path: ["not-hex", id.hex, "ABCDEF"], dir.path: 7] as [String: Any],
                     forKey: SEKeyRegistry.defaultsKey)
        let registry = SEKeyRegistry(defaults: defaults)
        XCTAssertEqual(registry.ids(for: path), [id])
        XCTAssertEqual(registry.ids(for: dir.path), [])
        registry.unregister(id, for: path)
        XCTAssertNil(defaults.object(forKey: SEKeyRegistry.defaultsKey))
    }

    func testIDHexRoundTrip() throws {
        let id = SEKeyID.random()
        XCTAssertEqual(SEKeyID(hex: id.hex), id)
        XCTAssertEqual(id.account, "se-key." + id.hex)
        XCTAssertNil(SEKeyID(hex: String(id.hex.dropLast())))
        XCTAssertNil(SEKeyID(hex: String(repeating: "AB", count: 16)))
        XCTAssertNil(SEKeyID(hex: "zz" + String(repeating: "00", count: 15)))
        XCTAssertThrowsError(try SEKeyID(bytes: Data(count: 15)))
        XCTAssertEqual(try SEKeyID(bytes: Data(repeating: 0xab, count: 16)).hex, String(repeating: "ab", count: 16))
    }
}
