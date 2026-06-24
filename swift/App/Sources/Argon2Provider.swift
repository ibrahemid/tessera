import Foundation
import Argon2Swift
import TesseraCore

/// App-side argon2id, wrapping the reference C implementation via Argon2Swift.
struct Argon2Provider: Argon2idProvider {
    func deriveKey(passphrase: Data, salt: Data, memoryKiB: UInt32, iterations: UInt32,
                   parallelism: UInt8, keyLength: Int) throws -> Data {
        let result = try Argon2Swift.hashPasswordBytes(
            password: passphrase,
            salt: Salt(bytes: salt),
            iterations: Int(iterations),
            memory: Int(memoryKiB),
            parallelism: Int(parallelism),
            length: keyLength,
            type: .id,
            version: .V13
        )
        return result.hashData()
    }
}
