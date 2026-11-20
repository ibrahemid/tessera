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

/// Envelope failures outside the `VaultError` set.
public enum EnvelopeError: Error, Equatable, Sendable, CustomStringConvertible, LocalizedError {
    case noWraps
    case tooManyWraps
    case appOnlyVault
    case invalidSecureEnclaveWrap

    public var description: String {
        switch self {
        case .noWraps: return "vault: envelope has no wraps (spec/vault-format.md requires at least one)"
        case .tooManyWraps: return "vault: envelope has more than 16 wraps"
        case .appOnlyVault:
            return "vault: openable only by the Tessera app on the Mac that created it (Secure Enclave); use the app's Settings > Export encrypted backup for a file the CLI can open"
        case .invalidSecureEnclaveWrap: return "vault: secure-enclave wrap is malformed"
        }
    }

    public var errorDescription: String? { description }
}

public struct Argon2Params: Codable, Sendable, Equatable {
    public var v: Int
    public var m: UInt32
    public var t: UInt32
    public var p: UInt8
    public init(v: Int, m: UInt32, t: UInt32, p: UInt8) { self.v = v; self.m = m; self.t = t; self.p = p }

    static let defaultCost = Argon2Params(v: 1, m: 131072, t: 3, p: 4)

    var isWithinBounds: Bool {
        t >= 1 && p >= 1 && p <= 16 && m >= 8 && m <= 1 << 20 && m >= 8 * UInt32(p)
            && UInt64(m) * UInt64(t) <= 4_194_304
    }

    var isBelowDefault: Bool {
        m < Self.defaultCost.m || t < Self.defaultCost.t || p < Self.defaultCost.p
    }
}

public struct VaultBox: Codable, Sendable {
    public var nonce: String
    public var ct: String
    public init(nonce: String, ct: String) { self.nonce = nonce; self.ct = ct }
}

/// The two ways the app stores a Secure Enclave wrap. `current` names a
/// keychain item by a 16-byte id under kdf `se-keychain-v1`; `legacy` has no
/// kdf and carries the key's `dataRepresentation` in `se_key`.
public enum SecureEnclaveWrapForm: Sendable, Equatable {
    case current
    case legacy
}

public struct VaultWrap: Sendable {
    public var type: String { didSet { raw = nil } }
    public var kdf: String? { didSet { raw = nil } }
    public var params: Argon2Params? { didSet { raw = nil } }
    public var salt: String? { didSet { raw = nil } }
    public var se_key: String? { didSet { raw = nil } }
    public var nonce: String { didSet { raw = nil } }
    public var ct: String { didSet { raw = nil } }
    /// The wrap as read, re-emitted verbatim until a field changes.
    var raw: [UInt8]?

    public init(type: String, kdf: String?, params: Argon2Params?, salt: String?,
                se_key: String?, nonce: String, ct: String) {
        self.type = type; self.kdf = kdf; self.params = params; self.salt = salt
        self.se_key = se_key; self.nonce = nonce; self.ct = ct
    }

    /// A current-form Secure Enclave wrap: `keyID` names the keychain item that
    /// holds the key, `nonce` and `ct` are the DEK sealed under the key it derives.
    public static func secureEnclave(keyID: Data, nonce: Data, ct: Data) throws -> VaultWrap {
        guard keyID.count == Envelope.secureEnclaveKeyIDLength, nonce.count == XChaCha.nonceSize, !ct.isEmpty else {
            throw EnvelopeError.invalidSecureEnclaveWrap
        }
        return VaultWrap(type: Envelope.secureEnclaveType, kdf: Envelope.secureEnclaveKDF, params: nil, salt: nil,
                         se_key: keyID.base64EncodedString(), nonce: nonce.base64EncodedString(),
                         ct: ct.base64EncodedString())
    }

