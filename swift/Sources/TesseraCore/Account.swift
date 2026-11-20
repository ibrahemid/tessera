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
    /// Free text kept with the account; empty means absent.
    public var note: String
    /// Unix seconds when the account was moved to the trash; zero means live.
    public var deletedAt: Int64
    /// Manual display position, 1 first; zero means unordered.
    public var order: Int
    private var extensionStorage: [ExtensionField]

    /// Payload keys outside the defined set, kept so a newer writer's data
    /// survives a round trip. Sorted by key bytes; a repeated key keeps the last value.
    public var extensions: [ExtensionField] {
        get { extensionStorage }
        set { extensionStorage = Self.normalized(newValue) }
    }

    public init(id: String, type: OTPType, issuer: String, account: String, secret: Data,
                algorithm: String = "SHA1", digits: Int = 6, period: Int = 30, counter: Int64 = 0,
                folder: String = "", handle: String = "", tags: [String] = [], pinned: Bool = false,
                createdAt: Int64 = 0, updatedAt: Int64 = 0,
                note: String = "", deletedAt: Int64 = 0, order: Int = 0,
                extensions: [ExtensionField] = []) {
        self.id = id; self.type = type; self.issuer = issuer; self.account = account
        self.secret = secret; self.algorithm = algorithm; self.digits = digits; self.period = period
        self.counter = counter; self.folder = folder; self.handle = handle; self.tags = tags; self.pinned = pinned
        self.createdAt = createdAt; self.updatedAt = updatedAt
        self.note = note; self.deletedAt = deletedAt; self.order = order
        self.extensionStorage = Self.normalized(extensions)
    }

    /// The largest HOTP counter a vault may store; advancing from it is refused.
    public static let maxCounter: Int64 = Int64.max - 1

    /// Whether the account is in the trash.
    public var isTrashed: Bool { deletedAt > 0 }

    /// Whether the account carries anything only a version 2 payload can hold.
    public var usesFormatVersion2: Bool {
        deletedAt != 0 || !note.isEmpty || order != 0 || !extensionStorage.isEmpty
    }

    /// The envelope version a writer must declare for these accounts: 2 when
    /// any account uses a key outside the version 1 set, else 1.
    public static func formatVersion(_ accounts: [Account]) -> Int {
        accounts.contains { $0.usesFormatVersion2 } ? 2 : 1
    }

    /// The HOTP counter after `counter`, refusing values outside 0..maxCounter-1.
    public static func nextCounter(after counter: Int64) throws -> Int64 {
        guard counter >= 0, counter < maxCounter else { throw CounterRangeError(counter: counter) }
        return counter + 1
    }

    /// Accounts not in the trash, in input order.
    public static func live(_ accounts: [Account]) -> [Account] {
        accounts.filter { !$0.isTrashed }
    }

    /// Accounts in the trash, in input order.
    public static func trashed(_ accounts: [Account]) -> [Account] {
        accounts.filter { $0.isTrashed }
    }

    /// Display order: accounts with a positive `order` first, ascending, then
    /// the rest by issuer and account name (natural, case-insensitive), with
    /// the id breaking ties. Stable.
    public static func sortedForDisplay(_ accounts: [Account]) -> [Account] {
        accounts.enumerated().sorted { lhs, rhs in
            let c = DisplayOrder.compare(lhs.element, rhs.element)
            return c != 0 ? c < 0 : lhs.offset < rhs.offset
        }.map(\.element)
    }

    private static func normalized(_ fields: [ExtensionField]) -> [ExtensionField] {
        var out: [ExtensionField] = []
        for f in fields {
            if let i = out.firstIndex(where: { $0.keyBytes == f.keyBytes }) {
                out[i] = f
            } else {
                out.append(f)
            }
        }
        return out.sorted { StrictJSON.bytesLess($0.keyBytes, $1.keyBytes) }
    }
}

/// A payload key outside the defined set and its value.
public struct ExtensionField: Sendable, Equatable {
    public let key: String
    public let value: ExtensionValue
    let keyBytes: [UInt8]

    /// Fails with `PayloadError.extensionKey` when `key` is one the spec defines.
    public init(key: String, value: ExtensionValue) throws {
        if CanonicalJSON.definedKeys.contains(Array(key.utf8)) { throw PayloadError.extensionKey(key) }
        self.key = key
        self.value = value
        self.keyBytes = Array(key.utf8)
    }

