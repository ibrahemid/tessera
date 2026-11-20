import CArgon2
import Foundation
import TesseraCore

/// argon2id via the vendored PHC reference C implementation (portable ref.c, no
/// SIMD, threads disabled). Matches Go's golang.org/x/crypto/argon2 IDKey for any
/// (m, t, p), so the Go-sealed passphrase wrap cross-decrypts in Swift.
public struct Argon2Reference: Argon2idProvider {
    public init() {}

    public enum Argon2Error: Error, CustomStringConvertible {
        case failed(Int32)
        public var description: String { "argon2id failed (code \(self.code))" }
        private var code: Int32 { if case .failed(let c) = self { return c }; return 0 }
    }

    public func deriveKey(passphrase: Data, salt: Data, memoryKiB: UInt32, iterations: UInt32,
                          parallelism: UInt8, keyLength: Int) throws -> Data {
        var out = [UInt8](repeating: 0, count: keyLength)
        var pwd = [UInt8](passphrase)
        let slt = [UInt8](salt)
        defer {
            SecureZero.zero(&pwd)
            SecureZero.zero(&out)
        }
        let rc = argon2id_hash_raw(
            iterations, memoryKiB, UInt32(parallelism),
            pwd, pwd.count, slt, slt.count, &out, keyLength
        )
        guard rc == 0 else { throw Argon2Error.failed(rc) }
        return out.withUnsafeBytes { Data($0) }
    }
}

enum SecureZero {
    static func zero(_ bytes: inout [UInt8]) {
        bytes.withUnsafeMutableBytes { buf in
            guard let base = buf.baseAddress, buf.count > 0 else { return }
            #if canImport(Darwin)
            _ = memset_s(base, buf.count, 0, buf.count)
            #else
            buf.initializeMemory(as: UInt8.self, repeating: 0)
            #endif
        }
    }
}
