import SwiftUI
import AppKit
import TesseraCore

// MARK: Settings

enum SettingsTab: String, CaseIterable, Identifiable {
    case general, security, vault, backup
    var id: String { rawValue }
    var title: String {
        switch self {
        case .general: return "General"
        case .security: return "Security"
        case .vault: return "Vault"
        case .backup: return "Backup"
        }
    }
    var symbol: String {
        switch self {
        case .general: return "gearshape"
        case .security: return "lock"
        case .vault: return "externaldrive"
        case .backup: return "arrow.up.doc"
        }
    }
}

/// The control whose result a feedback line belongs to; each line shows under
/// its own control.
enum SettingsFeedbackSlot: Hashable {
    case touchID, passphrase, vaultFile, duplicates, export, restore, links
}

/// A tab grows by the result lines it shows, so each line stays in view next to
/// its control instead of scrolling below the window.
enum SettingsTabHeight {
    static let infoLine: CGFloat = 40
    static let failureLine: CGFloat = 72

    static func height(base: CGFloat, showing notices: [AppModel.Notice]) -> CGFloat {
        notices.reduce(base) { total, notice in
            switch notice {
            case .info: return total + infoLine
            case .failure: return total + failureLine
            }
        }
    }
}

struct SettingsView: View {
    @EnvironmentObject var model: AppModel
    @AppStorage("tessera.theme") private var theme: AppTheme = .system
    @StateObject private var launchAtLogin: LaunchAtLogin
    @State private var tab: SettingsTab
    @State private var feedback: [SettingsFeedbackSlot: AppModel.Notice]
    @State private var exporting = false
    @State private var restoring = false
    @State private var settingPassphrase = false
    @State private var confirmReset = false
    @State private var pendingDuplicates: [Account] = []
    @State private var confirmDuplicates = false
    /// Renders one tab without the tab bar, for screenshots of each tab.
    private let singleTab: SettingsTab?

    init() {
        _launchAtLogin = StateObject(wrappedValue: LaunchAtLogin())
        _tab = State(initialValue: .general)
        _feedback = State(initialValue: [:])
        singleTab = nil
    }

    #if DEBUG
    init(tab: SettingsTab, feedback: [SettingsFeedbackSlot: AppModel.Notice] = [:],
         launchAtLogin: LaunchAtLogin = LaunchAtLogin()) {
        _launchAtLogin = StateObject(wrappedValue: launchAtLogin)
        _tab = State(initialValue: tab)
        _feedback = State(initialValue: feedback)
        singleTab = tab
    }
    #endif

    /// Real version from the bundle, so it never drifts from the build.
    static var appVersion: String {
        let short = Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String ?? "1.0"
        if let build = Bundle.main.object(forInfoDictionaryKey: "CFBundleVersion") as? String, build != short {
            return "\(short) (\(build))"
        }
        return short
    }

    var body: some View {
        tabs
        .frame(width: 460)
        .tint(Palette.accentFill)
        .sheet(isPresented: $exporting) {
            ExportBackupView { url in feedback[.export] = .info("Backup saved as \(url.lastPathComponent).") }
        }
        .sheet(isPresented: $restoring) {
            RestoreBackupView { line in feedback[.restore] = .info(line) }
        }
        .sheet(isPresented: $settingPassphrase) {
            RecoveryPassphraseView { feedback[.passphrase] = .info("Recovery passphrase saved.") }
        }
        .resetTesseraDialog(isPresented: $confirmReset, model: model)
        .confirmationDialog(duplicatesDialog.title, isPresented: $confirmDuplicates, titleVisibility: .visible) {
            Button(duplicatesDialog.action, role: .destructive) { removeDuplicates() }
            Button("Cancel", role: .cancel) {}.keyboardShortcut(.defaultAction)
        } message: {
            Text(duplicatesDialog.message)
        }
        .onReceive(NotificationCenter.default.publisher(for: NSApplication.didBecomeActiveNotification)) { _ in
            launchAtLogin.refresh()
        }
    }

    // MARK: Tabs