    init(keyBytes: [UInt8], value: ExtensionValue) {
        self.key = String(decoding: keyBytes, as: UTF8.self)
        self.value = value
        self.keyBytes = keyBytes
    }

    public static func == (lhs: ExtensionField, rhs: ExtensionField) -> Bool {
        lhs.keyBytes == rhs.keyBytes && lhs.value == rhs.value
    }
}

/// A JSON value held in canonical form: object keys sorted by bytes, Go
/// string escaping, canonical int64 numbers only, no whitespace.
public struct ExtensionValue: Sendable, Hashable {
    public let canonicalJSON: Data

    /// Parses `json` under the payload rules at extension depth and keeps its
    /// canonical form.
    public init(json: Data) throws {
        let bytes = [UInt8](json)
        let node: JSONNode
        do {
            node = try StrictJSON.parse(bytes, rule: .canonicalInt64, level: CanonicalJSON.extensionLevel)
        } catch let f as JSONFailure {
            throw PayloadError(f)
        }
        self.init(node: node, source: bytes)
    }

    init(node: JSONNode, source: [UInt8]) {
        var out: [UInt8] = []
        StrictJSON.appendCanonical(node, source: source, to: &out)
        canonicalJSON = Data(out)
    }
}

/// A payload that breaks the strict canonical JSON rules of /spec/vault-format.md.
public enum PayloadError: Error, Equatable, Sendable, CustomStringConvertible, LocalizedError {
    case malformed
    case duplicateKey
    case nonCanonicalNumber
    case fieldType(field: String)
    case nonCanonicalBase64(field: String)
    case extensionKey(String)
    case counterRange(Int64)

    init(_ failure: JSONFailure) {
        switch failure {
        case .syntax, .tooDeep: self = .malformed
        case .duplicateKey: self = .duplicateKey
        case .nonCanonicalNumber: self = .nonCanonicalNumber
        }
    }

    public var description: String {
        switch self {
        case .malformed: return "account: payload is not an array of account objects"
        case .duplicateKey: return "account: duplicate key"
        case .nonCanonicalNumber: return "account: number is not a canonical int64 integer"
        case .fieldType(let f): return "account: field \(GoQuote.quote(f)): account: field has the wrong JSON type"
        case .nonCanonicalBase64(let f):
            return "account: field \(GoQuote.quote(f)): account: secret is not canonical standard base64"
        case .extensionKey(let k): return "account: extension field uses a key the spec defines: \(GoQuote.quote(k))"
        case .counterRange(let c):
            return "account: field \"counter\": counter outside 0..\(Account.maxCounter), got \(c)"
        }
    }

    public var errorDescription: String? { description }
}

/// An HOTP counter that cannot be advanced.
public struct CounterRangeError: Error, Equatable, Sendable, CustomStringConvertible, LocalizedError {
    public let counter: Int64
    public var description: String { "counter outside 0..\(Account.maxCounter): cannot advance \(counter)" }
    public var errorDescription: String? { description }
}

public enum AccountError: Error, CustomStringConvertible {
    case invalid(String)
    public var description: String {
        switch self { case .invalid(let m): return "account: \(m)" }
    }
}

public extension Account {
    func validate() throws {
        let q = GoQuote.quote(id)
        if id.isEmpty { throw AccountError.invalid("empty id") }
        if secret.isEmpty { throw AccountError.invalid("account \(q): empty secret") }
        switch algorithm { case "SHA1", "SHA256", "SHA512": break
        default: throw AccountError.invalid("account \(q): invalid algorithm \(GoQuote.quote(algorithm))") }
        if type != .steam && (digits < 6 || digits > 8) {
            throw AccountError.invalid("account \(q): digits must be 6-8, got \(digits)")
        }
        if type != .hotp && period <= 0 {
            throw AccountError.invalid("account \(q): period must be positive, got \(period)")
        }
        if counter < 0 || counter > Account.maxCounter {
            throw AccountError.invalid("account \(q): counter outside 0..\(Account.maxCounter), got \(counter)")
        }
        if !handle.isEmpty && !Handles.isValid(handle) {
            throw AccountError.invalid("account \(q): invalid handle \(GoQuote.quote(handle))")
        }
        if deletedAt < 0 {
            throw AccountError.invalid("account \(q): deleted_at must not be negative, got \(deletedAt)")
        }
        if order < 0 {
            throw AccountError.invalid("account \(q): order must not be negative, got \(order)")
        }
    }
}

