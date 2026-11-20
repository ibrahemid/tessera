import Foundation

extension Exporters {

    // MARK: - 2FAS

    static func renderTwoFAS(_ accounts: [Account]) -> File {
        let groups: [ExportJSON] = folders(accounts).map { f in
            .object([
                ("id", .string(f.id)),
                ("name", .string(f.name)),
                ("isExpanded", .bool(true)),
                ("updatedAt", .int(f.lastUpdatedAt &* 1000)),
            ])
        }
        let services: [ExportJSON] = accounts.enumerated().map { i, a in
            let isHOTP = a.type == .hotp
            return .object([
                ("name", .string(displayName(a))),
                ("secret", .string(secretText(a))),
                ("updatedAt", .int(a.updatedAt &* 1000)),
                ("otp", .object([
                    ("link", .string(OTPAuth.format(a))),
                    ("label", .string(otpauthLabel(a))),
                    ("account", .string(a.account)),
                    ("issuer", .string(a.issuer)),
                    ("digits", .int(a.digits)),
                    ("period", isHOTP ? .null : .int(a.period)),
                    ("algorithm", .string(a.algorithm)),
                    ("tokenType", .string(tokenType(a.type))),
                    ("counter", isHOTP ? .int(a.counter) : .null),
                    ("source", .string("Link")),
                ])),
                ("order", .object([("position", .int(i))])),
                ("groupId", optionalFolderID(a)),
            ])
        }
        let doc: ExportJSON = .object([
            ("schemaVersion", .int(4)),
            ("services", .array(services)),
            ("groups", .array(groups)),
        ])
        return File(name: "2fas-export.json", data: doc.document)
    }

    // MARK: - Aegis

    static func renderAegis(_ accounts: [Account]) -> File {
        let groups: [ExportJSON] = folders(accounts).map { f in
            .object([("uuid", .string(f.id)), ("name", .string(f.name))])
        }
        let entries: [ExportJSON] = accounts.map { a in
            var info: [(String, ExportJSON)] = [
                ("secret", .string(secretText(a))),
                ("algo", .string(a.algorithm)),
                ("digits", .int(a.digits)),
            ]
            if a.type == .hotp {
                info.append(("counter", .int(a.counter)))
            } else {
                info.append(("period", .int(a.period)))
            }
            return .object([
                ("type", .string(a.type.rawValue)),
                ("uuid", .string(exportID(a.id))),
                ("name", .string(a.account)),
                ("issuer", .string(a.issuer)),
                ("note", .string(a.note)),
                ("favorite", .bool(a.pinned)),
                ("icon", .null),
                ("info", .object(info)),
                ("groups", .array(a.folder.isEmpty ? [] : [.string(folderID(a.folder))])),
            ])
        }
        let doc: ExportJSON = .object([
            ("version", .int(1)),
            ("header", .object([("slots", .null), ("params", .null)])),
            ("db", .object([
                ("version", .int(3)),
                ("entries", .array(entries)),
                ("groups", .array(groups)),
            ])),
        ])
        return File(name: "aegis-export.json", data: doc.document)
    }

    // MARK: - andOTP

    static func renderAndOTP(_ accounts: [Account]) -> File {
        let entries: [ExportJSON] = accounts.map { a in
            var members: [(String, ExportJSON)] = [
                ("secret", .string(secretText(a))),
                ("issuer", .string(a.issuer)),
                ("label", .string(a.account)),
                ("digits", .int(a.digits)),
                ("type", .string(tokenType(a.type))),
                ("algorithm", .string(a.algorithm)),
                ("thumbnail", .string("Default")),
                ("last_used", .int(0)),
                ("used_frequency", .int(0)),
            ]
            if a.type == .hotp {
                members.append(("counter", .int(a.counter)))
            } else {
                members.append(("period", .int(a.period)))
            }
            members.append(("tags", .array(a.tags.map { .string($0) })))
            return .object(members)
        }
        return File(name: "andotp-export.json", data: ExportJSON.array(entries).document)
    }

    // MARK: - Apple Passwords CSV

    static let appleCSVHeader = ["Title", "URL", "Username", "Password", "Notes", "OTPAuth"]
    static let appleFormulaReason = "title or username starts with =, +, - or @, which a spreadsheet runs as a formula"
    static let appleNoteFormulaReason = "it starts with =, +, - or @, which a spreadsheet runs as a formula"

    /// The CSV file, or nil when every account was left out.
    static func renderAppleCSV(_ accounts: [Account]) -> (File?, [Skipped]) {
        var out: [UInt8] = []
        appendCSVRecord(appleCSVHeader, to: &out)
        var skipped: [Skipped] = []
        var written = 0
        for a in accounts {
            if startsAsFormula(a.issuer) || startsAsFormula(a.account) {
                skipped.append(Skipped(label: label(a), reason: appleFormulaReason, kind: .formulaName))
                continue
            }
            var note = a.note
            if startsAsFormula(note) {
                skipped.append(Skipped(label: "the note of " + label(a), reason: appleNoteFormulaReason,
                                       kind: .formulaNote))
                note = ""
            }
            appendCSVRecord([a.issuer, "", a.account, "", note, OTPAuth.format(a)], to: &out)
            written += 1
        }
        if written == 0 { return (nil, skipped) }
        return (File(name: "apple-passwords-export.csv", data: Data(out)), skipped)
    }

