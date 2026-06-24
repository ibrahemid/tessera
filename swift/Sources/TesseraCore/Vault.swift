import Foundation

/// Argon2id is the one primitive CryptoKit lacks. The core depends on this
/// provider abstraction; the app/CI inject a vetted argon2 implementation.
public protocol Argon2idProvider {
    func deriveKey(passphrase: Data, salt: Data, memoryKiB: UInt32, iterations: UInt32,
                   parallelism: UInt8, keyLength: Int) throws -> Data
}

public enum VaultError: Error, CustomStringConvertible {
    case unsupportedVersion, unsupportedAEAD, noPassphraseWrap, wrongPassphrase, corrupt, badBase64(String)
    public var description: String {
        switch self {
        case .unsupportedVersion: return "vault: unsupported version"
        case .unsupportedAEAD: return "vault: unsupported aead"
        case .noPassphraseWrap: return "vault: no passphrase wrap present"
        case .wrongPassphrase: return "vault: wrong passphrase or corrupt vault"
        case .corrupt: return "vault: corrupt or tampered vault"
        case .badBase64(let f): return "vault: invalid base64 in \(f) (base64url is rejected)"
        }
    }
}

public struct Argon2Params: Codable, Sendable {
    public var v: Int
    public var m: UInt32
    public var t: UInt32
    public var p: UInt8
}

public struct VaultBox: Codable, Sendable {
    public var nonce: String
    public var ct: String
}

public struct VaultWrap: Codable, Sendable {
    public var type: String
    public var kdf: String?
    public var params: Argon2Params?
    public var salt: String?
    public var se_key: String?
    public var nonce: String
    public var ct: String
}

public struct Envelope: Codable, Sendable {
    public var version: Int
    public var aead: String
    public var wraps: [VaultWrap]
    public var payload: VaultBox

    public static func decode(_ data: Data) throws -> Envelope {
        let env = try JSONDecoder().decode(Envelope.self, from: data)
        if env.version != 1 { throw VaultError.unsupportedVersion }
        if env.aead != "xchacha20poly1305" { throw VaultError.unsupportedAEAD }
        return env
    }

    public func encoded() throws -> Data {
        let enc = JSONEncoder()
        enc.outputFormatting = [.prettyPrinted]
        return try enc.encode(self)
    }

    /// Decrypt with a raw DEK (used for cross-impl AEAD verification).
    public func open(dek: Data) throws -> [Account] {
        let plain = try XChaCha.open(try b64(payload.ct, "payload.ct"),
                                     key: dek, nonce: try b64(payload.nonce, "payload.nonce"))
        return try CanonicalJSON.decode(plain)
    }

    /// Decrypt with a passphrase, deriving the wrap key via the injected argon2.
    public func open(passphrase: String, argon2: Argon2idProvider) throws -> [Account] {
        let dek = try unwrap(passphrase: passphrase, argon2: argon2)
        return try open(dek: dek)
    }

    private func unwrap(passphrase: String, argon2: Argon2idProvider) throws -> Data {
        var found = false
        for w in wraps where w.type == "passphrase" {
            found = true
            guard let params = w.params, let saltStr = w.salt else { continue }
            let salt = try b64(saltStr, "wrap.salt")
            let key = try argon2.deriveKey(passphrase: Data(passphrase.utf8), salt: salt,
                                           memoryKiB: params.m, iterations: params.t,
                                           parallelism: params.p, keyLength: 32)
            if let dek = try? XChaCha.open(try b64(w.ct, "wrap.ct"), key: key,
                                           nonce: try b64(w.nonce, "wrap.nonce")) {
                return dek
            }
        }
        throw found ? VaultError.wrongPassphrase : VaultError.noPassphraseWrap
    }

    /// Strict standard-base64 decode (rejects base64url and missing padding).
    private func b64(_ s: String, _ field: String) throws -> Data {
        if s.contains("-") || s.contains("_") { throw VaultError.badBase64(field) }
        guard let d = Data(base64Encoded: s) else { throw VaultError.badBase64(field) }
        return d
    }
}