/// Canonical JSON: the interop contract. Must be byte-identical to the Go
/// `account.CanonicalJSON` reference (see /spec/vault-format.md).
public enum CanonicalJSON {

    static let extensionLevel = 3

    static let definedKeys: Set<[UInt8]> = Set([
        "account", "algorithm", "counter", "created_at", "deleted_at", "digits", "folder", "handle",
        "id", "issuer", "note", "order", "period", "pinned", "secret", "tags", "type", "updated_at",
    ].map { Array($0.utf8) })

    /// Serialize accounts to the canonical payload bytes: sorted by id bytes,
    /// keys sorted by bytes, Go-compatible escaping, no whitespace.
    public static func encode(_ accounts: [Account]) -> Data {
        let sorted = accounts.enumerated().sorted { lhs, rhs in
            let c = DisplayOrder.bytesCompare(lhs.element.id, rhs.element.id)
            return c != 0 ? c < 0 : lhs.offset < rhs.offset
        }
        var out: [UInt8] = [0x5B]
        for (i, entry) in sorted.enumerated() {
            if i > 0 { out.append(0x2C) }
            appendAccount(entry.element, to: &out)
        }
        out.append(0x5D)
        return Data(out)
    }

    private static func appendAccount(_ a: Account, to out: inout [UInt8]) {
        func str(_ s: String) -> [UInt8] { StrictJSON.quoted(s) }
        func num<T: BinaryInteger>(_ n: T) -> [UInt8] { Array(String(n).utf8) }
        var members: [([UInt8], [UInt8])] = [
            (Array("account".utf8), str(a.account)),
            (Array("algorithm".utf8), str(a.algorithm)),
            (Array("counter".utf8), num(a.counter)),
            (Array("created_at".utf8), num(a.createdAt)),
            (Array("digits".utf8), num(a.digits)),
            (Array("folder".utf8), str(a.folder)),
            (Array("id".utf8), str(a.id)),
            (Array("issuer".utf8), str(a.issuer)),
            (Array("period".utf8), num(a.period)),
            (Array("pinned".utf8), Array((a.pinned ? "true" : "false").utf8)),
            (Array("secret".utf8), [0x22] + GoBase64.encode(a.secret) + [0x22]),
            (Array("tags".utf8), tagsJSON(a.tags)),
            (Array("type".utf8), str(a.type.rawValue)),
            (Array("updated_at".utf8), num(a.updatedAt)),
        ]
        if !a.handle.isEmpty { members.append((Array("handle".utf8), str(a.handle))) }
        if a.deletedAt != 0 { members.append((Array("deleted_at".utf8), num(a.deletedAt))) }
        if !a.note.isEmpty { members.append((Array("note".utf8), str(a.note))) }
        if a.order != 0 { members.append((Array("order".utf8), num(a.order))) }
        for f in a.extensions { members.append((f.keyBytes, [UInt8](f.value.canonicalJSON))) }
        members.sort { StrictJSON.bytesLess($0.0, $1.0) }
        out.append(0x7B)
        for (i, m) in members.enumerated() {
            if i > 0 { out.append(0x2C) }
            StrictJSON.appendQuoted(m.0, to: &out)
            out.append(0x3A)
            out.append(contentsOf: m.1)
        }
        out.append(0x7D)
    }

    private static func tagsJSON(_ tags: [String]) -> [UInt8] {
        var out: [UInt8] = [0x5B]
        for (i, t) in tags.enumerated() {
            if i > 0 { out.append(0x2C) }
            StrictJSON.appendQuoted(t.utf8, to: &out)
        }
        out.append(0x5D)
        return out
    }

    /// Parse payload bytes under the strict rules: duplicate keys at any depth,
    /// non-canonical numbers, wrong field types, non-canonical base64, nesting
    /// past 64 levels and trailing data are all rejected with `PayloadError`.
    /// Missing fields take their zero value, as in the Go core.
    public static func decode(_ data: Data) throws -> [Account] {
        let bytes = [UInt8](data)
        let root: JSONNode
        do {
            root = try StrictJSON.parse(bytes, rule: .canonicalInt64)
        } catch let f as JSONFailure {
            throw PayloadError(f)
        }
        guard root.kind == .array else { throw PayloadError.malformed }
        var out: [Account] = []
        out.reserveCapacity(root.items.count)
        for item in root.items {
            guard item.kind == .object else { throw PayloadError.malformed }
            out.append(try decodeAccount(item, source: bytes))
        }
        return out
    }

