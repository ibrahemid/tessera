import XCTest
@testable import Tessera

/// The behavior every `KeyItemStore` keeps, run against the in-memory store.
final class KeyItemStoreContractTests: XCTestCase {
    private func makeStore() -> any KeyItemStore { InMemoryKeyItemStore() }

    private let account = SEKeyID.random().account
    private let data = Data("key-representation".utf8)

    private func item(_ data: Data, pending: Bool = false, biometrics: Bool = false) -> KeyItem {
        KeyItem(data: data, isRotationPending: pending, requiresBiometrics: biometrics)
    }

    func testAddThenReadReturnsTheItem() throws {
        let store = makeStore()
        try store.add(item(data), account: account)
        XCTAssertEqual(try store.read(account: account), item(data))
    }

    func testFreshReadReturnsThePendingMarker() throws {
        let store = makeStore()
        try store.add(item(data, pending: true), account: account)
        XCTAssertTrue(try store.read(account: account).isRotationPending)
        XCTAssertTrue(try store.read(account: account).isRotationPending)
    }

    func testFreshReadReturnsTheBiometricSetting() throws {
        let store = makeStore()
        try store.add(item(data, biometrics: true), account: account)
        XCTAssertEqual(try store.read(account: account), item(data, biometrics: true))
        XCTAssertFalse(try store.read(account: account).isRotationPending)
    }

    func testDuplicateAddFailsAndKeepsTheFirstItem() throws {
        let store = makeStore()
        try store.add(item(data), account: account)
        XCTAssertThrowsError(try store.add(item(Data("other".utf8), pending: true, biometrics: true),
                                           account: account)) {
            XCTAssertEqual($0 as? KeyItemError, .duplicate)
        }
        XCTAssertEqual(try store.read(account: account), item(data))
    }

    func testReadOfAMissingItemIsNotFound() {
        XCTAssertThrowsError(try makeStore().read(account: account)) {
            XCTAssertEqual($0 as? KeyItemError, .notFound)
        }
    }

    func testDeleteIsIdempotent() throws {
        let store = makeStore()
        try store.add(item(data), account: account)
        try store.delete(account: account)
        try store.delete(account: account)
        XCTAssertThrowsError(try store.read(account: account)) {
            XCTAssertEqual($0 as? KeyItemError, .notFound)
        }
        try store.add(item(data), account: account)
        XCTAssertEqual(try store.read(account: account).data, data)
    }

    func testItemsAreSeparateByAccount() throws {
        let store = makeStore()
        let other = SEKeyID.random().account
        try store.add(item(data), account: account)
        try store.add(item(Data("second".utf8), pending: true, biometrics: true), account: other)
        try store.delete(account: account)
        XCTAssertEqual(try store.read(account: other), item(Data("second".utf8), pending: true, biometrics: true))
    }
}