    /// The Secure Enclave form of this wrap, or nil when it is not a
    /// well-formed secure-enclave wrap.
    public var secureEnclaveForm: SecureEnclaveWrapForm? {
        guard Envelope.bytesEqual(type, Envelope.secureEnclaveType),
              let k = se_key, let key = GoBase64.decodeCanonical(k), !key.isEmpty,
              let n = GoBase64.decodeCanonical(nonce), n.count == XChaCha.nonceSize,
              let c = GoBase64.decodeCanonical(ct), !c.isEmpty else { return nil }
        switch kdf {
        case nil, ""?: return .legacy
        case let k? where Envelope.bytesEqual(k, Envelope.secureEnclaveKDF):
            return key.count == Envelope.secureEnclaveKeyIDLength ? .current : nil
        default: return nil
        }
    }

    struct PassphraseMaterial {
        let salt: Data
        let params: Argon2Params
        let nonce: Data
        let ct: Data
    }

    /// The decoded inputs of a passphrase wrap, or nil when the wrap is malformed.
    var passphraseMaterial: PassphraseMaterial? {
        guard let k = kdf, Envelope.bytesEqual(k, Envelope.kdfName),
              let s = salt, let saltBytes = GoBase64.decodeCanonical(s), saltBytes.count == Envelope.saltLength,
              let p = params, p.isWithinBounds,
              let n = GoBase64.decodeCanonical(nonce), n.count == XChaCha.nonceSize,
              let c = GoBase64.decodeCanonical(ct) else { return nil }
        return PassphraseMaterial(salt: saltBytes, params: p, nonce: n, ct: c)
    }

    func marshal() -> [UInt8] {
        if let raw { return raw }
        var o = JSONObjectWriter()
        o.member("type", StrictJSON.quoted(type))
        if let kdf, !kdf.isEmpty { o.member("kdf", StrictJSON.quoted(kdf)) }
        if let p = params { o.member("params", Array("{\"v\":\(p.v),\"m\":\(p.m),\"t\":\(p.t),\"p\":\(p.p)}".utf8)) }
        if let salt, !salt.isEmpty { o.member("salt", StrictJSON.quoted(salt)) }
        if let se_key, !se_key.isEmpty { o.member("se_key", StrictJSON.quoted(se_key)) }
        o.member("nonce", StrictJSON.quoted(nonce))
        o.member("ct", StrictJSON.quoted(ct))
        return o.bytes
    }
}

struct JSONObjectWriter {
    private var out: [UInt8] = [0x7B]
    private var count = 0

    mutating func member(_ key: String, _ value: [UInt8]) {
        member(keyBytes: Array(key.utf8), value)
    }

    mutating func member(keyBytes: [UInt8], _ value: [UInt8]) {
        if count > 0 { out.append(0x2C) }
        count += 1
        StrictJSON.appendQuoted(keyBytes, to: &out)
        out.append(0x3A)
        out.append(contentsOf: value)
    }

    var bytes: [UInt8] { out + [0x7D] }
}

struct EnvelopeMember: Sendable {
    let key: [UInt8]
    let value: [UInt8]
}

public struct Envelope: Sendable {
    public var version: Int
    public var aead: String
    public var wraps: [VaultWrap]
    public var payload: VaultBox
    /// Top-level keys this version does not define, compacted, sorted by key bytes.
    var extras: [EnvelopeMember] = []
    /// True once an unlock replaced a below-default passphrase wrap; the
    /// envelope should be written back.
    public private(set) var needsPersist = false

    public init(version: Int, aead: String, wraps: [VaultWrap], payload: VaultBox) {
        self.version = version; self.aead = aead; self.wraps = wraps; self.payload = payload
    }

    static let aeadName = "xchacha20poly1305"
    static let kdfName = "argon2id"
    static let passphraseType = "passphrase"
    static let secureEnclaveType = "secure-enclave"
    static let secureEnclaveKDF = "se-keychain-v1"
    static let secureEnclaveKeyIDLength = 16
    static let dekLength = 32
    static let saltLength = 16
    static let maxWraps = 16
    static let maxPassphraseAttempts = 4
    private static let envelopeKeys: Set<[UInt8]> = Set(["version", "aead", "wraps", "payload"].map { Array($0.utf8) })

    static func bytesEqual(_ a: String, _ b: String) -> Bool { a.utf8.elementsEqual(b.utf8) }

    /// The wrap types in envelope order.
    public var wrapTypes: [String] { wraps.map(\.type) }

