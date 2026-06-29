import SwiftUI
import AppKit
import TesseraCore

// MARK: Root

struct RootView: View {
    @EnvironmentObject var model: AppModel
    var body: some View {
        ZStack {
            Palette.background.ignoresSafeArea()
            Group {
                if let key = model.pendingRecoveryKey {
                    RecoveryKeyView(recoveryKey: key)
                } else if !model.vaultExists {
                    OnboardingView()
                } else if model.isLocked {
                    UnlockView()
                } else {
                    VaultView()
                }
            }
        }
        .tint(Palette.accent)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }
}

// MARK: Brand

struct Wordmark: View {
    var size: CGFloat = 17
    var body: some View {
        HStack(spacing: 7) {
            MosaicMark(side: size + 3)
            Text("Tessera").font(Typo.display(size)).foregroundStyle(Palette.textPrimary)
        }
    }
}

struct MosaicMark: View {
    var side: CGFloat = 20
    private var gap: CGFloat { side * 0.14 }
    private var cell: CGFloat { (side - gap) / 2 }
    private func tile(_ color: Color) -> some View {
        RoundedRectangle(cornerRadius: cell * 0.28).fill(color).frame(width: cell, height: cell)
    }
    var body: some View {
        VStack(spacing: gap) {
            HStack(spacing: gap) { tile(Palette.accent); tile(Palette.textSecondary.opacity(0.45)) }
            HStack(spacing: gap) { tile(Palette.textSecondary.opacity(0.45)); tile(Palette.textSecondary.opacity(0.25)) }
        }
        .frame(width: side, height: side)
    }
}

// MARK: Onboarding

struct OnboardingView: View {
    @EnvironmentObject var model: AppModel
    var body: some View {
        VStack(spacing: 18) {
            Spacer()
            MosaicMark(side: 56)
            VStack(spacing: 8) {
                Text("Welcome to Tessera").font(Typo.display(24)).foregroundStyle(Palette.textPrimary)
                Text(model.biometricsAvailable
                     ? "You'll unlock with Touch ID. We'll give you a recovery key to keep somewhere safe — that's the only thing that can restore your vault if Touch ID ever isn't available."
                     : "We'll give you a recovery key to unlock and restore your vault. Keep it somewhere safe, like a password manager.")
                    .multilineTextAlignment(.center).font(Typo.label(13)).foregroundStyle(Palette.textSecondary)
                    .frame(maxWidth: 380)
            }
            if let e = model.errorMessage { ErrorLine(e) }
            PrimaryButton("Create vault") { model.setUp() }.frame(maxWidth: 260)
            Spacer()
        }
        .padding(32)
    }
}

struct RecoveryKeyView: View {
    @EnvironmentObject var model: AppModel
    let recoveryKey: String
    @State private var saved = false
    @State private var copied = false

    var body: some View {
        VStack(spacing: 18) {
            Spacer()
            Image(systemName: "key.horizontal.fill").font(.system(size: 36)).foregroundStyle(Palette.accent)
            Text("Your recovery key").font(Typo.display(22)).foregroundStyle(Palette.textPrimary)
            Text("Save this now. It's the only way back into your vault if Touch ID isn't available. We can't recover it for you.")
                .multilineTextAlignment(.center).font(Typo.label(13)).foregroundStyle(Palette.textSecondary)
                .frame(maxWidth: 420)

            Text(recoveryKey)
                .font(.system(size: 16, weight: .medium, design: .monospaced))
                .foregroundStyle(Palette.textPrimary)
                .textSelection(.enabled)
                .padding(.horizontal, 16).padding(.vertical, 12)
                .background(Palette.surfaceHi, in: RoundedRectangle(cornerRadius: 10))
                .overlay(RoundedRectangle(cornerRadius: 10).stroke(Palette.border, lineWidth: 1))

            Button {
                NSPasteboard.general.clearContents()
                NSPasteboard.general.setString(recoveryKey, forType: .string)
                copied = true
            } label: {
                Label(copied ? "Copied" : "Copy recovery key", systemImage: copied ? "checkmark" : "doc.on.doc")
                    .font(Typo.label(12, .medium))
            }
            .buttonStyle(.plain).foregroundStyle(Palette.accent)

            Toggle("I've saved my recovery key somewhere safe", isOn: $saved)
                .toggleStyle(.checkbox).font(Typo.label(12)).foregroundStyle(Palette.textSecondary)

            PrimaryButton("Continue", enabled: saved) { model.acknowledgeRecoveryKey() }.frame(maxWidth: 260)
            Spacer()
        }
        .padding(32)
    }
}

