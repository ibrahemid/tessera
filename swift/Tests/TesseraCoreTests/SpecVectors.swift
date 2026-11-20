import Foundation
@testable import TesseraCore

struct VectorMissing: Error, CustomStringConvertible {
    let description: String
}

/// A value inside /spec/testvectors.json, read through the strict parser so
/// strings and numbers keep the exact bytes Go decodes.
struct VectorNode {
    let node: JSONNode
    let source: [UInt8]

    static func specFile(_ name: String) throws -> URL {
        var dir = URL(fileURLWithPath: #filePath)
        for _ in 0..<8 {
            dir.deleteLastPathComponent()
            let candidate = dir.appendingPathComponent("spec").appendingPathComponent(name)
            if FileManager.default.fileExists(atPath: candidate.path) { return candidate }
        }
        throw VectorMissing(description: "spec/\(name) not found")
    }

    static func root() throws -> VectorNode {
        let bytes = [UInt8](try Data(contentsOf: specFile("testvectors.json")))
        return VectorNode(node: try StrictJSON.parse(bytes, rule: .finiteDouble), source: bytes)
    }

    subscript(_ key: String) -> VectorNode? {
        node.member(key).map { VectorNode(node: $0, source: source) }
    }

    func get(_ key: String) throws -> VectorNode {
        guard let n = self[key] else { throw VectorMissing(description: "vector key \(key) missing") }
        return n
    }

    var items: [VectorNode] { node.items.map { VectorNode(node: $0, source: source) } }
    var text: String { String(decoding: node.string, as: UTF8.self) }
    var data: Data { Data(node.string) }
    var raw: [UInt8] { Array(source[node.range]) }
    var texts: [String] { items.map(\.text) }

    func int() throws -> Int64 {
        guard let v = Int64(String(decoding: raw, as: UTF8.self)) else {
            throw VectorMissing(description: "vector value is not an integer")
        }
        return v
    }
}

func envelopeErrorClass(_ error: Error) -> String {
    switch error {
    case VaultError.corrupt: return "corrupt"
    case VaultError.wrongPassphrase: return "auth_failed"
    case VaultError.unsupportedVersion: return "unsupported_version"
    case VaultError.unsupportedAEAD: return "unsupported_aead"
    case VaultError.noPassphraseWrap: return "no_passphrase_wrap"
    case EnvelopeError.noWraps: return "no_wraps"
    case EnvelopeError.tooManyWraps: return "too_many_wraps"
    case EnvelopeError.appOnlyVault: return "app_only"
    default: return "other (\(error))"
    }
}

func payloadErrorClass(_ error: Error) -> String {
    switch error {
    case PayloadError.malformed: return "malformed_payload"
    case PayloadError.duplicateKey: return "duplicate_key"
    case PayloadError.nonCanonicalNumber: return "non_canonical_number"
    case PayloadError.fieldType: return "field_type"
    case PayloadError.nonCanonicalBase64: return "non_canonical_base64"
    case PayloadError.counterRange: return "counter_range"
    default: return "other (\(error))"
    }
}

/// Compacted top-level member `key` of an envelope.
func envelopeMember(_ envelope: [UInt8], _ key: String) throws -> [UInt8]? {
    let root = try StrictJSON.parse(envelope, rule: .finiteDouble)
    return root.member(key).map { StrictJSON.compact(envelope[$0.range]) }
}

/// Compacted wraps of `type`, in envelope order.
func envelopeWraps(_ envelope: [UInt8], ofType type: String) throws -> [[UInt8]] {
    let root = try StrictJSON.parse(envelope, rule: .finiteDouble)
    let wraps = root.member("wraps")?.items ?? []
    return wraps.filter { $0.member("type")?.string == Array(type.utf8) }
        .map { StrictJSON.compact(envelope[$0.range]) }
}

/// A fast stand-in for argon2id: fine for envelope logic, never for vectors
/// sealed by Go.
struct XorArgon2: Argon2idProvider {
    func deriveKey(passphrase: Data, salt: Data, memoryKiB: UInt32, iterations: UInt32,
                   parallelism: UInt8, keyLength: Int) throws -> Data {
        let p = [UInt8](passphrase), s = [UInt8](salt)
        var out = [UInt8](repeating: 0, count: keyLength)
        for i in 0..<keyLength {
            out[i] = (p.isEmpty ? 0 : p[i % p.count]) ^ (s.isEmpty ? 0 : s[i % s.count]) ^ UInt8(i & 0xff)
        }
        return Data(out)
    }
}

/// Counts derivations, to observe the passphrase attempt limit.
final class CountingArgon2: Argon2idProvider, @unchecked Sendable {
    private let lock = NSLock()
    private var calls = 0
    private let inner = XorArgon2()

    var count: Int { lock.withLock { calls } }

    func deriveKey(passphrase: Data, salt: Data, memoryKiB: UInt32, iterations: UInt32,
                   parallelism: UInt8, keyLength: Int) throws -> Data {
        lock.withLock { calls += 1 }
        return try inner.deriveKey(passphrase: passphrase, salt: salt, memoryKiB: memoryKiB,
                                   iterations: iterations, parallelism: parallelism, keyLength: keyLength)
    }
}