    // MARK: - Reading

    /// Parses an envelope under the strict rules of /spec/vault-format.md:
    /// exact-case keys, no duplicate keys, canonical base64, 24-byte nonces,
    /// 1 to 16 wraps, version 1 or 2, nesting up to 64 levels, no trailing
    /// data. Unknown top-level keys and wraps of unknown types are kept.
    public static func decode(_ data: Data) throws -> Envelope {
        let src = [UInt8](data)
        let root: JSONNode
        do {
            root = try StrictJSON.parse(src, rule: .finiteDouble)
        } catch {
            throw VaultError.corrupt
        }
        guard root.kind == .object else { throw VaultError.corrupt }
        guard let versionNode = root.member("version") else { throw VaultError.corrupt }
        let version: Int
        switch Array(src[versionNode.range]) {
        case [0x31]: version = 1
        case [0x32]: version = 2
        default: throw VaultError.unsupportedVersion
        }
        let aead = try requiredString(root, "aead")
        guard bytesEqual(aead, aeadName) else { throw VaultError.unsupportedAEAD }
        guard let wrapsNode = root.member("wraps"), wrapsNode.kind == .array else { throw VaultError.corrupt }
        if wrapsNode.items.isEmpty { throw EnvelopeError.noWraps }
        if wrapsNode.items.count > maxWraps { throw EnvelopeError.tooManyWraps }
        let wraps = try wrapsNode.items.map { try parseWrap($0, src) }
        guard let payloadNode = root.member("payload") else { throw VaultError.corrupt }
        let payload = try parsePayload(payloadNode)
        var env = Envelope(version: version, aead: aead, wraps: wraps, payload: payload)
        env.extras = root.members
            .filter { !envelopeKeys.contains($0.key) }
            .map { EnvelopeMember(key: $0.key, value: StrictJSON.compact(src[$0.value.range])) }
            .sorted { StrictJSON.bytesLess($0.key, $1.key) }
        return env
    }

    private static func parseWrap(_ node: JSONNode, _ src: [UInt8]) throws -> VaultWrap {
        guard node.kind == .object else { throw VaultError.corrupt }
        let type = try requiredString(node, "type")
        let raw = StrictJSON.compact(src[node.range])
        var w = VaultWrap(type: type, kdf: nil, params: nil, salt: nil, se_key: nil, nonce: "", ct: "")
        if bytesEqual(type, passphraseType) {
            w.kdf = try optionalString(node, "kdf")
            if let p = node.member("params") { w.params = try parseParams(p, src) }
            w.salt = try optionalBase64(node, "salt")
        } else if bytesEqual(type, secureEnclaveType) {
            w.kdf = try optionalString(node, "kdf")
            w.se_key = try requiredBase64(node, "se_key")
        } else {
            w.raw = raw
            return w
        }
        w.nonce = try requiredNonce(node, "nonce")
        w.ct = try requiredBase64(node, "ct")
        w.raw = raw
        return w
    }

    /// Non-integer values fail the envelope; missing or out-of-range values
    /// leave the wrap without params, which only fails when it is tried.
    private static func parseParams(_ node: JSONNode, _ src: [UInt8]) throws -> Argon2Params? {
        guard node.kind == .object else { throw VaultError.corrupt }
        var values: [String: Int64] = [:]
        var missing = false
        for key in ["v", "m", "t", "p"] {
            guard let n = node.member(key) else {
                missing = true
                continue
            }
            let text = src[n.range]
            guard n.kind == .number, StrictJSON.isCanonicalInt(text),
                  let value = Int64(String(decoding: text, as: UTF8.self)) else { throw VaultError.corrupt }
            values[key] = value
        }
        if missing { return nil }
        guard let v = Int(exactly: values["v", default: 0]),
              let m = UInt32(exactly: values["m", default: -1]),
              let t = UInt32(exactly: values["t", default: -1]),
              let p = UInt8(exactly: values["p", default: -1]) else { return nil }
        return Argon2Params(v: v, m: m, t: t, p: p)
    }

