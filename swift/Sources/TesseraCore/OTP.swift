#if canImport(CryptoKit)
import CryptoKit
#else
import Crypto
#endif
import Foundation

/// HOTP (RFC 4226), TOTP (RFC 6238), and the Steam Guard variant, built on
/// swift-crypto / CryptoKit HMAC. Pinned against the shared interop vectors.
public enum OTP {
    public enum Algorithm: String, Sendable {
        case sha1 = "SHA1", sha256 = "SHA256", sha512 = "SHA512"

        public static func parse(_ s: String) throws -> Algorithm {
            switch s.uppercased() {
            case "", "SHA1": return .sha1
            case "SHA256": return .sha256
            case "SHA512": return .sha512
            default: throw AccountError.invalid("unsupported algorithm \(s)")
            }
        }
    }

    private static func hmac(_ secret: Data, _ message: Data, _ alg: Algorithm) -> Data {
        let key = SymmetricKey(data: secret)
        switch alg {
        case .sha1:   return Data(HMAC<Insecure.SHA1>.authenticationCode(for: message, using: key))
        case .sha256: return Data(HMAC<SHA256>.authenticationCode(for: message, using: key))
        case .sha512: return Data(HMAC<SHA512>.authenticationCode(for: message, using: key))
        }
    }

    private static func truncate(_ mac: Data) -> UInt32 {
        let bytes = [UInt8](mac)
        let offset = Int(bytes[bytes.count - 1] & 0x0f)
        return (UInt32(bytes[offset] & 0x7f) << 24)
            | (UInt32(bytes[offset + 1]) << 16)
            | (UInt32(bytes[offset + 2]) << 8)
            | UInt32(bytes[offset + 3])
    }

    private static func counterBytes(_ counter: UInt64) -> Data {
        var c = counter.bigEndian
        return withUnsafeBytes(of: &c) { Data($0) }
    }

    public static func hotp(secret: Data, counter: UInt64, digits: Int, algorithm: Algorithm) throws -> String {
        if secret.isEmpty { throw AccountError.invalid("empty secret") }
        if digits < 6 || digits > 8 { throw AccountError.invalid("digits must be 6-8") }
        let mac = hmac(secret, counterBytes(counter), algorithm)
        let bin = truncate(mac)
        let mod = UInt32(pow(10.0, Double(digits)))
        return String(format: "%0\(digits)u", bin % mod)
    }

    public static func totp(secret: Data, time: Date, period: Int, digits: Int, algorithm: Algorithm) throws -> String {
        if period <= 0 { throw AccountError.invalid("period must be positive") }
        let counter = UInt64(Int64(time.timeIntervalSince1970) / Int64(period))
        return try hotp(secret: secret, counter: counter, digits: digits, algorithm: algorithm)
    }

    public static func remainingSeconds(time: Date, period: Int) -> Int {
        if period <= 0 { return 0 }
        return period - Int(Int64(time.timeIntervalSince1970) % Int64(period))
    }

    private static let steamAlphabet = Array("23456789BCDFGHJKMNPQRTVWXY")

    public static func decodeSteamSecret(_ b64: String) throws -> Data {
        guard let d = Data(base64Encoded: b64.trimmingCharacters(in: .whitespacesAndNewlines)) else {
            throw AccountError.invalid("steam secret base64")
        }
        return d
    }

    public static func steam(secret: Data, time: Date) throws -> String {
        if secret.isEmpty { throw AccountError.invalid("empty secret") }
        let counter = UInt64(Int64(time.timeIntervalSince1970) / 30)
        var full = truncate(hmac(secret, counterBytes(counter), .sha1))
        var out = ""
        for _ in 0..<5 {
            out.append(steamAlphabet[Int(full % UInt32(steamAlphabet.count))])
            full /= UInt32(steamAlphabet.count)
        }
        return out
    }

    /// Compute the current code for an account.
    public static func code(for a: Account, at time: Date) throws -> String {
        let alg = try Algorithm.parse(a.algorithm)
        switch a.type {
        case .totp: return try totp(secret: a.secret, time: time, period: a.period, digits: a.digits, algorithm: alg)
        case .steam: return try steam(secret: a.secret, time: time)
        case .hotp: return try hotp(secret: a.secret, counter: UInt64(a.counter), digits: a.digits, algorithm: alg)
        }
    }
}
