import XCTest
import Foundation
@testable import TesseraCore

final class MemoryWipeTests: XCTestCase {
    func testZeroClearsArraysAndData() {
        var bytes = [UInt8](repeating: 0xA5, count: 64)
        MemoryWipe.zero(&bytes)
        XCTAssertEqual(bytes, [UInt8](repeating: 0, count: 64))
        var words = [UInt32](repeating: .max, count: 16)
        MemoryWipe.zero(&words)
        XCTAssertEqual(words, [UInt32](repeating: 0, count: 16))
        var data = Data(repeating: 0x5A, count: 48)
        MemoryWipe.zero(&data)
        XCTAssertEqual(data, Data(count: 48))
        var empty: [UInt8] = []
        MemoryWipe.zero(&empty)
        var emptyData = Data()
        MemoryWipe.zero(&emptyData)
        XCTAssertTrue(empty.isEmpty && emptyData.isEmpty)
    }

    func testXChaChaStillRoundTripsAfterWipingItsKeyCopies() throws {
        let key = Data(repeating: 7, count: 32), nonce = Data(repeating: 9, count: 24)
        let sealed = try XChaCha.seal(Data("payload".utf8), key: key, nonce: nonce)
        XCTAssertEqual(key, Data(repeating: 7, count: 32))
        XCTAssertEqual(try XChaCha.open(sealed, key: key, nonce: nonce), Data("payload".utf8))
    }
}