    private static func parsePayload(_ node: JSONNode) throws -> VaultBox {
        guard node.kind == .object else { throw VaultError.corrupt }
        let allowed: Set<[UInt8]> = [Array("nonce".utf8), Array("ct".utf8)]
        guard node.members.allSatisfy({ allowed.contains($0.key) }) else { throw VaultError.corrupt }
        return VaultBox(nonce: try requiredNonce(node, "nonce"), ct: try requiredBase64(node, "ct"))
    }

    private static func requiredString(_ node: JSONNode, _ key: String) throws -> String {
        guard let s = try optionalString(node, key) else { throw VaultError.corrupt }
        return s
    }

    private static func optionalString(_ node: JSONNode, _ key: String) throws -> String? {
        guard let n = node.member(key) else { return nil }
        guard n.kind == .string else { throw VaultError.corrupt }
        return String(decoding: n.string, as: UTF8.self)
    }

    private static func optionalBase64(_ node: JSONNode, _ key: String) throws -> String? {
        guard let n = node.member(key) else { return nil }
        guard n.kind == .string, GoBase64.decodeCanonical(n.string) != nil else { throw VaultError.corrupt }
        return String(decoding: n.string, as: UTF8.self)
    }

    private static func requiredBase64(_ node: JSONNode, _ key: String) throws -> String {
        guard let s = try optionalBase64(node, key) else { throw VaultError.corrupt }
        return s
    }

    private static func requiredNonce(_ node: JSONNode, _ key: String) throws -> String {
        let s = try requiredBase64(node, key)
        guard GoBase64.decodeCanonical(s.utf8)?.count == XChaCha.nonceSize else { throw VaultError.corrupt }
        return s
    }

    // MARK: - Writing

    /// The envelope as written to disk: version, aead, wraps, payload, then
    /// unknown top-level keys sorted by key, indented by two spaces. Wraps read
    /// from a file are written back byte for byte, compacted.
    public func encoded() throws -> Data {
        Data(StrictJSON.indent(try marshalCompact()))
    }

    func marshalCompact() throws -> [UInt8] {
        if wraps.isEmpty { throw EnvelopeError.noWraps }
        if wraps.count > Self.maxWraps { throw EnvelopeError.tooManyWraps }
        var wrapList: [UInt8] = [0x5B]
        for (i, w) in wraps.enumerated() {
            if i > 0 { wrapList.append(0x2C) }
            wrapList.append(contentsOf: w.marshal())
        }
        wrapList.append(0x5D)
        var box = JSONObjectWriter()
        box.member("nonce", StrictJSON.quoted(payload.nonce))
        box.member("ct", StrictJSON.quoted(payload.ct))
        var o = JSONObjectWriter()
        o.member("version", Array(String(version).utf8))
        o.member("aead", StrictJSON.quoted(aead))
        o.member("wraps", wrapList)
        o.member("payload", box.bytes)
        for e in extras { o.member(keyBytes: e.key, e.value) }
        return o.bytes
    }

    // MARK: - Opening

    /// Decrypt with a raw DEK. A payload that fails authentication reports
    /// `VaultError.wrongPassphrase`, as a failed passphrase does.
    public func open(dek: Data) throws -> [Account] {
        guard dek.count == Self.dekLength,
              let nonce = GoBase64.decodeCanonical(payload.nonce), nonce.count == XChaCha.nonceSize,
              let ct = GoBase64.decodeCanonical(payload.ct) else { throw VaultError.corrupt }
        var plain: Data
        do {
            plain = try XChaCha.open(ct, key: dek, nonce: nonce)
        } catch {
            throw VaultError.wrongPassphrase
        }
        defer { plain.resetBytes(in: 0..<plain.count) }
        return try CanonicalJSON.decode(plain)
    }

    /// Decrypt with a passphrase, deriving the wrap key via the injected
    /// argon2. Leaves a below-default wrap as it is; `unwrapDEK` upgrades it.
    public func open(passphrase: String, argon2: Argon2idProvider) throws -> [Account] {
        var dek = try unwrapWithPassphrase(passphrase, argon2: argon2).dek
        defer { dek.resetBytes(in: 0..<dek.count) }
        return try open(dek: dek)
    }

