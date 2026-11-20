import XCTest
import Foundation
@testable import TesseraArgon2

final class SecureZeroTests: XCTestCase {
    func testZeroClearsEveryByte() {
        var bytes = [UInt8](repeating: 0xA5, count: 64)
        SecureZero.zero(&bytes)
        XCTAssertEqual(bytes, [UInt8](repeating: 0, count: 64))
        var empty: [UInt8] = []
        SecureZero.zero(&empty)
        XCTAssertTrue(empty.isEmpty)
    }

    func testDeriveKeyStillReturnsTheKeyAfterZeroing() throws {
        let key = try Argon2Reference().deriveKey(passphrase: Data("pass".utf8), salt: Data(repeating: 1, count: 16),
                                                  memoryKiB: 8, iterations: 1, parallelism: 1, keyLength: 32)
        XCTAssertEqual(key.count, 32)
        XCTAssertNotEqual(key, Data(count: 32))
    }
}