    private struct Fields {
        var id = "", type = "", issuer = "", account = "", algorithm = "", folder = "", handle = "", note = ""
        var secret = Data()
        var digits = 0, period = 0, order = 0
        var counter: Int64 = 0, createdAt: Int64 = 0, updatedAt: Int64 = 0, deletedAt: Int64 = 0
        var tags: [String] = []
        var pinned = false
        var extensions: [ExtensionField] = []
    }

    private static func decodeAccount(_ obj: JSONNode, source: [UInt8]) throws -> Account {
        var f = Fields()
        for m in obj.members.sorted(by: { StrictJSON.bytesLess($0.key, $1.key) }) {
            let name = String(decoding: m.key, as: UTF8.self)
            let v = m.value
            guard definedKeys.contains(m.key) else {
                f.extensions.append(ExtensionField(keyBytes: m.key, value: ExtensionValue(node: v, source: source)))
                continue
            }
            switch name {
            case "account": f.account = try string(v, name)
            case "algorithm": f.algorithm = try string(v, name)
            case "folder": f.folder = try string(v, name)
            case "handle": f.handle = try string(v, name)
            case "id": f.id = try string(v, name)
            case "issuer": f.issuer = try string(v, name)
            case "note": f.note = try string(v, name)
            case "type": f.type = try string(v, name)
            case "counter": f.counter = try int64(v, name, source)
            case "created_at": f.createdAt = try int64(v, name, source)
            case "deleted_at": f.deletedAt = try int64(v, name, source)
            case "updated_at": f.updatedAt = try int64(v, name, source)
            case "digits": f.digits = try int(v, name, source)
            case "order": f.order = try int(v, name, source)
            case "period": f.period = try int(v, name, source)
            case "pinned":
                guard v.kind == .literal else { throw PayloadError.fieldType(field: name) }
                switch source[v.range].first {
                case 0x74: f.pinned = true
                case 0x66: f.pinned = false
                default: throw PayloadError.fieldType(field: name)
                }
            case "secret":
                guard v.kind == .string else { throw PayloadError.fieldType(field: name) }
                guard let b = GoBase64.decodeCanonical(v.string) else {
                    throw PayloadError.nonCanonicalBase64(field: name)
                }
                f.secret = Data(b)
            case "tags":
                guard v.kind == .array else { throw PayloadError.fieldType(field: name) }
                f.tags = try v.items.map { try string($0, name) }
            default:
                break
            }
        }
        if f.counter < 0 || f.counter > Account.maxCounter { throw PayloadError.counterRange(f.counter) }
        guard let type = OTPType(rawValue: f.type) else {
            throw AccountError.invalid("account \(GoQuote.quote(f.id)): invalid type \(GoQuote.quote(f.type))")
        }
        return Account(id: f.id, type: type, issuer: f.issuer, account: f.account, secret: f.secret,
                       algorithm: f.algorithm, digits: f.digits, period: f.period, counter: f.counter,
                       folder: f.folder, handle: f.handle, tags: f.tags, pinned: f.pinned,
                       createdAt: f.createdAt, updatedAt: f.updatedAt,
                       note: f.note, deletedAt: f.deletedAt, order: f.order, extensions: f.extensions)
    }

    private static func string(_ v: JSONNode, _ field: String) throws -> String {
        guard v.kind == .string else { throw PayloadError.fieldType(field: field) }
        return String(decoding: v.string, as: UTF8.self)
    }

    private static func int64(_ v: JSONNode, _ field: String, _ source: [UInt8]) throws -> Int64 {
        guard v.kind == .number, let n = Int64(String(decoding: source[v.range], as: UTF8.self)) else {
            throw PayloadError.fieldType(field: field)
        }
        return n
    }

    private static func int(_ v: JSONNode, _ field: String, _ source: [UInt8]) throws -> Int {
        let n = try int64(v, field, source)
        guard let i = Int(exactly: n) else { throw PayloadError.nonCanonicalNumber }
        return i
    }
}

