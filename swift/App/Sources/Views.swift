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
                if !model.vaultExists {
                    CreateVaultView()
                } else if model.isLocked {
                    UnlockView()
                } else {
                    VaultView()
                }
            }
        }
        .frame(width: Metrics.windowWidth, height: Metrics.windowHeight)
        .tint(Palette.accent)
    }
}

// MARK: Brandmark

struct Wordmark: View {
    var size: CGFloat = 17
    var body: some View {
        HStack(spacing: 7) {
            MosaicMark(side: size + 3)
            Text("Tessera").font(Typo.display(size)).foregroundStyle(Palette.textPrimary)
        }
    }
}

/// The mosaic mark: four tesserae, one gold. Mirrors the app icon.
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

// MARK: Create vault

struct CreateVaultView: View {
    @EnvironmentObject var model: AppModel
    @State private var pass = ""
    @State private var confirm = ""

    var body: some View {
        VStack(spacing: 18) {
            Spacer()
            MosaicMark(side: 54)
            VStack(spacing: 6) {
                Text("Welcome to Tessera").font(Typo.display(22)).foregroundStyle(Palette.textPrimary)
                Text("Set a passphrase to encrypt your vault.\nIt's shared with the tess command-line app.")
                    .multilineTextAlignment(.center)
                    .font(Typo.label(12)).foregroundStyle(Palette.textSecondary)
            }
            VStack(spacing: 10) {
                FieldBox { SecureField("Passphrase", text: $pass) }
                FieldBox { SecureField("Confirm passphrase", text: $confirm) }
            }
            .frame(maxWidth: 280)
            if let e = model.errorMessage { ErrorLine(e) }
            PrimaryButton("Create vault", enabled: !pass.isEmpty) {
                guard pass.count >= 8 else { model.errorMessage = "Use at least 8 characters."; return }
                guard pass == confirm else { model.errorMessage = "Passphrases don't match."; return }
                model.createVault(passphrase: pass)
            }
            .frame(maxWidth: 280)
            Spacer()
        }
        .padding(24)
    }
}

// MARK: Unlock

struct UnlockView: View {
    @EnvironmentObject var model: AppModel
    @State private var pass = ""

    var body: some View {
        VStack(spacing: 16) {
            Spacer()
            MosaicMark(side: 48)
            Text("Vault locked").font(Typo.display(19)).foregroundStyle(Palette.textPrimary)
            FieldBox {
                SecureField("Passphrase", text: $pass).onSubmit { model.unlock(passphrase: pass) }
            }
            .frame(maxWidth: 260)
            if let e = model.errorMessage { ErrorLine(e) }
            PrimaryButton("Unlock", enabled: !pass.isEmpty) { model.unlock(passphrase: pass) }
                .frame(maxWidth: 260)
            if model.touchIDAvailableForUnlock {
                Button { model.unlockWithTouchID() } label: {
                    Label("Unlock with Touch ID", systemImage: "touchid")
                        .font(Typo.label(12, .medium))
                }
                .buttonStyle(.plain).foregroundStyle(Palette.accent)
            }
            Spacer()
        }
        .padding(24)
    }
}

// MARK: Vault (main)

struct VaultView: View {
    @EnvironmentObject var model: AppModel
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var query = ""
    @State private var showingAdd = false
    @State private var copiedID: String?

    private var groups: (pinned: [Account], rest: [Account]) {
        let q = query.lowercased()
        let filtered = model.accounts.filter {
            q.isEmpty || "\($0.issuer) \($0.account)".lowercased().contains(q)
        }
        .sorted { lhs, rhs in
            let l = (lhs.issuer.isEmpty ? lhs.account : lhs.issuer).lowercased()
            let r = (rhs.issuer.isEmpty ? rhs.account : rhs.issuer).lowercased()
            return l < r
        }
        return (filtered.filter(\.pinned), filtered.filter { !$0.pinned })
    }