// MARK: Unlock

struct UnlockView: View {
    @EnvironmentObject var model: AppModel
    @State private var showRecovery = false
    @State private var recoveryKey = ""
    @State private var triedAuto = false

    var body: some View {
        VStack(spacing: 16) {
            Spacer()
            MosaicMark(side: 50)
            Text("Tessera is locked").font(Typo.display(20)).foregroundStyle(Palette.textPrimary)

            if model.touchIDAvailableForUnlock && !showRecovery {
                Button { model.unlockWithTouchID() } label: {
                    Label("Unlock with Touch ID", systemImage: "touchid")
                        .font(Typo.label(13, .semibold)).foregroundStyle(.white)
                        .frame(maxWidth: 260).padding(.vertical, 10)
                        .background(Palette.accent, in: RoundedRectangle(cornerRadius: 10))
                }.buttonStyle(.plain)
                Button("Use recovery key") { showRecovery = true }
                    .buttonStyle(.plain).font(Typo.label(12)).foregroundStyle(Palette.textSecondary)
            } else {
                Text("Enter your recovery key").font(Typo.label(12)).foregroundStyle(Palette.textSecondary)
                FieldBox { TextField("XXXX-XXXX-…", text: $recoveryKey).onSubmit { model.unlock(recoveryKey: recoveryKey) } }
                    .frame(maxWidth: 320)
                PrimaryButton("Unlock", enabled: !recoveryKey.isEmpty) { model.unlock(recoveryKey: recoveryKey) }
                    .frame(maxWidth: 320)
                if model.touchIDAvailableForUnlock {
                    Button("Use Touch ID instead") { showRecovery = false }
                        .buttonStyle(.plain).font(Typo.label(12)).foregroundStyle(Palette.textSecondary)
                }
            }
            if let e = model.errorMessage { ErrorLine(e) }
            Spacer()
        }
        .padding(32)
        .onAppear {
            if model.touchIDAvailableForUnlock && !triedAuto {
                triedAuto = true
                model.unlockWithTouchID()
            }
        }
    }
}

// MARK: Vault (windowed)

private enum SidebarItem: Hashable {
    case all, pinned, folder(String)
}

struct VaultView: View {
    @EnvironmentObject var model: AppModel
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.openSettings) private var openSettings
    @State private var selection: SidebarItem? = .all
    @State private var query = ""
    @State private var showingAdd = false
    @State private var copiedID: String?

    private var folders: [String] {
        Array(Set(model.accounts.map(\.folder).filter { !$0.isEmpty })).sorted()
    }

    private var visible: [Account] {
        var list = model.accounts
        switch selection ?? .all {
        case .all: break
        case .pinned: list = list.filter(\.pinned)
        case .folder(let f): list = list.filter { $0.folder == f }
        }
        if !query.isEmpty {
            let q = query.lowercased()
            list = list.filter { "\($0.issuer) \($0.account)".lowercased().contains(q) }
        }
        return list.sorted {
            ($0.pinned ? 0 : 1, ($0.issuer.isEmpty ? $0.account : $0.issuer).lowercased())
                < ($1.pinned ? 0 : 1, ($1.issuer.isEmpty ? $1.account : $1.issuer).lowercased())
        }
    }

    var body: some View {
        NavigationSplitView {
            List(selection: $selection) {
                Section {
                    Label("All", systemImage: "square.grid.2x2").tag(SidebarItem.all)
                    Label("Pinned", systemImage: "star").tag(SidebarItem.pinned)
                }
                if !folders.isEmpty {
                    Section("Folders") {
                        ForEach(folders, id: \.self) { f in
                            Label(f, systemImage: "folder").tag(SidebarItem.folder(f))
                        }
                    }
                }
            }
            .navigationSplitViewColumnWidth(min: 180, ideal: 210, max: 280)
            .safeAreaInset(edge: .bottom) {
                HStack {
                    Wordmark(size: 13)
                    Spacer()
                }.padding(10)
            }
        } detail: {
            detailList
                .navigationTitle("")
                .toolbar {
                    ToolbarItem(placement: .primaryAction) {
                        Button { showingAdd = true } label: { Image(systemName: "plus") }
                    }
                    ToolbarItem(placement: .automatic) {
                        Button { openSettings() } label: { Image(systemName: "gearshape") }
                    }
                    ToolbarItem(placement: .automatic) {
                        Button { model.lock() } label: { Image(systemName: "lock") }
                    }
                }
                .searchable(text: $query, placement: .toolbar, prompt: "Search")
        }
        .sheet(isPresented: $showingAdd) { AddAccountView() }
    }

    @ViewBuilder private var detailList: some View {
        if model.accounts.isEmpty {
            EmptyState { showingAdd = true }
        } else if visible.isEmpty {
            VStack(spacing: 8) {
                Image(systemName: "magnifyingglass").font(.system(size: 24)).foregroundStyle(Palette.textFaint)
                Text("No matches").font(Typo.label(13)).foregroundStyle(Palette.textSecondary)
            }.frame(maxWidth: .infinity, maxHeight: .infinity)
        } else {
            ScrollView {
                LazyVStack(spacing: 8) {
                    ForEach(visible, id: \.id) { account in
                        AccountRowView(account: account,
                                       remaining: model.remaining(for: account),
                                       code: model.code(for: account),
                                       copied: copiedID == account.id,
                                       reduceMotion: reduceMotion,
                                       onCopy: { copy(account) },
                                       onPin: { model.togglePin(account) },
                                       onDelete: { model.remove(account) },
                                       onAdvance: { model.advanceHOTP(account) })
                    }
                }
                .padding(16)
                .frame(maxWidth: 680)
                .frame(maxWidth: .infinity)
            }
        }
    }

    private func copy(_ account: Account) {
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(model.code(for: account), forType: .string)
        withAnimation(.spring(response: 0.3, dampingFraction: 0.7)) { copiedID = account.id }
        let id = account.id
        Task { @MainActor in
            try? await Task.sleep(for: .seconds(1.3))
            if copiedID == id { withAnimation { copiedID = nil } }
        }
    }
}