    /// Recover the raw DEK via a passphrase wrap, leaving the envelope unchanged.
    public func recoverDEK(passphrase: String, argon2: Argon2idProvider) throws -> Data {
        try unwrapWithPassphrase(passphrase, argon2: argon2).dek
    }

    /// Recover the raw DEK via a passphrase wrap. A wrap whose argon2 cost is
    /// below the default is replaced by a default-cost wrap under the same
    /// passphrase and DEK, and `needsPersist` becomes true.
    public mutating func unwrapDEK(passphrase: String, argon2: Argon2idProvider) throws -> Data {
        let (dek, index) = try unwrapWithPassphrase(passphrase, argon2: argon2)
        if let p = wraps[index].params, p.isBelowDefault,
           let upgraded = try? Self.makePassphraseWrap(dek: dek, passphrase: passphrase, argon2: argon2) {
            wraps[index] = upgraded
            needsPersist = true
        }
        return dek
    }

    /// Tries passphrase wraps in envelope order, skipping malformed ones and
    /// deriving at most four keys. Returns the DEK and the index of the wrap
    /// that opened.
    func unwrapWithPassphrase(_ passphrase: String, argon2: Argon2idProvider) throws -> (dek: Data, index: Int) {
        guard version == 1 || version == 2 else { throw VaultError.unsupportedVersion }
        guard Self.bytesEqual(aead, Self.aeadName) else { throw VaultError.unsupportedAEAD }
        var secret = Data(passphrase.utf8)
        defer { secret.resetBytes(in: 0..<secret.count) }
        var found = false
        var malformed = false
        var attempts = 0
        for (i, w) in wraps.enumerated() where Self.bytesEqual(w.type, Self.passphraseType) {
            found = true
            guard let m = w.passphraseMaterial else {
                malformed = true
                continue
            }
            if attempts == Self.maxPassphraseAttempts { break }
            attempts += 1
            var key = try argon2.deriveKey(passphrase: secret, salt: m.salt, memoryKiB: m.params.m,
                                           iterations: m.params.t, parallelism: m.params.p,
                                           keyLength: Self.dekLength)
            defer { key.resetBytes(in: 0..<key.count) }
            if let dek = try? XChaCha.open(m.ct, key: key, nonce: m.nonce) { return (dek, i) }
        }
        if !found {
            if wraps.contains(where: { Self.bytesEqual($0.type, Self.secureEnclaveType) }) {
                throw EnvelopeError.appOnlyVault
            }
            throw VaultError.noPassphraseWrap
        }
        if attempts == 0 && malformed { throw VaultError.corrupt }
        throw VaultError.wrongPassphrase
    }

    // MARK: - Creating and updating

    /// Create a new envelope: random DEK, sealed payload, single passphrase wrap.
    public static func create(accounts: [Account], passphrase: String, argon2: Argon2idProvider) throws -> Envelope {
        for a in accounts { try a.validate() }
        var dek = randomBytes(dekLength)
        defer { dek.resetBytes(in: 0..<dek.count) }
        let payload = try sealPayload(accounts, dek: dek)
        let wrap = try makePassphraseWrap(dek: dek, passphrase: passphrase, argon2: argon2)
        return Envelope(version: Account.formatVersion(accounts), aead: aeadName, wraps: [wrap], payload: payload)
    }

    /// Create an envelope with a fresh DEK and no wraps, returning the DEK so the
    /// caller can attach a platform wrap (e.g. Secure Enclave) before saving. The
    /// returned envelope is transient until at least one wrap is added.
    public static func createUnwrapped(accounts: [Account]) throws -> (Envelope, Data) {
        for a in accounts { try a.validate() }
        let dek = randomBytes(dekLength)
        let env = Envelope(version: Account.formatVersion(accounts), aead: aeadName, wraps: [],
                           payload: try sealPayload(accounts, dek: dek))
        return (env, dek)
    }

    /// Whether the envelope carries an argon2id passphrase wrap — the wrap the
    /// tess CLI opens, and the app's fallback when a Secure Enclave key is gone.
    public var hasPassphraseWrap: Bool { wraps.contains { Self.bytesEqual($0.type, Self.passphraseType) } }

