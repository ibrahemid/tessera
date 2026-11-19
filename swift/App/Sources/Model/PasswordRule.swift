import Foundation

/// The rule for a password the user creates: a backup password or a recovery
/// passphrase. Opening an existing backup accepts any non-empty password, since
/// older backups may predate the rule.
enum PasswordRule {
    static let minLength = 8

    enum Issue: Equatable {
        case tooShort
        case mismatch
    }

    /// The first problem that stops `password` being used, or nil when it can.
    /// A confirmation that hasn't been typed yet counts as a mismatch, so the
    /// action stays disabled until both fields agree.
    static func issue(password: String, confirm: String) -> Issue? {
        if password.count < minLength { return .tooShort }
        if password != confirm { return .mismatch }
        return nil
    }

    /// The issue to show inline: none until the confirmation has text, so the
    /// first field can be typed without an error under it.
    static func visibleIssue(password: String, confirm: String) -> Issue? {
        confirm.isEmpty ? nil : issue(password: password, confirm: confirm)
    }

    static func text(for issue: Issue, noun: String) -> String {
        switch issue {
        case .tooShort: return "Use at least \(minLength) characters."
        case .mismatch: return "The \(noun)s don’t match."
        }
    }
}