// MARK: Account row

struct AccountRowView: View {
    let account: Account
    let remaining: Int
    let code: String
    let copied: Bool
    var reduceMotion: Bool
    var onCopy: () -> Void
    var onPin: () -> Void
    var onDelete: () -> Void
    var onAdvance: () -> Void

    var body: some View {
        PressableTile(action: account.type == .hotp ? onAdvance : onCopy) {
            HStack(spacing: 12) {
                TesseraTile(account: account, remaining: remaining, reduceMotion: reduceMotion)
                VStack(alignment: .leading, spacing: 2) {
                    Text(account.issuer.isEmpty ? account.account : account.issuer)
                        .font(Typo.label(13, .semibold)).foregroundStyle(Palette.textPrimary).lineLimit(1)
                    if !account.issuer.isEmpty {
                        Text(account.account).font(Typo.label(11)).foregroundStyle(Palette.textSecondary).lineLimit(1)
                    }
                }
                Spacer(minLength: 8)
                Text(groupCode(code))
                    .font(Typo.code(18)).foregroundStyle(copied ? Palette.accent : Palette.textPrimary)
                    .contentTransition(.numericText())
                ZStack {
                    Image(systemName: "doc.on.doc").opacity(copied ? 0 : 1)
                    Image(systemName: "checkmark").opacity(copied ? 1 : 0)
                }
                .font(.system(size: 12, weight: .semibold))
                .foregroundStyle(copied ? Palette.accent : Palette.textFaint).frame(width: 16)
            }
            .padding(.horizontal, 12).padding(.vertical, 11)
            .scaleEffect(copied && !reduceMotion ? 1.01 : 1)
        }
        .contextMenu {
            Button(account.pinned ? "Unpin" : "Pin", systemImage: "pin") { onPin() }
            if account.type == .hotp { Button("Advance code", systemImage: "forward") { onAdvance() } }
            Button("Copy code", systemImage: "doc.on.doc") { onCopy() }
            Divider()
            Button("Delete", systemImage: "trash", role: .destructive) { onDelete() }
        }
    }
}

// MARK: Add account

