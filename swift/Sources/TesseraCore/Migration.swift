import Foundation

/// Reads and writes Google Authenticator's `otpauth-migration://` transfer
/// links. The protobuf MigrationPayload is handled directly on the wire format
/// (no codegen) with the bounds, rejections and error text of the Go core.
public enum Migration {

    /// One account the transfer format cannot carry, why, and the kind of
    /// reason. Carries no secret.
    public typealias Skipped = Exporters.Skipped

    /// The batches of a transfer export: each payload is one QR code's
    /// MigrationPayload, and `uris[i]` is the link that encodes `payloads[i]`.
    public struct Export: Equatable, Sendable {
        public let payloads: [Data]
        public let uris: [String]
        public let skipped: [Skipped]
    }

    static let maxAccountsPerBatch = 10
    static let maxPayloadBytes = 800

    public static func parse(_ uri: String) throws -> [Account] {
        let u: GoURL
        do { u = try GoURL.parse(GoText.trimSpace(uri)) } catch {
            throw AccountError.invalid("migration: invalid URI")
        }
        guard u.scheme == "otpauth-migration" else {
            throw AccountError.invalid(
                "migration: not an otpauth-migration uri (scheme \(GoText.quoteValue(u.scheme)))")
        }
        guard GoText.equalFold(GoText.string(u.host), ascii: "offline") else {
            throw AccountError.invalid(
                "migration: unexpected host; the link must start with otpauth-migration://offline?")
        }
        guard let data = rawQueryValue(u.rawQuery, "data") else {
            throw AccountError.invalid("migration: missing data query parameter")
        }
        if data.isEmpty { throw AccountError.invalid("migration: empty data query parameter") }

        let raw: [UInt8]
        do { raw = try decodeBase64(data) } catch let e as Base64Error {
            throw AccountError.invalid("migration: base64-decode data: \(e.message)")
        }
        let params = try decodePayload(raw[...])
        if params.isEmpty {
            throw AccountError.invalid("migration: payload contained no otp parameters")
        }
        return try params.enumerated().map { i, p in
            do { return try p.toAccount() } catch let ParamError.invalid(m) {
                throw AccountError.invalid("migration: otp parameter \(i): \(m)")
            }
        }
    }

    /// Splits `accounts` into transfer batches: at most ten accounts and 800
    /// payload bytes each, every batch sharing `batchID`. Accounts the format
    /// cannot carry (Steam, MD5, a digit count other than 6 or 8, a TOTP period
    /// other than 30 seconds, an empty secret) are listed in `skipped` instead.
    public static func export(_ accounts: [Account],
                              batchID: Int32 = Int32.random(in: .min ... .max)) -> Export {
        var encoded: [[UInt8]] = []
        var skipped: [Skipped] = []
        for a in accounts {
            switch encodeOtpParameters(a) {
            case .success(let params): encoded.append(params)
            case .failure(let reason):
                skipped.append(Skipped(label: Exporters.label(a), reason: reason.message, kind: reason.kind))
            }
        }
        let batches = partitionBatches(encoded)
        var payloads: [Data] = []
        var uris: [String] = []
        for (i, batch) in batches.enumerated() {
            let payload = assemblePayload(batch, batchSize: batches.count, batchIndex: i, batchID: batchID)
            payloads.append(Data(payload))
            uris.append("otpauth-migration://offline?data="
                + GoURL.escape(Data(payload).base64EncodedString(), .queryComponent))
        }
        return Export(payloads: payloads, uris: uris, skipped: skipped)
    }

    // MARK: - Reading

    private struct Base64Error: Error {
        let offset: Int
        var message: String { "illegal base64 data at input byte \(offset)" }
    }

    private enum ParamError: Error { case invalid(String) }

    /// Go `rawQueryValue`: the first pair whose raw key is `name`, its value
    /// path-unescaped. A bad escape counts as absent.
    private static func rawQueryValue(_ rawQuery: [UInt8], _ name: String) -> [UInt8]? {
        let key = Array(name.utf8)
        var rest = rawQuery[...]
        while !rest.isEmpty {
            let (pair, after, _) = GoURL.cut(rest, UInt8(ascii: "&"))
            rest = after
            if pair.isEmpty { continue }
            let (k, v, _) = GoURL.cut(pair, UInt8(ascii: "="))
            if !k.elementsEqual(key) { continue }
            return try? GoURL.unescape(Array(v), .pathSegment)
        }
        return nil
    }