    static func startsAsFormula(_ s: String) -> Bool {
        switch s.utf8.first {
        case UInt8(ascii: "="), UInt8(ascii: "+"), UInt8(ascii: "-"), UInt8(ascii: "@"): return true
        default: return false
        }
    }

    /// One record as Go's `csv.Writer` writes it with a comma separator and
    /// `UseCRLF` off.
    static func appendCSVRecord(_ fields: [String], to out: inout [UInt8]) {
        for (n, field) in fields.enumerated() {
            if n > 0 { out.append(0x2C) }
            let bytes = Array(field.utf8)
            guard csvFieldNeedsQuotes(field, bytes) else {
                out.append(contentsOf: bytes)
                continue
            }
            out.append(0x22)
            for c in bytes {
                if c == 0x22 { out.append(0x22) }
                out.append(c)
            }
            out.append(0x22)
        }
        out.append(0x0A)
    }

    static func csvFieldNeedsQuotes(_ field: String, _ bytes: [UInt8]) -> Bool {
        if bytes.isEmpty { return false }
        if bytes == [0x5C, 0x2E] { return true }
        if bytes.contains(where: { $0 == 0x0A || $0 == 0x0D || $0 == 0x22 || $0 == 0x2C }) { return true }
        guard let first = field.unicodeScalars.first else { return false }
        return GoText.isSpace(first.value)
    }

    // MARK: - Bitwarden Authenticator

    static func renderBitwarden(_ accounts: [Account]) -> File {
        let folderList: [ExportJSON] = folders(accounts).map { f in
            .object([("id", .string(f.id)), ("name", .string(f.name))])
        }
        let items: [ExportJSON] = accounts.map { a in
            .object([
                ("id", .string(exportID(a.id))),
                ("name", .string(displayName(a))),
                ("folderId", optionalFolderID(a)),
                ("organizationId", .null),
                ("collectionIds", .null),
                ("notes", optionalString(a.note)),
                ("type", .int(1)),
                ("login", .object([("totp", .string(OTPAuth.format(a)))])),
                ("favorite", .bool(a.pinned)),
            ])
        }
        var members: [(String, ExportJSON)] = [("encrypted", .bool(false))]
        if !folderList.isEmpty { members.append(("folders", .array(folderList))) }
        members.append(("items", .array(items)))
        return File(name: "bitwarden-authenticator-export.json", data: ExportJSON.object(members).document)
    }

    // MARK: - Proton Authenticator

    static let protonHOTPReason = "Proton Authenticator stores only TOTP and Steam entries"

    static func renderProton(_ accounts: [Account]) -> (File, [Skipped]) {
        var entries: [ExportJSON] = []
        var skipped: [Skipped] = []
        for a in accounts {
            let content: ExportJSON
            switch a.type {
            case .hotp:
                skipped.append(Skipped(label: label(a), reason: protonHOTPReason, kind: .unsupportedType))
                continue
            case .steam:
                content = .object([
                    ("uri", .string("steam://" + secretText(a))),
                    ("entry_type", .string("Steam")),
                    ("name", .string(a.account)),
                ])
            case .totp:
                content = .object([
                    ("uri", .string(OTPAuth.format(a))),
                    ("entry_type", .string("Totp")),
                    ("name", .string(displayName(a))),
                ])
            }
            entries.append(.object([
                ("id", .string(exportID(a.id))),
                ("content", content),
                ("note", optionalString(a.note)),
            ]))
        }
        let doc: ExportJSON = .object([("version", .int(1)), ("entries", .array(entries))])
        return (File(name: "proton-authenticator-export.json", data: doc.document), skipped)
    }

    // MARK: - Google Authenticator transfer

    /// Why the transfer format leaves `a` out, checked in the Go order, or nil
    /// when it can carry the account.
    static func transferSkipKind(_ a: Account) -> SkipKind? {
        if a.type == .steam { return .unsupportedType }
        switch Array(a.algorithm.utf8) {
        case Array("SHA1".utf8), Array("SHA256".utf8), Array("SHA512".utf8): break
        default: return .unsupportedAlgorithm
        }
        if a.digits != 6 && a.digits != 8 { return .unsupportedDigits }
        if a.type == .totp && a.period != 30 { return .unsupportedPeriod }
        return nil
    }

    static func renderTransferCodes(_ accounts: [Account], batchID: Int32) throws -> ([TransferCode], [Skipped]) {
        let export = Migration.export(accounts, batchID: batchID)
        let left = accounts.compactMap { a in transferSkipKind(a).map { (label(a), $0) } }
        guard left.count == export.skipped.count else { throw ExportError.transferSkipsDisagree }
        var skipped: [Skipped] = []
        skipped.reserveCapacity(left.count)
        for (rule, encoder) in zip(left, export.skipped) {
            guard Array(rule.0.utf8) == Array(encoder.label.utf8) else { throw ExportError.transferSkipsDisagree }
            skipped.append(Skipped(label: encoder.label, reason: encoder.reason, kind: rule.1))
        }
        let total = export.uris.count
        let codes = export.uris.enumerated().map { i, uri in
            TransferCode(name: "google-migration-\(i + 1)-of-\(total).png", uri: uri)
        }
        return (codes, skipped)
    }
}