    @ViewBuilder
    private var tabs: some View {
        if let singleTab {
            content(for: singleTab)
        } else {
            TabView(selection: $tab) {
                ForEach(SettingsTab.allCases) { t in
                    content(for: t)
                        .tabItem { Label(t.title, systemImage: t.symbol) }
                        .tag(t)
                }
            }
        }
    }

    @ViewBuilder
    private func content(for tab: SettingsTab) -> some View {
        switch tab {
        case .general: generalTab
        case .security: securityTab
        case .vault: vaultTab
        case .backup: backupTab
        }
    }

    private var generalTab: some View {
        Form {
            Section {
                Picker("Theme", selection: $theme) {
                    ForEach(AppTheme.allCases) { Text($0.label).tag($0) }
                }.pickerStyle(.segmented)
            }
            Section {
                Toggle("Launch at login", isOn: Binding(
                    get: { launchAtLogin.isEnabled },
                    set: { launchAtLogin.setEnabled($0) }
                ))
                if launchAtLogin.needsApproval {
                    HStack(alignment: .firstTextBaseline) {
                        hint("Waiting for your approval in System Settings > General > Login Items.")
                        Spacer()
                        Button("Open Login Items") { launchAtLogin.openLoginItemsSettings() }
                    }
                }
                if let e = launchAtLogin.errorText { ErrorLine(e) }
            }
            Section {
                LabeledContent("Version", value: Self.appVersion)
            }
        }
        .formStyle(.grouped)
        .frame(height: 260)
    }

    private var securityTab: some View {
        Form {
            if model.biometricsAvailable {
                Section("Touch ID") {
                    Toggle("Require Touch ID to open", isOn: Binding(
                        get: { model.requireBiometrics },
                        set: { on in
                            Task { feedback[.touchID] = await model.setRequireBiometrics(on).map { .failure($0) } }
                        }
                    )).disabled(model.isLocked)
                    thisMacOnlyNote
                    feedbackLine(.touchID)
                }
            }
            Section("Recovery passphrase") {
                // The wrap list is only known while unlocked, so don't claim a
                // state we can't read.
                if model.isLocked {
                    hint("Unlock the vault to manage its recovery passphrase.")
                } else {
                    if !model.biometricsAvailable { thisMacOnlyNote }
                    LabeledContent("Status", value: model.hasRecoveryPassphrase ? "Set" : "Not set")
                    VaultActionButton(
                        title: model.hasRecoveryPassphrase ? "Change recovery passphrase…" : "Set recovery passphrase…",
                        subtitle: "Opens this vault when Touch ID can’t, and on another Mac with the tess CLI."
                    ) { settingPassphrase = true }
                    feedbackLine(.passphrase)
                }
            }
        }
        .formStyle(.grouped)
        .frame(height: tabHeight(360, [.touchID, .passphrase]))
    }

    private var vaultTab: some View {
        Form {
            Section {
                LabeledContent("Location") {
                    Text(model.vaultPathDisplay)
                        .font(Typo.code(11))
                        .textSelection(.enabled)
                        .lineLimit(1).truncationMode(.middle)
                }
                hint(model.vaultStateLine)
                VaultActionButton(
                    title: "Use another vault file…",
                    subtitle: "Switches the app to that file in place. Use this to share one vault with the tess CLI."
                ) {
                    feedback[.vaultFile] = model.openExistingVault(guardSwitch: true).map { .failure($0) }
                }
                if model.isExternalVault {
                    VaultActionButton(
                        title: "Use built-in vault",
                        subtitle: "Switch back to the app’s own vault (\(model.builtInVaultPathDisplay))."
                    ) { model.useBuiltInVault() }
                }
                feedbackLine(.vaultFile)
            }
            Section {
                Button("Remove duplicates…") { reviewDuplicates() }
                    .disabled(model.isLocked || model.accounts.isEmpty)
                feedbackLine(.duplicates)
            }
            Section {
                Button("Reset Tessera…", role: .destructive) { confirmReset = true }
            }
        }
        .formStyle(.grouped)
        .frame(height: tabHeight(380, [.vaultFile, .duplicates]))
    }