    /// Go `base64.StdEncoding.DecodeString`: padded, non-strict, CR and LF
    /// ignored, the error offset as Go reports it.
    private static func decodeBase64(_ src: [UInt8]) throws -> [UInt8] {
        var out: [UInt8] = []
        var si = 0
        while si < src.count { si = try decodeQuantum(src, si, &out) }
        return out
    }

    private static func base64Value(_ c: UInt8) -> UInt8? {
        switch c {
        case UInt8(ascii: "A")...UInt8(ascii: "Z"): return c - UInt8(ascii: "A")
        case UInt8(ascii: "a")...UInt8(ascii: "z"): return c - UInt8(ascii: "a") + 26
        case UInt8(ascii: "0")...UInt8(ascii: "9"): return c - UInt8(ascii: "0") + 52
        case UInt8(ascii: "+"): return 62
        case UInt8(ascii: "/"): return 63
        default: return nil
        }
    }

    private static func isNewline(_ c: UInt8) -> Bool { c == 0x0A || c == 0x0D }

    private static func decodeQuantum(_ src: [UInt8], _ start: Int, _ out: inout [UInt8]) throws -> Int {
        let pad = UInt8(ascii: "=")
        var si = start
        var dbuf = [UInt8](repeating: 0, count: 4)
        var dlen = 4
        var j = 0
        while j < 4 {
            if si == src.count {
                if j == 0 { return si }
                throw Base64Error(offset: si - j)
            }
            let c = src[si]
            si += 1
            if let v = base64Value(c) {
                dbuf[j] = v
                j += 1
                continue
            }
            if isNewline(c) { continue }
            if c != pad || j < 2 { throw Base64Error(offset: si - 1) }
            if j == 2 {
                while si < src.count && isNewline(src[si]) { si += 1 }
                if si == src.count { throw Base64Error(offset: src.count) }
                if src[si] != pad { throw Base64Error(offset: si - 1) }
                si += 1
            }
            while si < src.count && isNewline(src[si]) { si += 1 }
            if si < src.count { throw Base64Error(offset: si) }
            dlen = j
            break
        }
        let val = UInt32(dbuf[0]) << 18 | UInt32(dbuf[1]) << 12 | UInt32(dbuf[2]) << 6 | UInt32(dbuf[3])
        let bytes = [UInt8(truncatingIfNeeded: val >> 16), UInt8(truncatingIfNeeded: val >> 8),
                     UInt8(truncatingIfNeeded: val)]
        out.append(contentsOf: bytes[0..<(dlen - 1)])
        return si
    }

    private struct OtpParams {
        var secret: [UInt8] = []
        var name: [UInt8] = []
        var issuer: [UInt8] = []
        var algorithm: Int64 = 0
        var digits: Int64 = 0
        var otpType: Int64 = 0
        var counter: Int64 = 0

        func toAccount() throws -> Account {
            if secret.isEmpty { throw ParamError.invalid("empty secret") }
            var issuerText = "", accountText = ""
            let nameText = GoText.string(name)
            if let colon = nameText.unicodeScalars.firstIndex(of: ":") {
                issuerText = TextClean.text(String(nameText.unicodeScalars[..<colon]))
                accountText = TextClean.text(String(nameText.unicodeScalars[nameText.unicodeScalars.index(after: colon)...]))
            } else {
                accountText = TextClean.text(nameText)
            }
            let iss = TextClean.text(GoText.string(issuer))
            if !iss.isEmpty { issuerText = iss }

            let algorithmName: String
            switch algorithm {
            case 0, 1: algorithmName = "SHA1"
            case 2: algorithmName = "SHA256"
            case 3: algorithmName = "SHA512"
            case 4: throw ParamError.invalid("algorithm MD5 is not supported by RFC TOTP clients")
            default: throw ParamError.invalid("unknown algorithm enum \(algorithm)")
            }
            let digitCount: Int
            switch digits {
            case 0, 1: digitCount = 6
            case 2: digitCount = 8
            default: throw ParamError.invalid("unknown digit-count enum \(digits)")
            }
            var type: OTPType
            switch otpType {
            case 0, 2: type = .totp
            case 1: type = .hotp
            default: throw ParamError.invalid("unknown otp-type enum \(otpType)")
            }
            if type == .totp && GoText.equalFold(issuerText, ascii: "Steam") { type = .steam }
            return Account(id: "", type: type, issuer: issuerText, account: accountText,
                           secret: Data(secret), algorithm: algorithmName, digits: digitCount,
                           period: 30, counter: counter)
        }
    }