    /// Add (or replace) the argon2id passphrase wrap for a DEK the caller already
    /// holds, leaving every other wrap and the payload untouched. This is how the
    /// app attaches a recovery passphrase to a Secure-Enclave-only vault; the wire
    /// format matches the Go core's `newPassphraseWrap` exactly.
    public mutating func setPassphraseWrap(dek: Data, passphrase: String, argon2: Argon2idProvider) throws {
        let wrap = try Self.makePassphraseWrap(dek: dek, passphrase: passphrase, argon2: argon2)
        var next = wraps.filter { !Self.bytesEqual($0.type, Self.passphraseType) }
        next.append(wrap)
        if next.count > Self.maxWraps { throw EnvelopeError.tooManyWraps }
        wraps = next
    }

    /// Re-seal the payload with a new account list, reusing the existing DEK so
    /// all wraps (including a Touch ID secure-enclave wrap) are preserved.
    public func reseal(accounts: [Account], passphrase: String, argon2: Argon2idProvider) throws -> Envelope {
        var dek = try recoverDEK(passphrase: passphrase, argon2: argon2)
        defer { dek.resetBytes(in: 0..<dek.count) }
        return try reseal(accounts: accounts, dek: dek)
    }

    /// Re-seal the payload with a known DEK (held after any successful unlock,
    /// including Touch ID). Lets the app mutate accounts without the passphrase.
    /// The version follows the accounts: 2 when any uses a version 2 key.
    public func reseal(accounts: [Account], dek: Data) throws -> Envelope {
        for a in accounts { try a.validate() }
        var copy = self
        copy.payload = try Self.sealPayload(accounts, dek: dek)
        copy.version = Account.formatVersion(accounts)
        return copy
    }

    // MARK: - Rotation

    /// Moves the vault to a fresh DEK: reseals `accounts` under it and replaces
    /// every wrap with one default-cost passphrase wrap per entry of
    /// `passphrases`, in order, followed by the wraps `secureEnclaveWraps`
    /// builds for the new DEK. Unknown top-level keys are kept.
    ///
    /// `recreating` holds the indices of current wraps the new wraps stand in
    /// for. `removed` lists, in envelope order and without merging repeats, the
    /// type of every current wrap whose index it does not hold. Nothing changes
    /// when the result would have no wraps or more than 16.
    @discardableResult
    public mutating func rotate(accounts: [Account], passphrases: [String], recreating: Set<Int> = [],
                                argon2: Argon2idProvider,
                                secureEnclaveWraps: (Data) throws -> [VaultWrap] = { _ in [] })
        throws -> (dek: Data, removed: [String]) {
        for a in accounts { try a.validate() }
        if passphrases.count > Self.maxWraps { throw EnvelopeError.tooManyWraps }
        let dek = Self.randomBytes(Self.dekLength)
        let sealed = try Self.sealPayload(accounts, dek: dek)
        var next = try passphrases.map { try Self.makePassphraseWrap(dek: dek, passphrase: $0, argon2: argon2) }
        let enclave = try secureEnclaveWraps(dek)
        guard enclave.allSatisfy({ $0.secureEnclaveForm != nil }) else { throw EnvelopeError.invalidSecureEnclaveWrap }
        next.append(contentsOf: enclave)
        if next.isEmpty { throw EnvelopeError.noWraps }
        if next.count > Self.maxWraps { throw EnvelopeError.tooManyWraps }
        let removed = wraps.indices.filter { !recreating.contains($0) }.map { wraps[$0].type }
        version = Account.formatVersion(accounts)
        aead = Self.aeadName
        wraps = next
        payload = sealed
        needsPersist = false
        return (dek, removed)
    }

    /// A passphrase change: opens with `old`, then rotates to a fresh DEK
    /// wrapped by `new` alone. Returns the types of the wraps it removed.
    @discardableResult
    public mutating func rotatePassphrase(old: String, new: String, argon2: Argon2idProvider) throws -> [String] {
        var (oldDEK, index) = try unwrapWithPassphrase(old, argon2: argon2)
        defer { oldDEK.resetBytes(in: 0..<oldDEK.count) }
        let accounts = try open(dek: oldDEK)
        var result = try rotate(accounts: accounts, passphrases: [new], recreating: [index], argon2: argon2)
        result.dek.resetBytes(in: 0..<result.dek.count)
        return result.removed
    }

