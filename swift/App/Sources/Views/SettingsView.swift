import SwiftUI

// MARK: Settings

struct SettingsView: View {
    @EnvironmentObject var model: AppModel
    @AppStorage("tessera.theme") private var theme: AppTheme = .system
    @AppStorage("tessera.launchAtLogin") private var launchAtLogin = false
    @State private var exporting = false
    @State private var restoring = false
    @State private var settingPassphrase = false
    @State private var confirmReset = false
    @State private var dedupeResult: Int?

    /// Real version from the bundle, so it never drifts from the build.
    static var appVersion: String {
        let short = Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String ?? "1.0"
        if let build = Bundle.main.object(forInfoDictionaryKey: "CFBundleVersion") as? String, build != short {
            return "\(short) (\(build))"
        }
        return short
    }

    var body: some View {
        Form {
            Section("Appearance") {
                Picker("Theme", selection: $theme) {
                    ForEach(AppTheme.allCases) { Text($0.label).tag($0) }
                }.pickerStyle(.segmented)
            }
            Section("General") {
                Toggle("Launch at login", isOn: $launchAtLogin)
                    .onChange(of: launchAtLogin) { _, on in LoginItem.set(enabled: on) }
                if model.biometricsAvailable {
                    Toggle("Require Touch ID to open", isOn: Binding(
                        get: { model.requireBiometrics },
                        set: { model.setRequireBiometrics($0) }
                    )).disabled(model.isLocked)
                    // Non-blocking: a Touch ID-gated key dies with the enrolled
                    // fingerprint set, and only a passphrase wrap survives that.
                    if model.requireBiometrics && !model.hasRecoveryPassphrase && !model.isLocked {
                        Text("Without a recovery passphrase, changing your enrolled fingerprints locks this vault for good.")
                            .font(Typo.label(11)).foregroundStyle(Palette.textSecondary)
                            .fixedSize(horizontal: false, vertical: true)
                    }
                }
            }
            Section("Recovery") {
                // The wrap list is only known while unlocked, so don't claim a
                // state we can't read.
                if model.isLocked {
                    Text("Unlock the vault to manage its recovery passphrase.")
                        .font(Typo.label(11)).foregroundStyle(Palette.textSecondary)
                } else {
                    LabeledContent("Recovery passphrase",
                                   value: model.hasRecoveryPassphrase ? "Set" : "Not set")
                    VaultActionButton(
                        title: model.hasRecoveryPassphrase ? "Change recovery passphrase…" : "Set recovery passphrase…",
                        subtitle: "A second way into this vault when Touch ID can't open it. The tess CLI opens the vault with it too."
                    ) { settingPassphrase = true }
                }
            }
            Section("Backup") {
                Button("Export encrypted backup…") { exporting = true }
                    .disabled(model.isLocked || model.accounts.isEmpty)
                Button("Export as otpauth links…") { model.exportPlaintextLinks() }
                    .disabled(model.isLocked || model.accounts.isEmpty)
            }
            Section("Accounts") {
                // nil means the write failed; the error line below is the report.
                Button("Remove duplicates") { dedupeResult = model.removeDuplicates() }
                    .disabled(model.isLocked || model.accounts.isEmpty)
                if let n = dedupeResult {
                    Text(n == 0 ? "No duplicates found" : "Removed \(n) duplicate\(n == 1 ? "" : "s")")
                        .font(Typo.label(11)).foregroundStyle(Palette.textSecondary)
                }
            }
            Section("Vault") {
                LabeledContent("Location") {
                    Text(model.vaultPathDisplay)
                        .font(Typo.code(11))
                        .textSelection(.enabled)
                        .lineLimit(1).truncationMode(.middle)
                }
                Text(model.vaultStateLine)
                    .font(Typo.label(11)).foregroundStyle(Palette.textSecondary)
                VaultActionButton(
                    title: "Use another vault file…",
                    subtitle: "Switches the app to that file in place. Use this to share one vault with the tess CLI."
                ) { model.openExistingVault(guardSwitch: true) }
                VaultActionButton(
                    title: "Restore from backup…",
                    subtitle: "Adds the accounts from an encrypted backup into the current vault. The backup file is not modified."
                ) { restoring = true }
                    .disabled(model.isLocked)
                if model.isExternalVault {
                    VaultActionButton(
                        title: "Use built-in vault",
                        subtitle: "Switch back to the app's own vault (\(model.builtInVaultPathDisplay))."
                    ) { model.useBuiltInVault() }
                }
            }
            Section {
                LabeledContent("Version", value: Self.appVersion)
            }
            Section {
                Button("Reset Tessera…", role: .destructive) { confirmReset = true }
            }
            if let e = model.errorMessage { ErrorLine(e) }
        }
        .formStyle(.grouped)
        .frame(width: 420, height: 420)
        .tint(Palette.accent)
        .sheet(isPresented: $exporting) { ExportBackupView() }
        .sheet(isPresented: $restoring) { RestoreBackupView() }
        .sheet(isPresented: $settingPassphrase) { RecoveryPassphraseView() }
        .resetTesseraDialog(isPresented: $confirmReset, model: model,
                            note: model.isExternalVault ? nil : "Export a backup first if you want to keep them.")
    }
}

