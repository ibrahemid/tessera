import Foundation

/// Parses Google Authenticator's `otpauth-migration://` export. The protobuf
/// MigrationPayload is decoded directly from the wire format (no codegen), the
/// same approach as the Go core, so both stay byte-compatible.
public enum Migration {

    public static func parse(_ uri: String) throws -> [Account] {
        guard let comps = URLComponents(string: uri.trimmingCharacters(in: .whitespacesAndNewlines)),
              comps.scheme == "otpauth-migration", (comps.host ?? "") == "offline" else {
            throw AccountError.invalid("not an otpauth-migration uri")
        }
        guard let dataParam = (comps.queryItems ?? []).first(where: { $0.name == "data" })?.value else {
            throw AccountError.invalid("missing data parameter")
        }
        guard let payload = Data(base64Encoded: dataParam) else {
            throw AccountError.invalid("data is not valid base64")
        }
        var reader = WireReader(payload)
        var accounts: [Account] = []
        while !reader.isAtEnd {
            let (field, wire) = try reader.readTag()
            if field == 1 && wire == 2 {
                let sub = try reader.readLengthDelimited()
                accounts.append(try parseOtpParameters(sub))
            } else {
                try reader.skip(wire)
            }
        }
        if accounts.isEmpty { throw AccountError.invalid("no otp parameters in payload") }
        return accounts
    }

    private static func parseOtpParameters(_ data: Data) throws -> Account {
        var reader = WireReader(data)
        var secret = Data(), name = "", issuer = ""
        var algorithm = "SHA1", digits = 6, counter: Int64 = 0
        var type: OTPType = .totp
        while !reader.isAtEnd {
            let (field, wire) = try reader.readTag()
            switch (field, wire) {
            case (1, 2): secret = try reader.readLengthDelimited()
            case (2, 2): name = String(decoding: try reader.readLengthDelimited(), as: UTF8.self)
            case (3, 2): issuer = String(decoding: try reader.readLengthDelimited(), as: UTF8.self)
            case (4, 0):
                switch try reader.readVarint() {
                case 1: algorithm = "SHA1"
                case 2: algorithm = "SHA256"
                case 3: algorithm = "SHA512"
                case 4: throw AccountError.invalid("MD5 algorithm is not supported")
                default: algorithm = "SHA1"
                }
            case (5, 0): digits = (try reader.readVarint() == 2) ? 8 : 6
            case (6, 0): type = (try reader.readVarint() == 1) ? .hotp : .totp
            case (7, 0): counter = Int64(bitPattern: try reader.readVarint())
            default: try reader.skip(wire)
            }
        }
        if secret.isEmpty { throw AccountError.invalid("migration entry has empty secret") }

        var iss = issuer
        var acct = name
        if let r = name.range(of: ":") {
            if iss.isEmpty { iss = String(name[..<r.lowerBound]) }
            acct = String(name[r.upperBound...])
        }
        if iss.lowercased() == "steam" { type = .steam }
        return Account(id: "", type: type, issuer: iss, account: acct, secret: secret,
                       algorithm: algorithm, digits: digits, period: 30, counter: counter)
    }
}

/// Minimal protobuf wire-format reader (varint + length-delimited + skip).
private struct WireReader {
    private let bytes: [UInt8]
    private var pos = 0
    init(_ data: Data) { bytes = [UInt8](data) }
    var isAtEnd: Bool { pos >= bytes.count }

    mutating func readVarint() throws -> UInt64 {
        var result: UInt64 = 0, shift: UInt64 = 0
        while pos < bytes.count {
            let b = bytes[pos]; pos += 1
            result |= UInt64(b & 0x7f) << shift
            if b & 0x80 == 0 { return result }
            shift += 7
            if shift >= 64 { throw AccountError.invalid("varint overflow") }
        }
        throw AccountError.invalid("truncated varint")
    }

    mutating func readTag() throws -> (field: Int, wire: Int) {
        let tag = try readVarint()
        return (Int(tag >> 3), Int(tag & 0x7))
    }

    mutating func readLengthDelimited() throws -> Data {
        let len = Int(try readVarint())
        guard pos + len <= bytes.count else { throw AccountError.invalid("truncated length-delimited field") }
        let slice = bytes[pos..<(pos + len)]
        pos += len
        return Data(slice)
    }

    mutating func skip(_ wire: Int) throws {
        switch wire {
        case 0: _ = try readVarint()
        case 2: _ = try readLengthDelimited()
        case 5: pos += 4
        case 1: pos += 8
        default: throw AccountError.invalid("unsupported wire type \(wire)")
        }
        if pos > bytes.count { throw AccountError.invalid("truncated field") }
    }
}
