import Foundation

/// The OTP scheme of an account.
public enum OTPType: String, Sendable {
    case totp
    case hotp
    case steam
}

/// One stored credential. `secret` is the RAW key bytes.
public struct Account: Sendable, Equatable {
    public var id: String
    public var type: OTPType
    public var issuer: String
    public var account: String
    public var secret: Data
    public var algorithm: String   // SHA1 | SHA256 | SHA512
    public var digits: Int
    public var period: Int
    public var counter: Int64
    public var folder: String
    public var handle: String   // OPTIONAL short unique identifier; see /spec/vault-format.md
    public var tags: [String]
    public var pinned: Bool
    public var createdAt: Int64
    public var updatedAt: Int64

    public init(id: String, type: OTPType, issuer: String, account: String, secret: Data,
                algorithm: String = "SHA1", digits: Int = 6, period: Int = 30, counter: Int64 = 0,
                folder: String = "", handle: String = "", tags: [String] = [], pinned: Bool = false,
                createdAt: Int64 = 0, updatedAt: Int64 = 0) {
        self.id = id; self.type = type; self.issuer = issuer; self.account = account
        self.secret = secret; self.algorithm = algorithm; self.digits = digits; self.period = period
        self.counter = counter; self.folder = folder; self.handle = handle; self.tags = tags; self.pinned = pinned
        self.createdAt = createdAt; self.updatedAt = updatedAt
    }
}

public enum AccountError: Error, CustomStringConvertible {
    case invalid(String)
    public var description: String {
        switch self { case .invalid(let m): return "account: \(m)" }
    }
}

public extension Account {
    func validate() throws {
        if id.isEmpty { throw AccountError.invalid("empty id") }
        if secret.isEmpty { throw AccountError.invalid("account \(id): empty secret") }
        switch algorithm { case "SHA1", "SHA256", "SHA512": break
        default: throw AccountError.invalid("account \(id): invalid algorithm \(algorithm)") }
        if type != .steam && (digits < 6 || digits > 8) {
            throw AccountError.invalid("account \(id): digits must be 6-8")
        }
        if type != .hotp && period <= 0 {
            throw AccountError.invalid("account \(id): period must be positive")
        }
        if !handle.isEmpty && !Handles.isValid(handle) {
            throw AccountError.invalid("account \(id): invalid handle \(handle)")
        }
    }
}

/// Canonical JSON: the interop contract. Must be byte-identical to the Go
/// `account.CanonicalJSON` reference (see /spec/vault-format.md).
public enum CanonicalJSON {

    /// Serialize accounts to the canonical payload bytes (sorted by id; fixed,
    /// byte-order-sorted keys; Go-compatible escaping; no whitespace).
    public static func encode(_ accounts: [Account]) -> Data {
        let sorted = accounts.sorted { $0.id < $1.id }
        var out = "["
        for (i, a) in sorted.enumerated() {
            if i > 0 { out += "," }
            out += encodeAccount(a)
        }
        out += "]"
        return Data(out.utf8)
    }

    /// Parse canonical payload bytes into accounts, rejecting duplicate keys.
    public static func decode(_ data: Data) throws -> [Account] {
        try rejectDuplicateKeys(data)
        guard let arr = try JSONSerialization.jsonObject(with: data) as? [[String: Any]] else {
            throw AccountError.invalid("canonical json is not an array of objects")
        }
        return try arr.map { try account(from: $0) }
    }

    private static func encodeAccount(_ a: Account) -> String {
        // Keys MUST be emitted in byte-order-sorted order.
        var s = "{"
        s += "\"account\":\(str(a.account)),"
        s += "\"algorithm\":\(str(a.algorithm)),"
        s += "\"counter\":\(a.counter),"
        s += "\"created_at\":\(a.createdAt),"
        s += "\"digits\":\(a.digits),"
        s += "\"folder\":\(str(a.folder)),"
        // Optional; omitted when empty. Sorts between "folder" and "id" by byte order.
        if !a.handle.isEmpty { s += "\"handle\":\(str(a.handle))," }
        s += "\"id\":\(str(a.id)),"
        s += "\"issuer\":\(str(a.issuer)),"
        s += "\"period\":\(a.period),"
        s += "\"pinned\":\(a.pinned ? "true" : "false"),"
        s += "\"secret\":\(str(a.secret.base64EncodedString())),"
        s += "\"tags\":[" + a.tags.enumerated().map { ($0.0 > 0 ? "," : "") + str($0.1) }.joined() + "],"
        s += "\"type\":\(str(a.type.rawValue)),"
        s += "\"updated_at\":\(a.updatedAt)"
        s += "}"
        return s
    }