    private static func decodePayload(_ input: ArraySlice<UInt8>) throws -> [OtpParams] {
        var b = input
        var out: [OtpParams] = []
        while !b.isEmpty {
            let (num, typ) = try wire(Wire.consumeTag(&b), "migration: malformed payload tag")
            switch num {
            case 1:
                guard typ == Wire.bytesType else {
                    throw AccountError.invalid("migration: otp_parameters has wire type \(typ), want bytes")
                }
                let sub = try wire(Wire.consumeBytes(&b), "migration: malformed otp_parameters")
                out.append(try decodeOtpParameters(sub))
            case 2, 3, 4, 5:
                guard typ == Wire.varintType else {
                    throw AccountError.invalid("migration: payload field \(num) has wire type \(typ), want varint")
                }
                _ = try wire(Wire.consumeVarint(&b), "migration: malformed payload field \(num)")
            default:
                try wire(Wire.consumeFieldValue(num, typ, &b), "migration: malformed unknown field \(num)")
            }
        }
        return out
    }

    private static func decodeOtpParameters(_ input: ArraySlice<UInt8>) throws -> OtpParams {
        var b = input
        var p = OtpParams()
        func bytesField(_ name: String, _ typ: Int) throws -> [UInt8] {
            guard typ == Wire.bytesType else {
                throw AccountError.invalid("migration: \(name) has wire type \(typ), want bytes")
            }
            return Array(try wire(Wire.consumeBytes(&b), "migration: malformed \(name)"))
        }
        func varintField(_ name: String, _ typ: Int) throws -> UInt64 {
            guard typ == Wire.varintType else {
                throw AccountError.invalid("migration: \(name) has wire type \(typ), want varint")
            }
            return try wire(Wire.consumeVarint(&b), "migration: malformed \(name)")
        }
        while !b.isEmpty {
            let (num, typ) = try wire(Wire.consumeTag(&b), "migration: malformed otp parameter tag")
            switch num {
            case 1: p.secret = try bytesField("secret", typ)
            case 2: p.name = try bytesField("name", typ)
            case 3: p.issuer = try bytesField("issuer", typ)
            case 4: p.algorithm = Int64(bitPattern: try varintField("algorithm", typ))
            case 5: p.digits = Int64(bitPattern: try varintField("digits", typ))
            case 6: p.otpType = Int64(bitPattern: try varintField("type", typ))
            case 7:
                let v = try varintField("counter", typ)
                guard v <= UInt64(OTPAuth.maxCounter) else {
                    throw AccountError.invalid("migration: counter \(v) exceeds \(OTPAuth.maxCounter)")
                }
                p.counter = Int64(v)
            default:
                try wire(Wire.consumeFieldValue(num, typ, &b), "migration: malformed unknown otp field \(num)")
            }
        }
        return p
    }

    private static func wire<T>(_ body: @autoclosure () throws -> T, _ context: @autoclosure () -> String) throws -> T {
        do { return try body() } catch let e as Wire.ParseError {
            throw AccountError.invalid("\(context()): \(e.message)")
        }
    }

    // MARK: - Writing

    private struct SkipReason: Error {
        let message: String
        let kind: Exporters.SkipKind
    }

