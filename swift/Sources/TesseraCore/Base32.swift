import Foundation

/// RFC 4648 base32 with lenient decoding (case-insensitive, optional padding,
/// embedded whitespace tolerated). Used only at the otpauth import/export
/// boundary; the vault stores raw bytes.
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

    public static func decode(_ input: String) throws -> Data {
        var cleaned = ""
        for ch in input.uppercased() {
            if ch == " " || ch == "\t" || ch == "\n" || ch == "\r" || ch == "-" || ch == "=" { continue }
            cleaned.append(ch)
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
        return out
    }
}
