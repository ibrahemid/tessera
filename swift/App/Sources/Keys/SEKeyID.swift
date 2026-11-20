import CryptoKit
import Foundation
import TesseraCore

/// The 16-byte id a current-form Secure Enclave wrap carries in `se_key`. It
/// names the keychain item that holds the key: `se-key.<lowercase hex>`.
struct SEKeyID: Hashable, Sendable {
    static let byteCount = 16
    private static let accountPrefix = "se-key."

    let bytes: Data

    init(bytes: Data) throws(SEKeyError) {
        guard bytes.count == Self.byteCount else { throw .malformedWrap }
        self.bytes = Data(bytes)
    }

    init?(hex: String) {
        guard hex.utf8.count == Self.byteCount * 2 else { return nil }
        var out = Data(capacity: Self.byteCount)
        var high: UInt8?
        for c in hex.utf8 {
            let v: UInt8
            switch c {
            case UInt8(ascii: "0")...UInt8(ascii: "9"): v = c - UInt8(ascii: "0")
            case UInt8(ascii: "a")...UInt8(ascii: "f"): v = c - UInt8(ascii: "a") + 10
            default: return nil
            }
            if let h = high {
                out.append(h << 4 | v)
                high = nil
            } else {
                high = v
            }
        }
        bytes = out
    }

    private init(unchecked bytes: Data) { self.bytes = bytes }

    static func random() -> SEKeyID { SEKeyID(unchecked: SecureRandom.bytes(byteCount)) }

    var hex: String { bytes.map { String(format: "%02x", $0) }.joined() }

    var account: String { Self.accountPrefix + hex }
}

extension VaultWrap {
    /// The key id of a current-form Secure Enclave wrap; nil for any other wrap.
    var secureEnclaveKeyID: SEKeyID? {
        guard secureEnclaveForm == .current, let k = se_key, let data = Data(base64Encoded: k) else { return nil }
        return try? SEKeyID(bytes: data)
    }
}

extension Envelope {
    /// The key ids of every current-form Secure Enclave wrap, in envelope order.
    var secureEnclaveKeyIDs: [SEKeyID] { secureEnclaveWraps(.current).compactMap(\.secureEnclaveKeyID) }
}

enum SecureRandom {
    /// Random bytes from the system CSPRNG through CryptoKit.
    static func bytes(_ count: Int) -> Data {
        SymmetricKey(size: SymmetricKeySize(bitCount: count * 8)).withUnsafeBytes { Data($0) }
    }
}