    private var backupTab: some View {
        Form {
            Section("Encrypted backup") {
                LabeledContent("Last backup", value: lastBackupText)
                Button("Export encrypted backup…") { exporting = true }
                    .disabled(model.isLocked || model.accounts.isEmpty)
                feedbackLine(.export)
                VStack(alignment: .leading, spacing: 6) {
                    Button("Restore from backup…") { restoring = true }
                        .disabled(model.isLocked)
                    hint("Adds the accounts from a backup to this vault. The backup file isn’t changed.")
                }
                feedbackLine(.restore)
            }
            Section("Plain text") {
                VStack(alignment: .leading, spacing: 6) {
                    Button("Export as otpauth links…") { feedback[.links] = model.exportPlaintextLinks() }
                        .disabled(model.isLocked || model.accounts.isEmpty)
                    hint("Any authenticator app can import these links. The setup keys in the file are readable.")
                }
                feedbackLine(.links)
            }
        }
        .formStyle(.grouped)
        .frame(height: tabHeight(380, [.export, .restore, .links]))
    }

    // MARK: Pieces

    private var lastBackupText: String {
        guard let d = model.lastBackupExport else { return "None on record" }
        return d.formatted(date: .abbreviated, time: .shortened)
    }

    @ViewBuilder
    private var thisMacOnlyNote: some View {
        if model.opensOnThisMacOnly {
            hint(model.requireBiometrics
                 ? "Without a recovery passphrase, this vault opens only on this Mac, and changing your enrolled fingerprints locks it for good. Set a recovery passphrase below or export a backup."
                 : "Without a recovery passphrase, this vault opens only on this Mac. Set a recovery passphrase below or export a backup.")
        }
    }

    private var duplicatesDialog: (title: String, message: String, action: String) {
        DuplicateScan.confirmation(for: pendingDuplicates)
    }

    private func hint(_ text: String) -> some View {
        Text(text)
            .font(Typo.label(11)).foregroundStyle(Palette.textSecondary)
            .fixedSize(horizontal: false, vertical: true)
    }

    private func tabHeight(_ base: CGFloat, _ slots: [SettingsFeedbackSlot]) -> CGFloat {
        SettingsTabHeight.height(base: base, showing: slots.compactMap { feedback[$0] })
    }

    @ViewBuilder
    private func feedbackLine(_ slot: SettingsFeedbackSlot) -> some View {
        if let f = feedback[slot] { NoticeLine(f) }
    }

    private func reviewDuplicates() {
        let found = model.duplicatesToRemove
        if found.isEmpty {
            feedback[.duplicates] = .info("No duplicates found.")
        } else {
            feedback[.duplicates] = nil
            pendingDuplicates = found
            confirmDuplicates = true
        }
    }

    private func removeDuplicates() {
        do {
            let n = try model.removeDuplicates()
            feedback[.duplicates] = .info("Removed \(n) duplicate\(n == 1 ? "" : "s").")
        } catch {
            feedback[.duplicates] = UserMessage.textUnlessCancelled(for: error, context: .save).map { .failure($0) }
        }
        pendingDuplicates = []
    }
}

// MARK: Sheets

/// Footer shared by the password sheets: a busy line on the left, Cancel and the
/// default action on the right.
private struct SheetFooter: View {
    let busyText: String?
    let actionTitle: String
    let actionEnabled: Bool
    let cancel: () -> Void
    let action: () -> Void

    var body: some View {
        HStack(spacing: 10) {
            if let busyText {
                ProgressView().controlSize(.small)
                Text(busyText).font(Typo.label(12)).foregroundStyle(Palette.textSecondary)
            }
            Spacer()
            Button("Cancel", action: cancel).buttonStyle(.plain).foregroundStyle(Palette.textSecondary)
                .keyboardShortcut(.cancelAction)
                .disabled(busyText != nil)
            ActionButton(actionTitle, enabled: actionEnabled, action: action)
        }
    }
}