    var body: some View {
        VStack(spacing: 0) {
            header
            Divider().overlay(Palette.border)
            content
            Divider().overlay(Palette.border)
            footer
        }
        .sheet(isPresented: $showingAdd) { AddAccountView() }
    }

    private var header: some View {
        VStack(spacing: 10) {
            HStack {
                Wordmark()
                Spacer()
                IconButton(system: "plus") { showingAdd = true }
            }
            HStack(spacing: 8) {
                Image(systemName: "magnifyingglass").font(.system(size: 12)).foregroundStyle(Palette.textFaint)
                TextField("Search", text: $query).textFieldStyle(.plain).font(Typo.label(13))
                if !query.isEmpty {
                    Button { query = "" } label: { Image(systemName: "xmark.circle.fill") }
                        .buttonStyle(.plain).foregroundStyle(Palette.textFaint)
                }
            }
            .padding(.horizontal, 10).padding(.vertical, 7)
            .background(Palette.surfaceHi, in: RoundedRectangle(cornerRadius: 9))
        }
        .padding(Metrics.pad)
    }

    @ViewBuilder private var content: some View {
        let g = groups
        if model.accounts.isEmpty {
            EmptyState { showingAdd = true }
        } else if g.pinned.isEmpty && g.rest.isEmpty {
            VStack(spacing: 6) {
                Spacer()
                Image(systemName: "magnifyingglass").font(.system(size: 22)).foregroundStyle(Palette.textFaint)
                Text("No matches for \"\(query)\"").font(Typo.label(12)).foregroundStyle(Palette.textSecondary)
                Spacer()
            }
            .frame(maxWidth: .infinity)
        } else {
            ScrollView {
                LazyVStack(spacing: 8, pinnedViews: []) {
                    if !g.pinned.isEmpty {
                        SectionLabel("Pinned")
                        ForEach(g.pinned, id: \.id) { row($0) }
                        if !g.rest.isEmpty { SectionLabel("All").padding(.top, 4) }
                    }
                    ForEach(g.rest, id: \.id) { row($0) }
                }
                .padding(Metrics.pad)
            }
        }
    }