/// The display comparison of the Go core's `SortForDisplay`.
enum DisplayOrder {
    static func compare(_ a: Account, _ b: Account) -> Int {
        let aOrdered = a.order > 0, bOrdered = b.order > 0
        switch (aOrdered, bOrdered) {
        case (true, false): return -1
        case (false, true): return 1
        case (true, true):
            if a.order != b.order { return a.order < b.order ? -1 : 1 }
        case (false, false):
            let ci = naturalCompare(a.issuer, b.issuer)
            if ci != 0 { return ci }
            let ca = naturalCompare(a.account, b.account)
            if ca != 0 { return ca }
        }
        return bytesCompare(a.id, b.id)
    }

    static func bytesCompare(_ a: String, _ b: String) -> Int {
        if a.utf8.elementsEqual(b.utf8) { return 0 }
        return StrictJSON.utf8Less(a, b) ? -1 : 1
    }

    private static func isDigit(_ r: Unicode.Scalar) -> Bool { r.value >= 0x30 && r.value <= 0x39 }

    static func naturalCompare(_ a: String, _ b: String) -> Int {
        let ra = Array(a.unicodeScalars), rb = Array(b.unicodeScalars)
        var i = 0, j = 0
        while i < ra.count && j < rb.count {
            if isDigit(ra[i]) && isDigit(rb[j]) {
                let si = i
                while i < ra.count && isDigit(ra[i]) { i += 1 }
                let sj = j
                while j < rb.count && isDigit(rb[j]) { j += 1 }
                let c = compareDigitRuns(ra[si..<i], rb[sj..<j])
                if c != 0 { return c }
                continue
            }
            let ca = lower(ra[i]), cb = lower(rb[j])
            if ca != cb { return ca < cb ? -1 : 1 }
            i += 1
            j += 1
        }
        if i < ra.count { return 1 }
        if j < rb.count { return -1 }
        return 0
    }

    private static func compareDigitRuns(_ a: ArraySlice<Unicode.Scalar>, _ b: ArraySlice<Unicode.Scalar>) -> Int {
        let ta = a.drop { $0 == "0" }, tb = b.drop { $0 == "0" }
        if ta.count != tb.count { return ta.count < tb.count ? -1 : 1 }
        for (x, y) in zip(ta, tb) where x != y { return x.value < y.value ? -1 : 1 }
        return 0
    }

    /// Go's `unicode.ToLower`: the simple one-to-one lowercase mapping.
    static func lower(_ r: Unicode.Scalar) -> UInt32 {
        if r.value == 0x130 { return 0x69 }
        let mapped = r.properties.lowercaseMapping.unicodeScalars
        guard mapped.count == 1, let m = mapped.first else { return r.value }
        return m.value
    }
}

/// Go's `strconv.Quote`, used so error text matches the Go core.
enum GoQuote {
    static func quote(_ s: String) -> String {
        var out = "\""
        for scalar in s.unicodeScalars {
            switch scalar {
            case "\"": out += "\\\""
            case "\\": out += "\\\\"
            case "\u{07}": out += "\\a"
            case "\u{08}": out += "\\b"
            case "\u{0C}": out += "\\f"
            case "\n": out += "\\n"
            case "\r": out += "\\r"
            case "\t": out += "\\t"
            case "\u{0B}": out += "\\v"
            default:
                if isPrint(scalar) {
                    out.unicodeScalars.append(scalar)
                } else if scalar.value < 0x80 {
                    out += "\\x" + hex(scalar.value, width: 2)
                } else if scalar.value < 0x10000 {
                    out += "\\u" + hex(scalar.value, width: 4)
                } else {
                    out += "\\U" + hex(scalar.value, width: 8)
                }
            }
        }
        return out + "\""
    }

    private static func hex(_ v: UInt32, width: Int) -> String {
        let s = String(v, radix: 16)
        return String(repeating: "0", count: max(0, width - s.count)) + s
    }

    private static func isPrint(_ r: Unicode.Scalar) -> Bool {
        if r == " " { return true }
        switch r.properties.generalCategory {
        case .uppercaseLetter, .lowercaseLetter, .titlecaseLetter, .modifierLetter, .otherLetter,
             .nonspacingMark, .spacingMark, .enclosingMark,
             .decimalNumber, .letterNumber, .otherNumber,
             .connectorPunctuation, .dashPunctuation, .openPunctuation, .closePunctuation,
             .initialPunctuation, .finalPunctuation, .otherPunctuation,
             .mathSymbol, .currencySymbol, .modifierSymbol, .otherSymbol:
            return true
        default:
            return false
        }
    }
}