    /// Rotates to a fresh DEK and keeps the passphrase.
    @discardableResult
    public mutating func rekey(passphrase: String, argon2: Argon2idProvider) throws -> [String] {
        try rotatePassphrase(old: passphrase, new: passphrase, argon2: argon2)
    }

    // MARK: - Secure Enclave wraps

    /// Whether the envelope carries any secure-enclave wrap.
    public var hasSecureEnclaveWrap: Bool { wraps.contains { Self.bytesEqual($0.type, Self.secureEnclaveType) } }

    /// The well-formed secure-enclave wraps of `form`, or of either form when nil, in envelope order.
    public func secureEnclaveWraps(_ form: SecureEnclaveWrapForm? = nil) -> [VaultWrap] {
        wraps.filter { w in w.secureEnclaveForm.map { form == nil || $0 == form } ?? false }
    }

    /// Appends a secure-enclave wrap for the current DEK.
    public mutating func addSecureEnclaveWrap(_ wrap: VaultWrap) throws {
        guard wrap.secureEnclaveForm != nil else { throw EnvelopeError.invalidSecureEnclaveWrap }
        if wraps.count >= Self.maxWraps { throw EnvelopeError.tooManyWraps }
        wraps.append(wrap)
    }

    /// Removes the secure-enclave wraps of `form` (every secure-enclave wrap
    /// when nil) and appends `wrap`, in one change.
    public mutating func replaceSecureEnclaveWraps(_ form: SecureEnclaveWrapForm?, with wrap: VaultWrap) throws {
        guard wrap.secureEnclaveForm != nil else { throw EnvelopeError.invalidSecureEnclaveWrap }
        var next = wraps.filter { !Self.matches($0, form) }
        next.append(wrap)
        if next.count > Self.maxWraps { throw EnvelopeError.tooManyWraps }
        wraps = next
    }

    /// Removes the secure-enclave wraps of `form` (every secure-enclave wrap
    /// when nil) and returns how many it removed. Refuses to remove the last wrap.
    @discardableResult
    public mutating func removeSecureEnclaveWraps(_ form: SecureEnclaveWrapForm? = nil) throws -> Int {
        let next = wraps.filter { !Self.matches($0, form) }
        if next.isEmpty && !wraps.isEmpty { throw EnvelopeError.noWraps }
        let removed = wraps.count - next.count
        wraps = next
        return removed
    }

    private static func matches(_ w: VaultWrap, _ form: SecureEnclaveWrapForm?) -> Bool {
        guard bytesEqual(w.type, secureEnclaveType) else { return false }
        guard let form else { return true }
        return w.secureEnclaveForm == form
    }

    // MARK: - Primitives

    private static func sealPayload(_ accounts: [Account], dek: Data) throws -> VaultBox {
        var plain = CanonicalJSON.encode(accounts)
        defer { plain.resetBytes(in: 0..<plain.count) }
        let nonce = randomBytes(XChaCha.nonceSize)
        let ct = try XChaCha.seal(plain, key: dek, nonce: nonce)
        return VaultBox(nonce: nonce.base64EncodedString(), ct: ct.base64EncodedString())
    }

    private static func makePassphraseWrap(dek: Data, passphrase: String, argon2: Argon2idProvider) throws -> VaultWrap {
        let params = Argon2Params.defaultCost
        let salt = randomBytes(saltLength)
        var secret = Data(passphrase.utf8)
        defer { secret.resetBytes(in: 0..<secret.count) }
        var key = try argon2.deriveKey(passphrase: secret, salt: salt,
                                       memoryKiB: params.m, iterations: params.t,
                                       parallelism: params.p, keyLength: dekLength)
        defer { key.resetBytes(in: 0..<key.count) }
        let nonce = randomBytes(XChaCha.nonceSize)
        let ct = try XChaCha.seal(dek, key: key, nonce: nonce)
        return VaultWrap(type: passphraseType, kdf: kdfName, params: params,
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
}