struct AddAccountView: View {
    @EnvironmentObject var model: AppModel
    @Environment(\.dismiss) var dismiss
    @State private var uri = ""
    @State private var scanning = false

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            HStack { Text("Add accounts").font(Typo.display(18)).foregroundStyle(Palette.textPrimary); Spacer() }
            Text("Paste one or more otpauth or Google Authenticator links (one per line), scan a QR on screen, or import a file.")
                .font(Typo.label(12)).foregroundStyle(Palette.textSecondary)
            FieldBox { TextField("otpauth://…", text: $uri, axis: .vertical).lineLimit(3...8) }
            HStack(spacing: 16) {
                Button {
                    scanning = true
                    Task { defer { scanning = false }
                        do { uri = try await QRCapture.scanScreen() } catch { model.errorMessage = "\(error)" } }
                } label: {
                    Label(scanning ? "Scanning…" : "Scan QR on screen", systemImage: "qrcode.viewfinder").font(Typo.label(12, .medium))
                }.buttonStyle(.plain).foregroundStyle(Palette.accent).disabled(scanning)
                Button { model.importFromFile(); if model.errorMessage == nil { dismiss() } } label: {
                    Label("Import file…", systemImage: "doc.text").font(Typo.label(12, .medium))
                }.buttonStyle(.plain).foregroundStyle(Palette.accent)
            }
            if let e = model.errorMessage { ErrorLine(e) }
            HStack {
                Spacer()
                Button("Cancel") { dismiss() }.buttonStyle(.plain).foregroundStyle(Palette.textSecondary)
                PrimaryButton("Add", enabled: !uri.isEmpty) { add() }.fixedSize()
            }
        }
        .padding(20).frame(width: 420).background(Palette.background)
    }

    private func add() {
        let added = model.importText(uri)
        if model.errorMessage == nil && added > 0 { dismiss() }
    }
}

// MARK: Settings

struct SettingsView: View {
    @EnvironmentObject var model: AppModel
    @AppStorage("tessera.theme") private var theme: AppTheme = .system
    @AppStorage("tessera.launchAtLogin") private var launchAtLogin = false

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
                    Toggle("Unlock with Touch ID", isOn: Binding(
                        get: { model.hasTouchIDWrap },
                        set: { $0 ? model.enableTouchID() : model.disableTouchID() }
                    )).disabled(model.isLocked)
                }
            }
            Section {
                LabeledContent("Vault", value: model.vaultPathDisplay)
                LabeledContent("Version", value: "1.0.0")
            }
            if let e = model.errorMessage { ErrorLine(e) }
        }
        .formStyle(.grouped)
        .frame(width: 420, height: 360)
        .tint(Palette.accent)
    }
}

// MARK: Shared building blocks

struct FieldBox<Content: View>: View {
    @ViewBuilder var content: Content
    var body: some View {
        content
            .textFieldStyle(.plain).font(Typo.label(13))
            .padding(.horizontal, 11).padding(.vertical, 9)
            .background(Palette.surfaceHi, in: RoundedRectangle(cornerRadius: 9))
            .overlay(RoundedRectangle(cornerRadius: 9).stroke(Palette.border, lineWidth: 1))
    }
}

struct PrimaryButton: View {
    let title: String; var enabled: Bool; var action: () -> Void
    init(_ title: String, enabled: Bool = true, action: @escaping () -> Void) {
        self.title = title; self.enabled = enabled; self.action = action
    }
    var body: some View {
        Button(action: action) {
            Text(title).font(Typo.label(13, .semibold)).foregroundStyle(.white)
                .frame(maxWidth: .infinity).padding(.vertical, 9)
                .background(Palette.accent.opacity(enabled ? 1 : 0.4), in: RoundedRectangle(cornerRadius: 9))
        }
        .buttonStyle(.plain).disabled(!enabled)
    }
}

struct SectionLabel: View {
    let text: String
    init(_ text: String) { self.text = text }
    var body: some View {
        HStack {
            Text(text.uppercased()).font(Typo.label(10, .semibold)).tracking(0.6).foregroundStyle(Palette.textFaint)
            Spacer()
        }.padding(.horizontal, 4).padding(.top, 2)
    }
}

struct ErrorLine: View {
    let text: String
    init(_ text: String) { self.text = text }
    var body: some View {
        Label(text, systemImage: "exclamationmark.triangle.fill").font(Typo.label(11)).foregroundStyle(Palette.warning)
    }
}

struct EmptyState: View {
    var add: () -> Void
    var body: some View {
        VStack(spacing: 12) {
            MosaicMark(side: 44)
            Text("No accounts yet").font(Typo.display(16)).foregroundStyle(Palette.textPrimary)
            Text("Add your first account from an otpauth link or a QR code on screen.")
                .multilineTextAlignment(.center).font(Typo.label(12)).foregroundStyle(Palette.textSecondary).frame(maxWidth: 320)
            Button(action: add) {
                Label("Add account", systemImage: "plus").font(Typo.label(12, .semibold)).foregroundStyle(Palette.accent)
            }.buttonStyle(.plain).padding(.top, 2)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }
}
