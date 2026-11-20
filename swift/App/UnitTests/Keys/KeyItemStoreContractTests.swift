import XCTest
@testable import Tessera

/// The behavior every `KeyItemStore` keeps, run against the in-memory store.
final class KeyItemStoreContractTests: XCTestCase {
    private func makeStore() -> any KeyItemStore { InMemoryKeyItemStore() }

    private let account = SEKeyID.random().account
    private let data = Data("key-representation".utf8)

    func testAddThenReadReturnsTheBytes() throws {
        let store = makeStore()
        try store.add(data, account: account, isRotationPending: false)
        XCTAssertEqual(try store.read(account: account), KeyItem(data: data, isRotationPending: false))
    }

    func testFreshReadReturnsThePendingMarker() throws {
        let store = makeStore()
        try store.add(data, account: account, isRotationPending: true)
        XCTAssertTrue(try store.read(account: account).isRotationPending)
        XCTAssertTrue(try store.read(account: account).isRotationPending)
    }

    func testDuplicateAddFailsAndKeepsTheFirstItem() throws {
        let store = makeStore()
        try store.add(data, account: account, isRotationPending: false)
        XCTAssertThrowsError(try store.add(Data("other".utf8), account: account, isRotationPending: true)) {
            XCTAssertEqual($0 as? KeyItemError, .duplicate)
        }
        XCTAssertEqual(try store.read(account: account), KeyItem(data: data, isRotationPending: false))
    }

    func testReadOfAMissingItemIsNotFound() {
        XCTAssertThrowsError(try makeStore().read(account: account)) {
            XCTAssertEqual($0 as? KeyItemError, .notFound)
        }
    }

    func testDeleteIsIdempotent() throws {
        let store = makeStore()
        try store.add(data, account: account, isRotationPending: false)
        try store.delete(account: account)
        try store.delete(account: account)
        XCTAssertThrowsError(try store.read(account: account)) {
            XCTAssertEqual($0 as? KeyItemError, .notFound)
        }
        try store.add(data, account: account, isRotationPending: false)
        XCTAssertEqual(try store.read(account: account).data, data)
    }

    func testItemsAreSeparateByAccount() throws {
        let store = makeStore()
        let other = SEKeyID.random().account
        try store.add(data, account: account, isRotationPending: false)
        try store.add(Data("second".utf8), account: other, isRotationPending: true)
        try store.delete(account: account)
        XCTAssertEqual(try store.read(account: other), KeyItem(data: Data("second".utf8), isRotationPending: true))
    }
}