    private func row(_ account: Account) -> some View {
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

    private var footer: some View {
        HStack(spacing: 14) {
            FooterButton(system: "lock.fill", title: "Lock") { model.lock() }
            Spacer()
            Text("\(model.accounts.count) accounts").font(Typo.label(11)).foregroundStyle(Palette.textFaint)
            Spacer()
            SettingsLink { FooterButtonLabel(system: "gearshape.fill", title: "Settings") }
                .buttonStyle(.plain)
        }
        .padding(.horizontal, Metrics.pad).padding(.vertical, 9)
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
                        .font(Typo.label(13, .semibold)).foregroundStyle(Palette.textPrimary)
                        .lineLimit(1)
                    if !account.issuer.isEmpty {
                        Text(account.account).font(Typo.label(11)).foregroundStyle(Palette.textSecondary).lineLimit(1)
                    }
                }
                Spacer(minLength: 8)
                Text(groupCode(code))
                    .font(Typo.code(17)).foregroundStyle(copied ? Palette.accent : Palette.textPrimary)
                    .contentTransition(.numericText())
                ZStack {
                    Image(systemName: "doc.on.doc").opacity(copied ? 0 : 1)
                    Image(systemName: "checkmark").opacity(copied ? 1 : 0)
                }
                .font(.system(size: 12, weight: .semibold))
                .foregroundStyle(copied ? Palette.accent : Palette.textFaint)
                .frame(width: 16)
            }
            .padding(.horizontal, 12).padding(.vertical, 10)
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

// MARK: Add

struct AddAccountView: View {
    @EnvironmentObject var model: AppModel
    @Environment(\.dismiss) var dismiss
    @State private var uri = ""
    @State private var scanning = false

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            HStack { Text("Add accounts").font(Typo.display(17)).foregroundStyle(Palette.textPrimary); Spacer() }
            Text("Paste one or more otpauth or Google Authenticator links (one per line), scan a QR on screen, or import a file.")
                .font(Typo.label(12)).foregroundStyle(Palette.textSecondary)
            FieldBox { TextField("otpauth://…", text: $uri, axis: .vertical).lineLimit(3...8) }
            HStack(spacing: 16) {
                Button {
                    scanning = true
                    Task { defer { scanning = false }
                        do { uri = try await QRCapture.scanScreen() } catch { model.errorMessage = "\(error)" } }
                } label: {
                    Label(scanning ? "Scanning…" : "Scan QR on screen", systemImage: "qrcode.viewfinder")
                        .font(Typo.label(12, .medium))
                }
                .buttonStyle(.plain).foregroundStyle(Palette.accent).disabled(scanning)
                Button { model.importFromFile(); if model.errorMessage == nil { dismiss() } } label: {
                    Label("Import file…", systemImage: "doc.text").font(Typo.label(12, .medium))
                }
                .buttonStyle(.plain).foregroundStyle(Palette.accent)
            }
            if let e = model.errorMessage { ErrorLine(e) }
            HStack {
                Spacer()
                Button("Cancel") { dismiss() }.buttonStyle(.plain).foregroundStyle(Palette.textSecondary)
                PrimaryButton("Add", enabled: !uri.isEmpty) { add() }.fixedSize()
            }
        }
        .padding(20).frame(width: 380).background(Palette.background)
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
                if SecureEnclaveWrap.isAvailable {
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
        .frame(width: 380, height: 320)
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

struct IconButton: View {
    let system: String; var action: () -> Void
    @State private var hovering = false
    var body: some View {
        Button(action: action) {
            Image(systemName: system).font(.system(size: 13, weight: .semibold))
                .foregroundStyle(Palette.accent).frame(width: 28, height: 28)
                .background(Palette.accentSoft.opacity(hovering ? 1 : 0.6), in: RoundedRectangle(cornerRadius: 8))
        }
        .buttonStyle(.plain).onHover { hovering = $0 }
    }
}

struct FooterButton: View {
    let system: String; let title: String; var action: () -> Void
    var body: some View {
        Button(action: action) { FooterButtonLabel(system: system, title: title) }.buttonStyle(.plain)
    }
}

struct FooterButtonLabel: View {
    let system: String; let title: String
    var body: some View {
        Label(title, systemImage: system).font(Typo.label(11, .medium)).foregroundStyle(Palette.textSecondary)
    }
}

struct SectionLabel: View {
    let text: String
    init(_ text: String) { self.text = text }
    var body: some View {
        HStack {
            Text(text.uppercased()).font(Typo.label(10, .semibold)).tracking(0.6).foregroundStyle(Palette.textFaint)
            Spacer()
        }
        .padding(.horizontal, 4).padding(.top, 2)
    }
}

struct ErrorLine: View {
    let text: String
    init(_ text: String) { self.text = text }
    var body: some View {
        Label(text, systemImage: "exclamationmark.triangle.fill")
            .font(Typo.label(11)).foregroundStyle(Palette.warning)
    }
}

struct EmptyState: View {
    var add: () -> Void
    var body: some View {
        VStack(spacing: 12) {
            Spacer()
            MosaicMark(side: 44)
            Text("No accounts yet").font(Typo.display(16)).foregroundStyle(Palette.textPrimary)
            Text("Add your first account from an otpauth link\nor a QR code on screen.")
                .multilineTextAlignment(.center).font(Typo.label(12)).foregroundStyle(Palette.textSecondary)
            Button(action: add) {
                Label("Add account", systemImage: "plus").font(Typo.label(12, .semibold)).foregroundStyle(Palette.accent)
            }.buttonStyle(.plain).padding(.top, 2)
            Spacer()
        }
        .frame(maxWidth: .infinity)
    }
}
