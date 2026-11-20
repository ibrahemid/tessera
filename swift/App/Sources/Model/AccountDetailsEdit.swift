import Foundation
import TesseraCore

struct ParameterDraft: Equatable {
    var type: OTPType
    var algorithm: String
    var digits: Int
    var period: Int
    var counter: Int64
    var secretText = ""

    static let algorithms = ["SHA1", "SHA256", "SHA512"]

    init(_ account: Account) {
        type = account.type
        algorithm = account.algorithm
        digits = account.digits
        period = account.period
        counter = account.counter
    }

    mutating func setType(_ new: OTPType) {
        guard new != type else { return }
        if new == .steam {
            digits = 5
            period = 30
            algorithm = "SHA1"
        } else if type == .steam {
            digits = 6
        }
        if new != .hotp, period <= 0 { period = 30 }
        type = new
    }

    mutating func adopt(_ link: Account) {
        type = link.type
        algorithm = link.algorithm
        digits = link.digits
        period = link.period
        counter = link.counter
    }
}

enum SecretInput: Equatable {
    case keep
    case key(Data)
    case link(Account)
    case invalid
}

enum ParameterEdit {
    static func secretInput(_ raw: String) -> SecretInput {
        let text = TextCleaning.clean(raw)
        guard !text.isEmpty else { return .keep }
        switch InputDetect.classify(text) {
        case .otpauth:
            return (try? OTPAuth.parse(text)).map(SecretInput.link) ?? .invalid
        case .migration, .exportJSON, .exportCSV, .exportBinary:
            return .invalid
        case .setupKey, .invalid:
            guard let secret = try? Base32.decode(text), !secret.isEmpty else { return .invalid }
            return .key(secret)
        }
    }

    static func updated(_ account: Account, with draft: ParameterDraft) throws -> Account {
        var a = account
        a.type = draft.type
        a.algorithm = draft.algorithm
        a.digits = draft.type == .steam ? 5 : draft.digits
        a.period = draft.type == .steam ? 30 : draft.period
        a.counter = draft.type == .hotp ? draft.counter : account.counter
        switch secretInput(draft.secretText) {
        case .keep: break
        case .key(let secret): a.secret = secret
        case .link(let link): a.secret = link.secret
        case .invalid: throw ParameterEditError.invalidSecret
        }
        try a.validate()
        return a
    }

    static func changesCodes(_ account: Account, draft: ParameterDraft) -> Bool {
        if secretInput(draft.secretText) != .keep { return true }
        guard let a = try? updated(account, with: draft) else { return true }
        return a.type != account.type || a.algorithm != account.algorithm || a.digits != account.digits
            || (a.type != .hotp && a.period != account.period)
            || (a.type == .hotp && a.counter != account.counter)
    }

    static func needsConfirmation(_ account: Account, draft: ParameterDraft) -> Bool {
        draft.type != account.type || secretInput(draft.secretText) != .keep
    }

    static func confirmationTitle(for account: Account) -> String {
        "Change the codes for \(account.displayName)?"
    }

    static let confirmationMessage =
        "Tessera will show different codes. Sign-in works only if the service uses the same secret and settings."
}

enum ParameterEditError: Error, Equatable {
    case invalidSecret

    var text: String { "Paste a setup key or an otpauth link." }
}

enum TagDraft {
    static func normalized(_ tags: [String]) -> [String] {
        var seen = Set<String>()
        return tags.compactMap(VaultEdit.normalizedTag).filter { seen.insert($0).inserted }
    }

    static func adding(_ raw: String, to tags: [String]) -> [String] {
        guard let tag = VaultEdit.normalizedTag(raw), !tags.contains(tag) else { return tags }
        return tags + [tag]
    }
}

extension AppModel {
    func applyDetails(_ account: Account, tags: [String], parameters draft: ParameterDraft) -> String? {
        do {
            guard let i = accounts.firstIndex(where: { $0.id == account.id }) else { return nil }
            var a = try ParameterEdit.updated(accounts[i], with: draft)
            a.tags = TagDraft.normalized(tags)
            guard a != accounts[i] else { return nil }
            a.updatedAt = Int64(Date().timeIntervalSince1970)
            try stage(VaultChange.replacing(account.id, from: accounts[i], with: a))
            return nil
        } catch let e as ParameterEditError {
            return e.text
        } catch {
            return UserMessage.textUnlessCancelled(for: error, context: .save)
        }
    }
}
