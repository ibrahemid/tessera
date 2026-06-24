#if canImport(CryptoKit)
import CryptoKit
#else
import Crypto
#endif
import Foundation

/// XChaCha20-Poly1305 implemented as the standard XChaCha construction over
/// CryptoKit's IETF `ChaChaPoly`: HChaCha20 derives a subkey from the key and
/// the first 16 nonce bytes, then ChaChaPoly is used with a 12-byte nonce of
/// `0x00000000 || nonce[16:24]`. Wire-compatible with Go x/crypto and libsodium.
public enum XChaCha {
    public static let nonceSize = 24

    public enum CryptoError: Error, CustomStringConvertible {
        case badKey, badNonce, openFailed
        public var description: String {
            switch self {
            case .badKey: return "xchacha: key must be 32 bytes"
            case .badNonce: return "xchacha: nonce must be 24 bytes"
            case .openFailed: return "xchacha: authentication failed"
            }
        }
    }

    /// Seal plaintext; returns ciphertext with the 16-byte tag appended.
    public static func seal(_ plaintext: Data, key: Data, nonce: Data) throws -> Data {
        let (subkey, ietfNonce) = try derive(key: key, nonce: nonce)
        let box = try ChaChaPoly.seal(plaintext, using: subkey, nonce: ietfNonce)
        return box.ciphertext + box.tag
    }

    /// Open ciphertext (with appended 16-byte tag).
    public static func open(_ ciphertext: Data, key: Data, nonce: Data) throws -> Data {
        guard ciphertext.count >= 16 else { throw CryptoError.openFailed }
        let (subkey, ietfNonce) = try derive(key: key, nonce: nonce)
        let ct = ciphertext.prefix(ciphertext.count - 16)
        let tag = ciphertext.suffix(16)
        let box = try ChaChaPoly.SealedBox(nonce: ietfNonce, ciphertext: ct, tag: tag)
        do {
            return try ChaChaPoly.open(box, using: subkey)
        } catch {
            throw CryptoError.openFailed
        }
    }

    private static func derive(key: Data, nonce: Data) throws -> (SymmetricKey, ChaChaPoly.Nonce) {
        guard key.count == 32 else { throw CryptoError.badKey }
        guard nonce.count == nonceSize else { throw CryptoError.badNonce }
        let nb = [UInt8](nonce)
        let subkeyBytes = hchacha20(key: [UInt8](key), nonce16: Array(nb[0..<16]))
        // IETF nonce: 4 zero bytes + last 8 nonce bytes.
        var ietf = [UInt8](repeating: 0, count: 4)
        ietf.append(contentsOf: nb[16..<24])
        let ietfNonce = try ChaChaPoly.Nonce(data: Data(ietf))
        return (SymmetricKey(data: Data(subkeyBytes)), ietfNonce)
    }

    // MARK: HChaCha20

    private static func rotl(_ x: UInt32, _ n: UInt32) -> UInt32 { (x << n) | (x >> (32 - n)) }

    private static func quarterRound(_ s: inout [UInt32], _ a: Int, _ b: Int, _ c: Int, _ d: Int) {
        s[a] = s[a] &+ s[b]; s[d] ^= s[a]; s[d] = rotl(s[d], 16)
        s[c] = s[c] &+ s[d]; s[b] ^= s[c]; s[b] = rotl(s[b], 12)
        s[a] = s[a] &+ s[b]; s[d] ^= s[a]; s[d] = rotl(s[d], 8)
        s[c] = s[c] &+ s[d]; s[b] ^= s[c]; s[b] = rotl(s[b], 7)
    }

    private static func le32(_ b: ArraySlice<UInt8>) -> UInt32 {
        let a = Array(b)
        return UInt32(a[0]) | (UInt32(a[1]) << 8) | (UInt32(a[2]) << 16) | (UInt32(a[3]) << 24)
    }

    /// HChaCha20 subkey derivation (RFC draft-irtf-cfrg-xchacha §2.2).
    static func hchacha20(key: [UInt8], nonce16: [UInt8]) -> [UInt8] {
        var s = [UInt32](repeating: 0, count: 16)
        s[0] = 0x61707865; s[1] = 0x3320646e; s[2] = 0x79622d32; s[3] = 0x6b206574
        for i in 0..<8 { s[4 + i] = le32(key[(i * 4)..<(i * 4 + 4)]) }
        for i in 0..<4 { s[12 + i] = le32(nonce16[(i * 4)..<(i * 4 + 4)]) }
        for _ in 0..<10 {
            quarterRound(&s, 0, 4, 8, 12)
            quarterRound(&s, 1, 5, 9, 13)
            quarterRound(&s, 2, 6, 10, 14)
            quarterRound(&s, 3, 7, 11, 15)
            quarterRound(&s, 0, 5, 10, 15)
            quarterRound(&s, 1, 6, 11, 12)
            quarterRound(&s, 2, 7, 8, 13)
            quarterRound(&s, 3, 4, 9, 14)
        }
        let words = [s[0], s[1], s[2], s[3], s[12], s[13], s[14], s[15]]
        var out = [UInt8]()
        for w in words {
            out.append(UInt8(w & 0xff))
            out.append(UInt8((w >> 8) & 0xff))
            out.append(UInt8((w >> 16) & 0xff))
            out.append(UInt8((w >> 24) & 0xff))
        }
        return out
    }
}