/// Add or replace the vault's recovery passphrase. Requires an unlocked vault:
/// the DEK is already in hand, so this only adds a wrap — the payload and the
/// Touch ID wrap are untouched.
struct RecoveryPassphraseView: View {
    @EnvironmentObject var model: AppModel
    @Environment(\.dismiss) var dismiss
    @State private var passphrase = ""
    @State private var confirm = ""
    @State private var saving = false
    @State private var error: String?

    private var canSave: Bool {
        passphrase.count >= AppModel.minPassphraseLength && passphrase == confirm && !saving
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            Text(model.hasRecoveryPassphrase ? "Change recovery passphrase" : "Set recovery passphrase")
                .font(Typo.display(18)).foregroundStyle(Palette.textPrimary)
            Text("Opens this vault when Touch ID can't: after you change your enrolled fingerprints, or from the tess CLI. At least \(AppModel.minPassphraseLength) characters.")
                .font(Typo.label(12)).foregroundStyle(Palette.textSecondary)
                .fixedSize(horizontal: false, vertical: true)
            FieldBox { SecureField("Recovery passphrase", text: $passphrase) }
            FieldBox { SecureField("Confirm passphrase", text: $confirm).onSubmit(save) }
            if !confirm.isEmpty && passphrase != confirm { ErrorLine("Passphrases don't match") }
            if let e = error { ErrorLine(e) }
            HStack {
                Spacer()
                Button("Cancel") { dismiss() }.buttonStyle(.plain).foregroundStyle(Palette.textSecondary)
                    .keyboardShortcut(.cancelAction)
                ActionButton(saving ? "Saving…" : "Save", enabled: canSave) { save() }
            }
        }
        .padding(20).frame(width: 400).background(Palette.background)
        .background(CaptureShield())
    }

    private func save() {
        guard canSave else { return }
        saving = true
        Task {
            let reason = await model.setRecoveryPassphrase(passphrase)
            saving = false
            if let reason { error = reason } else { dismiss() }
        }
    }
}

struct ExportBackupView: View {
    @EnvironmentObject var model: AppModel
    @Environment(\.dismiss) var dismiss
    @State private var password = ""
    @State private var confirm = ""

    private var canExport: Bool { !password.isEmpty && password == confirm }

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            Text("Export encrypted backup").font(Typo.display(18)).foregroundStyle(Palette.textPrimary)
            Text("Set a password to encrypt the backup file. You'll need it to restore.")
                .font(Typo.label(12)).foregroundStyle(Palette.textSecondary)
            FieldBox { SecureField("Backup password", text: $password) }
            FieldBox { SecureField("Confirm password", text: $confirm).onSubmit(submitExport) }
            if !confirm.isEmpty && password != confirm { ErrorLine("Passwords don't match") }
            if let e = model.errorMessage { ErrorLine(e) }
            HStack {
                Spacer()
                Button("Cancel") { dismiss() }.buttonStyle(.plain).foregroundStyle(Palette.textSecondary)
                    .keyboardShortcut(.cancelAction)
                ActionButton("Export", enabled: canExport) { submitExport() }
            }
        }
        .padding(20).frame(width: 380).background(Palette.background)
    }

    private func submitExport() {
        guard canExport else { return }
        Task { if await model.exportBackup(passphrase: password) { dismiss() } }
    }
}

struct RestoreBackupView: View {
    @EnvironmentObject var model: AppModel
    @Environment(\.dismiss) var dismiss
    @State private var password = ""

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            Text("Restore from backup").font(Typo.display(18)).foregroundStyle(Palette.textPrimary)
            Text("Pick a Tessera encrypted backup and enter its password. Accounts merge into your vault; duplicates are skipped.")
                .font(Typo.label(12)).foregroundStyle(Palette.textSecondary)
            FieldBox { SecureField("Backup password", text: $password).onSubmit(restore) }
            if let e = model.errorMessage { ErrorLine(e) }
            HStack {
                Spacer()
                Button("Cancel") { dismiss() }.buttonStyle(.plain).foregroundStyle(Palette.textSecondary)
                    .keyboardShortcut(.cancelAction)
                ActionButton("Choose file & restore", enabled: !password.isEmpty) { restore() }
            }
        }
        .padding(20).frame(width: 400).background(Palette.background)
        .onAppear { model.errorMessage = nil }
    }

    private func restore() {
        guard !password.isEmpty else { return }
        Task { if await model.restoreBackup(passphrase: password) { dismiss() } }
    }
}
