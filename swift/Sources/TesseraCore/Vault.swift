import Foundation

/// Argon2id is the one primitive CryptoKit lacks. The core depends on this
/// provider abstraction; the app/CI inject a vetted argon2 implementation.
public protocol Argon2idProvider: Sendable {
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
    public init(v: Int, m: UInt32, t: UInt32, p: UInt8) { self.v = v; self.m = m; self.t = t; self.p = p }
}

public struct VaultBox: Codable, Sendable {
    public var nonce: String
    public var ct: String
    public init(nonce: String, ct: String) { self.nonce = nonce; self.ct = ct }
}

public struct VaultWrap: Codable, Sendable {
    public var type: String
    public var kdf: String?
    public var params: Argon2Params?
    public var salt: String?
    public var se_key: String?
    public var nonce: String
    public var ct: String
    public init(type: String, kdf: String?, params: Argon2Params?, salt: String?,
                se_key: String?, nonce: String, ct: String) {
        self.type = type; self.kdf = kdf; self.params = params; self.salt = salt
        self.se_key = se_key; self.nonce = nonce; self.ct = ct
    }
}

public struct Envelope: Codable, Sendable {
    public var version: Int
    public var aead: String
    public var wraps: [VaultWrap]
    public var payload: VaultBox
    public init(version: Int, aead: String, wraps: [VaultWrap], payload: VaultBox) {
        self.version = version; self.aead = aead; self.wraps = wraps; self.payload = payload
    }

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

    /// Create a new envelope: random DEK, sealed payload, single passphrase wrap.
    public static func create(accounts: [Account], passphrase: String, argon2: Argon2idProvider) throws -> Envelope {
        for a in accounts { try a.validate() }
        let dek = randomBytes(32)
        let payloadNonce = randomBytes(XChaCha.nonceSize)
        let payloadCT = try XChaCha.seal(CanonicalJSON.encode(accounts), key: dek, nonce: payloadNonce)
        let wrap = try makePassphraseWrap(dek: dek, passphrase: passphrase, argon2: argon2)
        return Envelope(version: 1, aead: "xchacha20poly1305", wraps: [wrap],
                        payload: VaultBox(nonce: payloadNonce.base64EncodedString(),
                                          ct: payloadCT.base64EncodedString()))
    }

    /// Recover the raw DEK via a passphrase wrap (for re-sealing the payload).
    public func recoverDEK(passphrase: String, argon2: Argon2idProvider) throws -> Data {
        try unwrap(passphrase: passphrase, argon2: argon2)
    }

    /// Re-seal the payload with a new account list, reusing the existing DEK so
    /// all wraps (including a Touch ID secure-enclave wrap) are preserved.
    public func reseal(accounts: [Account], passphrase: String, argon2: Argon2idProvider) throws -> Envelope {
        for a in accounts { try a.validate() }
        let dek = try recoverDEK(passphrase: passphrase, argon2: argon2)
        let nonce = Self.randomBytes(XChaCha.nonceSize)
        let ct = try XChaCha.seal(CanonicalJSON.encode(accounts), key: dek, nonce: nonce)
        var copy = self
        copy.payload = VaultBox(nonce: nonce.base64EncodedString(), ct: ct.base64EncodedString())
        return copy
    }

    private static func makePassphraseWrap(dek: Data, passphrase: String, argon2: Argon2idProvider) throws -> VaultWrap {
        let params = Argon2Params(v: 1, m: 131072, t: 3, p: 4)
        let salt = randomBytes(16)
        let key = try argon2.deriveKey(passphrase: Data(passphrase.utf8), salt: salt,
                                       memoryKiB: params.m, iterations: params.t,
                                       parallelism: params.p, keyLength: 32)
        let nonce = randomBytes(XChaCha.nonceSize)
        let ct = try XChaCha.seal(dek, key: key, nonce: nonce)
        return VaultWrap(type: "passphrase", kdf: "argon2id", params: params,
                         salt: salt.base64EncodedString(), se_key: nil,
                         nonce: nonce.base64EncodedString(), ct: ct.base64EncodedString())
    }

    static func randomBytes(_ n: Int) -> Data {
        // SystemRandomNumberGenerator is a CSPRNG on Apple platforms.
        var rng = SystemRandomNumberGenerator()
        var d = Data(count: n)
        for i in 0..<n { d[i] = UInt8.random(in: 0...255, using: &rng) }
        return d
    }

    /// Strict standard-base64 decode (rejects base64url and missing padding).
    private func b64(_ s: String, _ field: String) throws -> Data {
        if s.contains("-") || s.contains("_") { throw VaultError.badBase64(field) }
        guard let d = Data(base64Encoded: s) else { throw VaultError.badBase64(field) }
        return d
    }
}
