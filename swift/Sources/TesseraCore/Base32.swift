import Foundation

/// RFC 4648 base32. Decoding is lenient about how a key is written down
/// (case-insensitive, optional padding, embedded whitespace and dashes
/// tolerated) and strict about what it means: input that does not re-encode to
/// itself is rejected rather than turned into a secret the user was never given.
/// Mirrors Go `internal/base32x` exactly. Used only at the otpauth
/// import/export boundary; the vault stores raw bytes.
public enum Base32 {
    private static let alphabet = Array("ABCDEFGHIJKLMNOPQRSTUVWXYZ234567")
    private static let reverse: [Character: UInt8] = {
        var m: [Character: UInt8] = [:]
        for (i, c) in alphabet.enumerated() { m[c] = UInt8(i) }
        return m
    }()

    public static func encode(_ data: Data) -> String {
        var out = ""
        var buffer = 0, bits = 0
        for byte in data {
            buffer = (buffer << 8) | Int(byte)
            bits += 8
            while bits >= 5 {
                bits -= 5
                out.append(alphabet[(buffer >> bits) & 0x1f])
            }
        }
        if bits > 0 {
            out.append(alphabet[(buffer << (5 - bits)) & 0x1f])
        }
        while out.count % 8 != 0 { out.append("=") }
        return out
    }

    public static func encodeNoPad(_ data: Data) -> String {
        var s = encode(data)
        while s.hasSuffix("=") { s.removeLast() }
        return s
    }

    /// Group lengths that no byte count can produce: a base32 quantum of 8
    /// characters encodes 5 bytes, so a trailing group of 1, 3, or 6 characters
    /// is always truncated input.
    private static let badQuantum: Set<Int> = [1, 3, 6]

    /// Decodes base32: whitespace, `-` and `=` padding are stripped and the
    /// input is uppercased before decoding. Input that cannot be re-encoded to
    /// itself is rejected — an empty secret, a truncated final group, or
    /// non-zero bits past the last whole byte all yield a key that does not
    /// round-trip, which would silently produce wrong codes forever.
    public static func decode(_ input: String) throws -> Data {
        var cleaned = ""
        for ch in input.uppercased() {
            if ch == " " || ch == "\t" || ch == "\n" || ch == "\r" || ch == "-" || ch == "=" { continue }
            cleaned.append(ch)
        }
        if cleaned.isEmpty {
            throw AccountError.invalid("base32: empty input")
        }
        if badQuantum.contains(cleaned.count % 8) {
            throw AccountError.invalid("base32: truncated final group")
        }
        var out = Data()
        var buffer = 0, bits = 0
        for ch in cleaned {
            guard let v = reverse[ch] else {
                throw AccountError.invalid("base32: invalid character \(ch)")
            }
            buffer = (buffer << 5) | Int(v)
            bits += 5
            if bits >= 8 {
                bits -= 8
                out.append(UInt8((buffer >> bits) & 0xff))
            }
        }
        // Non-canonical trailing bits decode without error but do not re-encode
        // to the input; such a secret is not the secret the user was given.
        if encodeNoPad(out) != cleaned {
            throw AccountError.invalid("base32: non-canonical trailing bits")
        }
        return out
    }
}