    private static func encodeOtpParameters(_ a: Account) -> Result<[UInt8], SkipReason> {
        let typ: UInt64
        switch a.type {
        case .totp: typ = 2
        case .hotp: typ = 1
        case .steam, .unrecognized:
            return .failure(SkipReason(message: "Google Authenticator has no Steam entry type", kind: .unsupportedType))
        }
        let alg: UInt64
        switch a.algorithm {
        case "SHA1": alg = 1
        case "SHA256": alg = 2
        case "SHA512": alg = 3
        default:
            return .failure(SkipReason(message: "Google Authenticator does not store the \(a.algorithm) algorithm",
                                       kind: .unsupportedAlgorithm))
        }
        let digits: UInt64
        switch a.digits {
        case 6: digits = 1
        case 8: digits = 2
        default:
            return .failure(SkipReason(
                message: "Google Authenticator stores only 6- and 8-digit codes, this one has \(a.digits)",
                kind: .unsupportedDigits))
        }
        if a.type == .totp && a.period != 30 {
            return .failure(SkipReason(
                message: "Google Authenticator stores only 30-second codes, this one uses \(a.period)s",
                kind: .unsupportedPeriod))
        }
        if a.secret.isEmpty {
            return .failure(SkipReason(message: "the account has no secret", kind: .emptySecret))
        }
        var b: [UInt8] = []
        Wire.appendTag(&b, 1, Wire.bytesType)
        Wire.appendBytes(&b, [UInt8](a.secret))
        let name = a.issuer.isEmpty ? a.account : a.issuer + ":" + a.account
        if !name.isEmpty {
            Wire.appendTag(&b, 2, Wire.bytesType)
            Wire.appendBytes(&b, Array(name.utf8))
        }
        if !a.issuer.isEmpty {
            Wire.appendTag(&b, 3, Wire.bytesType)
            Wire.appendBytes(&b, Array(a.issuer.utf8))
        }
        Wire.appendTag(&b, 4, Wire.varintType)
        Wire.appendVarint(&b, alg)
        Wire.appendTag(&b, 5, Wire.varintType)
        Wire.appendVarint(&b, digits)
        Wire.appendTag(&b, 6, Wire.varintType)
        Wire.appendVarint(&b, typ)
        if a.type == .hotp {
            Wire.appendTag(&b, 7, Wire.varintType)
            Wire.appendVarint(&b, UInt64(bitPattern: a.counter))
        }
        return .success(b)
    }

    private static func partitionBatches(_ encoded: [[UInt8]]) -> [[[UInt8]]] {
        var batches: [[[UInt8]]] = []
        var current: [[UInt8]] = []
        var size = 0
        for p in encoded {
            let n = Wire.sizeVarint(Wire.encodeTag(1, 0)) + Wire.sizeVarint(UInt64(p.count)) + p.count
            if !current.isEmpty && (current.count >= maxAccountsPerBatch || size + n > maxPayloadBytes) {
                batches.append(current)
                current = []
                size = 0
            }
            current.append(p)
            size += n
        }
        if !current.isEmpty { batches.append(current) }
        return batches
    }

    private static func assemblePayload(_ batch: [[UInt8]], batchSize: Int, batchIndex: Int,
                                        batchID: Int32) -> [UInt8] {
        var b: [UInt8] = []
        for p in batch {
            Wire.appendTag(&b, 1, Wire.bytesType)
            Wire.appendBytes(&b, p)
        }
        Wire.appendTag(&b, 2, Wire.varintType)
        Wire.appendVarint(&b, 1)
        Wire.appendTag(&b, 3, Wire.varintType)
        Wire.appendVarint(&b, UInt64(batchSize))
        Wire.appendTag(&b, 4, Wire.varintType)
        Wire.appendVarint(&b, UInt64(batchIndex))
        Wire.appendTag(&b, 5, Wire.varintType)
        Wire.appendVarint(&b, UInt64(bitPattern: Int64(batchID)))
        return b
    }
}

/// The protobuf wire-format primitives of Go `protowire`, with its limits and
/// error text. Group skipping keeps an explicit stack, so a deeply nested
/// payload cannot exhaust the call stack.
enum Wire {
    static let varintType = 0
    static let fixed64Type = 1
    static let bytesType = 2
    static let startGroupType = 3
    static let endGroupType = 4
    static let fixed32Type = 5
    static let recursionLimit = 10000