    /// Escape a string exactly as Go's encoding/json with SetEscapeHTML(false):
    /// short escapes for " \ \n \r \t; \u00xx for other control chars;
    /// and   escaped; everything else (incl. < > & /) emitted raw UTF-8.
    static func str(_ value: String) -> String {
        var out = "\""
        for scalar in value.unicodeScalars {
            switch scalar {
            case "\"": out += "\\\""
            case "\\": out += "\\\\"
            case "\n": out += "\\n"
            case "\r": out += "\\r"
            case "\t": out += "\\t"
            case "\u{2028}": out += "\\u2028"
            case "\u{2029}": out += "\\u2029"
            default:
                if scalar.value < 0x20 {
                    out += String(format: "\\u%04x", scalar.value)
                } else {
                    out.unicodeScalars.append(scalar)
                }
            }
        }
        out += "\""
        return out
    }

    private static func account(from o: [String: Any]) throws -> Account {
        func sval(_ k: String) -> String { o[k] as? String ?? "" }
        func ival(_ k: String) -> Int { (o[k] as? NSNumber)?.intValue ?? 0 }
        func i64(_ k: String) -> Int64 { (o[k] as? NSNumber)?.int64Value ?? 0 }
        guard let secret = Data(base64Encoded: sval("secret")) else {
            throw AccountError.invalid("invalid base64 secret")
        }
        guard let type = OTPType(rawValue: sval("type")) else {
            throw AccountError.invalid("invalid type \(sval("type"))")
        }
        let tags = (o["tags"] as? [String]) ?? []
        return Account(id: sval("id"), type: type, issuer: sval("issuer"), account: sval("account"),
                       secret: secret, algorithm: sval("algorithm"), digits: ival("digits"),
                       period: ival("period"), counter: i64("counter"), folder: sval("folder"),
                       handle: sval("handle"), tags: tags, pinned: (o["pinned"] as? Bool) ?? false,
                       createdAt: i64("created_at"), updatedAt: i64("updated_at"))
    }

    /// Reject objects with duplicate keys (JSONSerialization silently keeps the
    /// last); we scan the raw tokens to enforce canonical-JSON rule 6.
    private static func rejectDuplicateKeys(_ data: Data) throws {
        // JSONSerialization does not expose duplicates; do a lightweight check by
        // counting keys in each object via a manual scan of the UTF-8 stream.
        // Accounts have a fixed key set, so duplicate keys would exceed 14 per object.
        let s = String(decoding: data, as: UTF8.self)
        var depth = 0
        var inString = false
        var escaped = false
        var keysAtDepth: [Int: Set<String>] = [:]
        var i = s.startIndex
        var pendingKey = ""
        var collectingKey = false
        var expectKey = false
        while i < s.endIndex {
            let c = s[i]
            if inString {
                if escaped { escaped = false }
                else if c == "\\" { escaped = true }
                else if c == "\"" {
                    inString = false
                    if collectingKey {
                        collectingKey = false
                        // Peek next non-space for ':' to confirm it's a key.
                        var j = s.index(after: i)
                        while j < s.endIndex, s[j] == " " { j = s.index(after: j) }
                        if j < s.endIndex, s[j] == ":" {
                            if keysAtDepth[depth, default: []].contains(pendingKey) {
                                throw AccountError.invalid("duplicate key \(pendingKey)")
                            }
                            keysAtDepth[depth, default: []].insert(pendingKey)
                        }
                    }
                } else if collectingKey {
                    pendingKey.append(c)
                }
            } else {
                switch c {
                case "{": depth += 1; keysAtDepth[depth] = []; expectKey = true
                case "}": keysAtDepth[depth] = nil; depth -= 1
                case "\"":
                    inString = true
                    if expectKey { collectingKey = true; pendingKey = ""; expectKey = false }
                case ",": expectKey = true
                case ":": expectKey = false
                default: break
                }
            }
            i = s.index(after: i)
        }
    }
}
