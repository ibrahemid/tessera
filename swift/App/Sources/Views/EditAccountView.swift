import SwiftUI
import TesseraCore

// MARK: Edit account

/// Edit an account's names: issuer, account label, alias (handle), and list
/// (folder). Crypto parameters (type, algorithm, digits, period) are shown but
/// not editable — changing them would silently break the codes. The alias keeps
/// its charset + vault-uniqueness rules, with a factual inline reason on reject.
struct EditAccountView: View {
    @EnvironmentObject var model: AppModel
    @Environment(\.dismiss) var dismiss
    let account: Account
    @State private var issuer: String
    @State private var accountLabel: String
    @State private var alias: String
    @State private var folder: String
    @State private var error: String?

    init(account: Account) {
        self.account = account
        _issuer = State(initialValue: account.issuer)
        _accountLabel = State(initialValue: account.account)
        _alias = State(initialValue: account.handle)
        _folder = State(initialValue: account.folder)
    }

    private var canSave: Bool {
        !alias.trimmingCharacters(in: .whitespaces).isEmpty
            && !(issuer.trimmingCharacters(in: .whitespaces).isEmpty
            && accountLabel.trimmingCharacters(in: .whitespaces).isEmpty)
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("Edit account").font(Typo.display(16)).foregroundStyle(Palette.textPrimary)
                .frame(maxWidth: .infinity)

            labeledField("Issuer") {
                TextField("Service name", text: $issuer)
                    .textFieldStyle(.plain).autocorrectionDisabled()
                    .onChange(of: issuer) { error = nil }
            }
            labeledField("Account") {
                TextField("Username or email", text: $accountLabel)
                    .textFieldStyle(.plain).autocorrectionDisabled()
                    .onChange(of: accountLabel) { error = nil }
            }
            labeledField("Alias") {
                TextField("Short lookup name", text: $alias)
                    .font(Typo.code(13)).textFieldStyle(.plain).autocorrectionDisabled()
                    .onChange(of: alias) { error = nil }
            }
            // Every account carries an alias: the vault format lets the field be
            // absent, but both cores assign one at unlock, so clearing it here
            // would only produce a different auto-assigned alias.
            if alias.trimmingCharacters(in: .whitespaces).isEmpty {
                Text("An alias is required. Type one to save.")
                    .font(Typo.label(11)).foregroundStyle(Palette.textSecondary)
            }
            labeledField("List") {
                TextField("Optional folder", text: $folder)
                    .textFieldStyle(.plain).autocorrectionDisabled()
                    .onChange(of: folder) { error = nil }
            }

            Text(paramSummary(account)).font(Typo.label(11)).foregroundStyle(Palette.textSecondary)

            if let e = error { ErrorLine(e) }

            HStack {
                Button("Cancel") { dismiss() }.buttonStyle(.plain).foregroundStyle(Palette.textSecondary)
                    .keyboardShortcut(.cancelAction)
                Spacer()
                ActionButton("Save", enabled: canSave) {
                    let reason = model.applyEdit(account, issuer: issuer, accountLabel: accountLabel,
                                                 handle: alias, folder: folder)
                    if let reason { error = reason } else { dismiss() }
                }
            }.font(Typo.label(12, .medium))
        }
        .padding(24).frame(width: 320).background(Palette.background)
    }

    @ViewBuilder private func labeledField<Content: View>(_ label: String,
                                                          @ViewBuilder _ field: () -> Content) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(label).font(Typo.label(10, .medium)).foregroundStyle(Palette.textSecondary)
            FieldBox { field() }
        }
    }

    /// The read-only crypto parameters, e.g. "TOTP · SHA1 · 6 digits · 30s".
    private func paramSummary(_ a: Account) -> String {
        var parts = [a.type.rawValue.uppercased(), a.algorithm, "\(a.digits) digits"]
        parts.append(a.type == .hotp ? "counter \(a.counter)" : "\(a.period)s")
        return parts.joined(separator: " · ")
    }
}