/// Add or replace the vault's recovery passphrase. Requires an unlocked vault:
/// the DEK is already in hand, so this only adds a wrap; the payload and the
/// Touch ID wrap are untouched.
struct RecoveryPassphraseView: View {
    @EnvironmentObject var model: AppModel
    @Environment(\.dismiss) var dismiss
    var onSaved: () -> Void = {}
    @State private var passphrase = ""
    @State private var confirm = ""
    @State private var saving = false
    @State private var error: String?
    @FocusState private var firstFocused: Bool

    init(onSaved: @escaping () -> Void = {}) {
        self.onSaved = onSaved
    }

    #if DEBUG
    init(passphrase: String, confirm: String, saving: Bool = false, error: String? = nil) {
        _passphrase = State(initialValue: passphrase)
        _confirm = State(initialValue: confirm)
        _saving = State(initialValue: saving)
        _error = State(initialValue: error)
    }
    #endif

    private var issue: PasswordRule.Issue? { PasswordRule.issue(password: passphrase, confirm: confirm) }

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            Text(model.hasRecoveryPassphrase ? "Change recovery passphrase" : "Set recovery passphrase")
                .font(Typo.display(18)).foregroundStyle(Palette.textPrimary)
            Text("Opens this vault when Touch ID can’t: after you change your enrolled fingerprints, or from the tess CLI. At least \(PasswordRule.minLength) characters.")
                .font(Typo.label(12)).foregroundStyle(Palette.textSecondary)
                .fixedSize(horizontal: false, vertical: true)
            FieldBox { SecureField("Recovery passphrase", text: $passphrase).focused($firstFocused) }
            FieldBox { SecureField("Confirm passphrase", text: $confirm).onSubmit(save) }
            if let i = PasswordRule.visibleIssue(password: passphrase, confirm: confirm) {
                ErrorLine(PasswordRule.text(for: i, noun: "passphrase"))
            }
            if let e = error { ErrorLine(e) }
            SheetFooter(busyText: saving ? "Saving…" : nil, actionTitle: "Save",
                        actionEnabled: issue == nil && !saving,
                        cancel: { dismiss() }, action: save)
        }
        .disabled(saving)
        .padding(20).frame(width: 400).background(Palette.background)
        .background(CaptureShield())
        .onAppear { firstFocused = true }
    }

    private func save() {
        guard issue == nil, !saving else { return }
        saving = true
        error = nil
        Task {
            let reason = await model.setRecoveryPassphrase(passphrase)
            saving = false
            if let reason { error = reason } else { onSaved(); dismiss() }
        }
    }
}

struct ExportBackupView: View {
    @EnvironmentObject var model: AppModel
    @Environment(\.dismiss) var dismiss
    var onSaved: (URL) -> Void = { _ in }
    @State private var password = ""
    @State private var confirm = ""
    @State private var busy = false
    @State private var error: String?
    @FocusState private var firstFocused: Bool

    #if DEBUG
    init(password: String = "", confirm: String = "", busy: Bool = false, error: String? = nil) {
        _password = State(initialValue: password)
        _confirm = State(initialValue: confirm)
        _busy = State(initialValue: busy)
        _error = State(initialValue: error)
    }
    #endif

    init(onSaved: @escaping (URL) -> Void) {
        self.onSaved = onSaved
    }

