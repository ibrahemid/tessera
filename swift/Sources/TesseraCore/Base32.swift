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

    /// Decodes base32: space, tab, CR, LF, `-` and `=` are stripped and the
    /// rest is uppercased (Go `strings.ToUpper`) before decoding. Input that
    /// cannot be re-encoded to itself is rejected: an empty secret, a truncated
    /// final group, or non-zero bits past the last whole byte all yield a key
    /// that does not round-trip, which would silently produce wrong codes.
    public static func decode(_ input: String) throws -> Data {
        var cleaned = String.UnicodeScalarView()
        for u in input.unicodeScalars {
            switch u {
            case " ", "\t", "\n", "\r", "-", "=": continue
            default: cleaned.append(GoText.upper(u))
            }
        }
        let clean = String(cleaned)
        if clean.isEmpty || badQuantum.contains(clean.utf8.count % 8) { throw invalidInput }
        var out = Data()
        var buffer = 0, bits = 0
        for u in clean.unicodeScalars {
            guard u.isASCII, let v = reverse[Character(u)] else { throw invalidInput }
            buffer = ((buffer << 5) | Int(v)) & 0xFFFF
            bits += 5
            if bits >= 8 {
                bits -= 8
                out.append(UInt8((buffer >> bits) & 0xff))
            }
        }
        if encodeNoPad(out) != clean { throw invalidInput }
        return out
    }

    private static let invalidInput = AccountError.invalid("base32x: invalid base32 input")
}
