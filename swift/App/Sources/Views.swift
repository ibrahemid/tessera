import SwiftUI
import TesseraCore

// MARK: Create vault

struct CreateVaultView: View {
    @EnvironmentObject var model: AppModel
    @State private var pass = ""
    @State private var confirm = ""

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("Welcome to Tessera").font(.title2.bold())
            Text("Create a passphrase to encrypt your vault. It's shared with the `tess` CLI.")
                .font(.callout).foregroundStyle(.secondary)
            SecureField("Passphrase", text: $pass)
            SecureField("Confirm passphrase", text: $confirm)
            if let e = model.errorMessage { Text(e).font(.caption).foregroundStyle(.red) }
            Button("Create Vault") {
                guard pass.count >= 8, pass == confirm else {
                    model.errorMessage = "Passphrases must match and be at least 8 characters"; return
                }
                model.createVault(passphrase: pass)
            }
            .buttonStyle(.borderedProminent)
            .disabled(pass.isEmpty)
            Spacer()
        }
    }
}

// MARK: Unlock

struct UnlockView: View {
    @EnvironmentObject var model: AppModel
    @State private var pass = ""

    var body: some View {
        VStack(spacing: 14) {
            Image(systemName: "lock.shield").font(.system(size: 40)).foregroundStyle(.tint)
            Text("Tessera is locked").font(.headline)
            SecureField("Passphrase", text: $pass)
                .onSubmit { model.unlock(passphrase: pass) }
            if let e = model.errorMessage { Text(e).font(.caption).foregroundStyle(.red) }
            Button("Unlock") { model.unlock(passphrase: pass) }
                .buttonStyle(.borderedProminent)
                .disabled(pass.isEmpty)
            if model.touchIDAvailableForUnlock {
                Button { model.unlockWithTouchID() } label: {
                    Label("Unlock with Touch ID", systemImage: "touchid")
                }
            }
            Spacer()
        }
    }
}

// MARK: Main menu

struct MenuView: View {
    @EnvironmentObject var model: AppModel
    @State private var query = ""
    @State private var showingAdd = false
    @State private var copied: String?

    private var filtered: [Account] {
        let base = model.accounts.sorted { ($0.pinned ? 0 : 1, $0.issuer.lowercased()) < ($1.pinned ? 0 : 1, $1.issuer.lowercased()) }
        guard !query.isEmpty else { return base }
        let q = query.lowercased()
        return base.filter { "\($0.issuer) \($0.account)".lowercased().contains(q) }
    }

    var body: some View {
        VStack(spacing: 8) {
            HStack {
                Image(systemName: "magnifyingglass").foregroundStyle(.secondary)
                TextField("Search", text: $query).textFieldStyle(.plain)
                Button { showingAdd = true } label: { Image(systemName: "plus") }
                    .buttonStyle(.borderless)
            }
            .padding(8).background(.quaternary, in: RoundedRectangle(cornerRadius: 8))

            if filtered.isEmpty {
                Spacer(); Text("No accounts").foregroundStyle(.secondary); Spacer()
            } else {
                ScrollView {
                    LazyVStack(spacing: 6) {
                        ForEach(filtered, id: \.id) { account in
                            AccountRow(account: account, copied: $copied)
                        }
                    }
                }
            }

            HStack {
                Button { model.lock() } label: { Label("Lock", systemImage: "lock") }
                Spacer()
                SettingsLink { Label("Settings", systemImage: "gearshape") }
            }
            .buttonStyle(.borderless).font(.callout)
        }
        .sheet(isPresented: $showingAdd) { AddAccountView() }
    }
}

struct AccountRow: View {
    @EnvironmentObject var model: AppModel
    let account: Account
    @Binding var copied: String?

    var body: some View {
        let code = model.code(for: account)
        HStack {
            VStack(alignment: .leading, spacing: 2) {
                Text(account.issuer.isEmpty ? account.account : account.issuer).font(.callout.bold())
                Text(account.account).font(.caption).foregroundStyle(.secondary)
            }
            Spacer()
            VStack(alignment: .trailing, spacing: 2) {
                Text(formatted(code)).font(.system(.title3, design: .monospaced))
                if account.type != .hotp {
                    Text("\(model.remaining(for: account))s").font(.caption2).foregroundStyle(.secondary)
                }
            }
            Button {
                NSPasteboard.general.clearContents()
                NSPasteboard.general.setString(code, forType: .string)
                copied = account.id
            } label: {
                Image(systemName: copied == account.id ? "checkmark" : "doc.on.doc")
            }
            .buttonStyle(.borderless)
        }
        .padding(10)
        .background(.quaternary.opacity(0.5), in: RoundedRectangle(cornerRadius: 10))
        .contextMenu {
            Button("Copy code") {
                NSPasteboard.general.clearContents()
                NSPasteboard.general.setString(code, forType: .string)
            }
            Button("Delete", role: .destructive) { model.remove(account) }
        }
    }

    private func formatted(_ code: String) -> String {
        guard code.count == 6 else { return code }
        let i = code.index(code.startIndex, offsetBy: 3)
        return code[..<i] + " " + code[i...]
    }
}

// MARK: Add account

struct AddAccountView: View {
    @EnvironmentObject var model: AppModel
    @Environment(\.dismiss) var dismiss
    @State private var uri = ""
    @State private var scanning = false

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("Add account").font(.headline)
            Text("Paste an otpauth:// or otpauth-migration:// link, or scan a QR code shown on screen.")
                .font(.caption).foregroundStyle(.secondary)
            TextField("otpauth://…", text: $uri, axis: .vertical).lineLimit(2...4)
            HStack {
                Button("Scan QR on screen") { scan() }.disabled(scanning)
                Spacer()
                Button("Cancel") { dismiss() }
                Button("Add") { add() }.buttonStyle(.borderedProminent).disabled(uri.isEmpty)
            }
            if let e = model.errorMessage { Text(e).font(.caption).foregroundStyle(.red) }
        }
        .padding(16).frame(width: 380)
    }

    private func add() {
        if uri.hasPrefix("otpauth-migration://") { model.importMigration(uri) }
        else { model.importOTPAuth(uri) }
        if model.errorMessage == nil { dismiss() }
    }

    private func scan() {
        scanning = true
        Task {
            defer { scanning = false }
            do { uri = try await QRCapture.scanScreen() } catch { model.errorMessage = "\(error)" }
        }
    }
}

// MARK: Settings

struct SettingsView: View {
    @EnvironmentObject var model: AppModel
    @AppStorage("tessera.theme") private var theme: AppTheme = .system
    @AppStorage("tessera.launchAtLogin") private var launchAtLogin = false

    var body: some View {
        Form {
            Picker("Appearance", selection: $theme) {
                ForEach(AppTheme.allCases) { Text($0.label).tag($0) }
            }.pickerStyle(.segmented)

            Toggle("Launch at login", isOn: $launchAtLogin)
                .onChange(of: launchAtLogin) { _, on in LoginItem.set(enabled: on) }

            if SecureEnclaveWrap.isAvailable {
                Toggle("Unlock with Touch ID", isOn: Binding(
                    get: { model.hasTouchIDWrap },
                    set: { $0 ? model.enableTouchID() : model.disableTouchID() }
                ))
                .disabled(model.isLocked)
            }

            if let e = model.errorMessage { Text(e).font(.caption).foregroundStyle(.red) }
        }
        .formStyle(.grouped)
        .frame(width: 360)
        .padding()
    }
}