    struct ParseError: Error, Equatable {
        let message: String
        static let truncated = ParseError(message: "unexpected EOF")
        static let fieldNumber = ParseError(message: "proto: invalid field number")
        static let overflow = ParseError(message: "proto: variable length integer overflow")
        static let reserved = ParseError(message: "proto: cannot parse reserved wire type")
        static let endGroup = ParseError(message: "proto: mismatching end group marker")
        static let parse = ParseError(message: "proto: parse error")
    }

    static func consumeVarint(_ b: inout ArraySlice<UInt8>) throws -> UInt64 {
        var v: UInt64 = 0
        var i = b.startIndex
        for k in 0..<10 {
            guard i < b.endIndex else { throw ParseError.truncated }
            let y = b[i]
            i += 1
            if k == 9 {
                guard y < 2 else { throw ParseError.overflow }
                v |= UInt64(y) << 63
                break
            }
            v |= UInt64(y & 0x7F) << (7 * UInt64(k))
            if y < 0x80 { break }
        }
        b = b[i...]
        return v
    }

    static func consumeTag(_ b: inout ArraySlice<UInt8>) throws -> (Int64, Int) {
        var rest = b
        let x = try consumeVarint(&rest)
        guard x >> 3 <= UInt64(Int32.max), x >> 3 >= 1 else { throw ParseError.fieldNumber }
        b = rest
        return (Int64(x >> 3), Int(x & 7))
    }

    static func consumeBytes(_ b: inout ArraySlice<UInt8>) throws -> ArraySlice<UInt8> {
        var rest = b
        let m = try consumeVarint(&rest)
        guard m <= UInt64(rest.count) else { throw ParseError.truncated }
        let end = rest.startIndex + Int(m)
        let v = rest[rest.startIndex..<end]
        b = rest[end...]
        return v
    }

    static func consumeFixed(_ b: inout ArraySlice<UInt8>, _ width: Int) throws {
        guard b.count >= width else { throw ParseError.truncated }
        b = b.dropFirst(width)
    }

    /// Go `protowire.ConsumeFieldValue`.
    static func consumeFieldValue(_ num: Int64, _ typ: Int, _ b: inout ArraySlice<UInt8>) throws {
        var rest = b
        var groups: [Int64] = []
        var field = (num: num, typ: typ)
        while true {
            switch field.typ {
            case varintType: _ = try consumeVarint(&rest)
            case fixed32Type: try consumeFixed(&rest, 4)
            case fixed64Type: try consumeFixed(&rest, 8)
            case bytesType: _ = try consumeBytes(&rest)
            case startGroupType:
                if recursionLimit - groups.count < 0 { throw ParseError.parse }
                groups.append(field.num)
            case endGroupType: throw ParseError.endGroup
            default: throw ParseError.reserved
            }
            while let open = groups.last {
                let (n, t) = try consumeTag(&rest)
                if t != endGroupType {
                    field = (n, t)
                    break
                }
                guard n == open else { throw ParseError.endGroup }
                groups.removeLast()
            }
            if groups.isEmpty { break }
        }
        b = rest
    }

    static func encodeTag(_ num: Int64, _ typ: Int) -> UInt64 {
        UInt64(num) << 3 | UInt64(typ & 7)
    }

    static func appendVarint(_ b: inout [UInt8], _ value: UInt64) {
        var v = value
        while v >= 0x80 {
            b.append(UInt8(truncatingIfNeeded: v) | 0x80)
            v >>= 7
        }
        b.append(UInt8(v))
    }

    static func appendTag(_ b: inout [UInt8], _ num: Int64, _ typ: Int) {
        appendVarint(&b, encodeTag(num, typ))
    }

    static func appendBytes(_ b: inout [UInt8], _ v: [UInt8]) {
        appendVarint(&b, UInt64(v.count))
        b.append(contentsOf: v)
    }

    static func sizeVarint(_ v: UInt64) -> Int {
        let bits = 64 - (v | 1).leadingZeroBitCount
        return (bits + 6) / 7
    }
}