    private var issue: PasswordRule.Issue? { PasswordRule.issue(password: password, confirm: confirm) }

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            Text("Export encrypted backup").font(Typo.display(18)).foregroundStyle(Palette.textPrimary)
            Text("Encrypts every account in this vault with a password of at least \(PasswordRule.minLength) characters. You’ll need it to restore.")
                .font(Typo.label(12)).foregroundStyle(Palette.textSecondary)
                .fixedSize(horizontal: false, vertical: true)
            FieldBox { SecureField("Backup password", text: $password).focused($firstFocused) }
            FieldBox { SecureField("Confirm password", text: $confirm).onSubmit(submit) }
            if let i = PasswordRule.visibleIssue(password: password, confirm: confirm) {
                ErrorLine(PasswordRule.text(for: i, noun: "password"))
            }
            if let e = error { ErrorLine(e) }
            SheetFooter(busyText: busy ? "Encrypting…" : nil, actionTitle: "Export…",
                        actionEnabled: issue == nil && !busy,
                        cancel: { dismiss() }, action: submit)
        }
        .disabled(busy)
        .padding(20).frame(width: 400).background(Palette.background)
        .background(CaptureShield())
        .onAppear { firstFocused = true }
    }

    private func submit() {
        guard issue == nil, !busy, let url = model.chooseBackupDestination() else { return }
        busy = true
        error = nil
        Task {
            let outcome = await model.exportBackup(to: url, password: password)
            busy = false
            switch outcome {
            case .saved(let saved): onSaved(saved); dismiss()
            case .failed(let message): error = message
            }
        }
    }
}

/// Restore a Tessera encrypted backup: the file first, then its password.
struct RestoreBackupView: View {
    @EnvironmentObject var model: AppModel
    @Environment(\.dismiss) var dismiss
    var onRestored: (String) -> Void = { _ in }
    @State private var file: URL?
    @State private var password = ""
    @State private var busy = false
    @State private var error: String?
    @FocusState private var passwordFocused: Bool
    private let checksFileOnAppear: Bool

    init(file: URL? = nil, onRestored: @escaping (String) -> Void = { _ in }) {
        _file = State(initialValue: file)
        self.onRestored = onRestored
        checksFileOnAppear = true
    }

    #if DEBUG
    init(file: URL?, password: String, busy: Bool = false, error: String? = nil) {
        _file = State(initialValue: file)
        _password = State(initialValue: password)
        _busy = State(initialValue: busy)
        _error = State(initialValue: error)
        checksFileOnAppear = false
    }
    #endif

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            Text("Restore from backup").font(Typo.display(18)).foregroundStyle(Palette.textPrimary)
            Text("Adds the accounts from a Tessera backup to this vault. Accounts already here are skipped, and the backup file isn’t changed.")
                .font(Typo.label(12)).foregroundStyle(Palette.textSecondary)
                .fixedSize(horizontal: false, vertical: true)
            if let file {
                HStack(spacing: 8) {
                    Image(systemName: "doc").foregroundStyle(Palette.textSecondary)
                        .accessibilityHidden(true)
                    Text(file.lastPathComponent).font(Typo.code(12)).foregroundStyle(Palette.textPrimary)
                        .lineLimit(1).truncationMode(.middle)
                    Spacer()
                    Button("Choose another…") { choose() }
                        .buttonStyle(.plain).font(Typo.label(12)).foregroundStyle(Palette.accentInk)
                }
                FieldBox { SecureField("Backup password", text: $password).focused($passwordFocused).onSubmit(primary) }
            }
            if let e = error { ErrorLine(e) }
            SheetFooter(busyText: busy ? "Decrypting…" : nil,
                        actionTitle: file == nil ? "Choose file…" : "Restore",
                        actionEnabled: !busy && (file == nil || !password.isEmpty),
                        cancel: { dismiss() }, action: primary)
        }
        .disabled(busy)
        .padding(20).frame(width: 400).background(Palette.background)
        .background(CaptureShield())
        .onAppear {
            if checksFileOnAppear, let file, let problem = BackupFile.problem(at: file) { error = problem; self.file = nil }
            passwordFocused = file != nil
        }
    }

    private func primary() {
        if file == nil { choose() } else { restore() }
    }

    private func choose() {
        guard let url = model.chooseBackupFile() else { return }
        if let problem = BackupFile.problem(at: url) {
            error = problem
            file = nil
            return
        }
        error = nil
        file = url
        passwordFocused = true
    }

    private func restore() {
        guard let file, !password.isEmpty, !busy else { return }
        busy = true
        error = nil
        Task {
            let outcome = await model.restoreBackup(from: file, password: password)
            busy = false
            if let line = outcome.line {
                onRestored(line)
                dismiss()
            } else if case .failed(let message) = outcome {
                error = message
            }
        }
    }
}
