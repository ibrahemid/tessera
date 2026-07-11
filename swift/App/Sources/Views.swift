import SwiftUI
import AppKit
import UniformTypeIdentifiers
import TesseraCore

extension Account: @retroactive Identifiable {}

// MARK: Root

struct RootView: View {
    @EnvironmentObject var model: AppModel
    @AppStorage(AppModel.compactPrefKey) private var compact = false
    var body: some View {
        ZStack {
            Palette.background.ignoresSafeArea()
            Group {
                if model.needsReset {
                    ResetPromptView()
                } else if model.vaultUnreachable {
                    VaultUnreachableView()
                } else if model.isOpening {
                    LaunchView()
                } else if model.needsPassphrase {
                    PassphraseUnlockView()
                } else if model.isLocked {
                    UnlockView()
                } else {
                    VaultView()
                }
            }
        }
        .tint(Palette.accent)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(WindowConfigurator(compact: compact))
        .task { await model.openOnLaunch() }
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

// MARK: Launch / lock / reset

struct LaunchView: View {
    var body: some View {
        MosaicMark(side: 56)
            .frame(maxWidth: .infinity, maxHeight: .infinity)
    }
}

/// The one confirm dialog in front of the destructive reset, shared by every
/// surface that offers it.
extension View {
    func resetTesseraDialog(isPresented: Binding<Bool>, model: AppModel, message: String) -> some View {
        confirmationDialog("Reset Tessera?", isPresented: isPresented, titleVisibility: .visible) {
            Button("Delete vault and start over", role: .destructive) { model.resetTessera() }
            Button("Cancel", role: .cancel) {}
        } message: {
            Text(message)
        }
    }
}

struct UnlockView: View {
    @EnvironmentObject var model: AppModel
    @State private var confirmReset = false
    var body: some View {
        VStack(spacing: 16) {
            Spacer()
            MosaicMark(side: 50)
            Text("Tessera is locked").font(Typo.display(20)).foregroundStyle(Palette.textPrimary)
            Button { model.unlock() } label: {
                Label(model.requireBiometrics ? "Unlock with Touch ID" : "Unlock",
                      systemImage: model.requireBiometrics ? "touchid" : "lock.open")
                    .font(Typo.label(13, .semibold)).foregroundStyle(Palette.onAccent)
                    .frame(maxWidth: 260).padding(.vertical, 10)
                    .background(Palette.accent, in: RoundedRectangle(cornerRadius: 10))
            }.buttonStyle(.plain)
            if let e = model.errorMessage { ErrorLine(e) }
            Button("Can't unlock? Reset Tessera") { confirmReset = true }
                .buttonStyle(.plain).font(Typo.label(12)).foregroundStyle(Palette.textSecondary)
            Spacer()
        }
        .padding(32)
        .resetTesseraDialog(isPresented: $confirmReset, model: model,
                            message: "This deletes all accounts and the vault key on this Mac.")
    }
}

/// Unlock for a vault whose key isn't on this Mac but that carries a passphrase
/// wrap — typically one created with the tess CLI.
struct PassphraseUnlockView: View {
    @EnvironmentObject var model: AppModel
    @State private var passphrase = ""
    @State private var unlocking = false
    @State private var confirmReset = false
    var body: some View {
        VStack(spacing: 16) {
            Spacer()
            MosaicMark(side: 50)
            Text("This vault has a passphrase").font(Typo.display(20)).foregroundStyle(Palette.textPrimary)
            Text("It was created outside this app, for example with the tess CLI. After it opens once, Tessera unlocks it automatically.")
                .multilineTextAlignment(.center).font(Typo.label(13)).foregroundStyle(Palette.textSecondary)
                .frame(maxWidth: 360)
            FieldBox { SecureField("Vault passphrase", text: $passphrase).onSubmit(unlock) }
                .frame(maxWidth: 260)
            PrimaryButton(unlocking ? "Unlocking…" : "Unlock", enabled: !passphrase.isEmpty && !unlocking) { unlock() }
                .frame(maxWidth: 260)
            if let e = model.errorMessage { ErrorLine(e) }
            Button("Can't unlock? Reset Tessera") { confirmReset = true }
                .buttonStyle(.plain).font(Typo.label(12)).foregroundStyle(Palette.textSecondary)
            Spacer()
        }
        .padding(32)
        .resetTesseraDialog(isPresented: $confirmReset, model: model,
                            message: "This deletes the vault file and all accounts in it.")
    }

    private func unlock() {
        guard !passphrase.isEmpty, !unlocking else { return }
        unlocking = true
        Task {
            await model.unlockWithPassphrase(passphrase)
            unlocking = false
        }
    }
}

struct ResetPromptView: View {
    @EnvironmentObject var model: AppModel
    var body: some View {
        VStack(spacing: 16) {
            Spacer()
            MosaicMark(side: 50)
            Text("Can't open this vault").font(Typo.display(20)).foregroundStyle(Palette.textPrimary)
            Text("Its key isn't on this Mac, so the accounts can't be decrypted. Reset to start a new vault.")
                .multilineTextAlignment(.center).font(Typo.label(13)).foregroundStyle(Palette.textSecondary)
                .frame(maxWidth: 360)
            PrimaryButton("Reset Tessera") { model.resetTessera() }.frame(maxWidth: 260)
            if let e = model.errorMessage { ErrorLine(e) }
            Spacer()
        }
        .padding(32)
    }
}

/// The external vault (a file shared with the tess CLI) is configured but can't
/// be read this launch. Never offers to delete it — only to relocate or detach.
struct VaultUnreachableView: View {
    @EnvironmentObject var model: AppModel
    var body: some View {
        VStack(spacing: 16) {
            Spacer()
            MosaicMark(side: 50)
            Text("Can't reach your vault").font(Typo.display(20)).foregroundStyle(Palette.textPrimary)
            Text("The vault file you opened isn't available. It may have moved, or the disk isn't mounted.")
                .multilineTextAlignment(.center).font(Typo.label(13)).foregroundStyle(Palette.textSecondary)
                .frame(maxWidth: 360)
            PrimaryButton("Locate vault…") { model.openExistingVault() }.frame(maxWidth: 260)
            Button("Use built-in vault") { model.useBuiltInVault() }
                .buttonStyle(.plain).font(Typo.label(12)).foregroundStyle(Palette.textSecondary)
            if let e = model.errorMessage { ErrorLine(e) }
            Spacer()
        }
        .padding(32)
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
    @State private var qrAccount: Account?
    /// Density: roomy windowed layout (sidebar) vs a narrow menu-bar-friendly
    /// one (sidebar folded into a filter row). Persisted across launches.
    @AppStorage(AppModel.compactPrefKey) private var compact = false
    @State private var columnVisibility: NavigationSplitViewVisibility = .all
    @FocusState private var searchFocused: Bool
    @State private var listSheetAccount: Account?   // account being assigned to a new list
    @State private var newListName = ""
    @State private var mainDropTargeted = false

    private var folders: [String] { model.folders }

    /// Accounts shown in the user's saved (drag-reorderable) order.
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
        return list
    }

    /// Drag-to-reorder maps to the payload order, so only the unfiltered list.
    private var reorderable: Bool {
        (selection ?? .all) == .all && query.isEmpty
    }

    var body: some View {
        NavigationSplitView(columnVisibility: $columnVisibility) {
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
            detailScaffold
                .navigationTitle("")
                .toolbar {
                    ToolbarItem(placement: .primaryAction) {
                        Button { showingAdd = true } label: { Image(systemName: "plus") }
                            .help("Add account")
                            .keyboardShortcut("n", modifiers: .command)
                    }
                    ToolbarItem(placement: .automatic) {
                        Button { withAnimation(.easeInOut(duration: 0.25)) { compact.toggle() } } label: {
                            Image(systemName: compact ? "arrow.up.left.and.arrow.down.right" : "arrow.down.right.and.arrow.up.left")
                        }
                        .help(compact ? "Expand to the full window" : "Shrink to a compact window")
                        .accessibilityLabel(compact ? "Expand to full window" : "Shrink to compact window")
                    }
                    ToolbarItem(placement: .automatic) {
                        Button { openSettings() } label: { Image(systemName: "gearshape") }
                            .help("Settings")
                    }
                    ToolbarItem(placement: .automatic) {
                        Button { model.lock() } label: { Image(systemName: "lock") }
                            .help("Lock")
                    }
                }
        }
        // Sidebar is fixed open in window mode; folded entirely in compact. No
        // collapse toggle — the density button is the only control.
        .toolbar(removing: .sidebarToggle)
        .onAppear { columnVisibility = compact ? .detailOnly : .all }
        .onChange(of: compact) { _, c in
            withAnimation(.easeInOut(duration: 0.25)) {
                columnVisibility = c ? .detailOnly : .all
            }
        }
        // A list vanishes when its last account leaves it — don't strand the user
        // on a dead selection.
        .onChange(of: folders) { _, f in
            if case .folder(let name) = (selection ?? .all), !f.contains(name) { selection = .all }
        }
        .sheet(isPresented: $showingAdd) { AddAccountView() }
        .sheet(item: $qrAccount) { QRExportView(account: $0) }
        // Secondary drop target: dropping export files or QR images onto the main
        // window imports them; the summary shows in the status capsule.
        .onDrop(of: [.fileURL], isTargeted: $mainDropTargeted) { providers in
            loadDroppedURLs(providers) { urls in
                guard !urls.isEmpty else { return }
                _ = model.importDroppedFiles(urls)
                if let r = model.importReport, !r.isEmpty { model.status = r.line }
            }
            return true
        }
        .alert("New List", isPresented: Binding(
            get: { listSheetAccount != nil },
            set: { if !$0 { listSheetAccount = nil; newListName = "" } }
        )) {
            TextField("List name", text: $newListName)
            Button("Create") {
                if let a = listSheetAccount, !newListName.trimmingCharacters(in: .whitespaces).isEmpty {
                    model.setFolder(a, to: newListName)
                }
                listSheetAccount = nil; newListName = ""
            }
            Button("Cancel", role: .cancel) { listSheetAccount = nil; newListName = "" }
        } message: {
            Text("Group this account under a new list.")
        }
        .overlay(alignment: .bottom) {
            if let s = model.status, !showingAdd {
                StatusLine(s)
                    .padding(.horizontal, 14).padding(.vertical, 9)
                    .background(Palette.surfaceHi, in: Capsule())
                    .overlay(Capsule().stroke(Palette.border, lineWidth: 1))
                    .padding(.bottom, 16)
                    .transition(.move(edge: .bottom).combined(with: .opacity))
                    .task(id: s) {
                        try? await Task.sleep(for: .seconds(2.6))
                        if model.status == s { withAnimation { model.status = nil } }
                    }
            }
        }
        .animation(.spring(response: 0.35, dampingFraction: 0.8), value: model.status)
    }

    /// Detail = a fixed search header, an optional compact filter row (the
    /// sidebar's job when folded), then the account list.
    @ViewBuilder private var detailScaffold: some View {
        VStack(spacing: 0) {
            if !model.accounts.isEmpty { searchHeader }
            if compact && !model.accounts.isEmpty { compactFilterBar }
            detailContent
        }
        // One centered content column so the search field, filter row, and list
        // rows share the same left/right edges (was: full-width search over a
        // 680-capped list).
        .frame(maxWidth: compact ? .infinity : 680)
        .frame(maxWidth: .infinity)
        .background(
            // ⌘F focuses search (it used to live in the toolbar's .searchable).
            Button("") { searchFocused = true }
                .keyboardShortcut("f", modifiers: .command)
                .opacity(0).accessibilityHidden(true)
        )
        .onAppear {
            // Land focus in search on launch so you can type-to-find immediately.
            DispatchQueue.main.async { searchFocused = true }
        }
    }

    /// In-content search field — fixed position (never reflows like a toolbar
    /// item), focused on launch, Enter copies the top match.
    private var searchHeader: some View {
        HStack(spacing: 8) {
            Image(systemName: "magnifyingglass")
                .font(.system(size: 13)).foregroundStyle(Palette.textFaint)
            TextField("Search", text: $query)
                .textFieldStyle(.plain).font(Typo.label(13))
                .focused($searchFocused)
                .onSubmit { copyTopHit() }
                .onExitCommand { if query.isEmpty { searchFocused = false } else { query = "" } }
            if !query.isEmpty {
                Button { query = ""; searchFocused = true } label: {
                    Image(systemName: "xmark.circle.fill").font(.system(size: 13))
                        .foregroundStyle(Palette.textFaint)
                }
                .buttonStyle(.plain)
                .accessibilityLabel("Clear search")
            }
        }
        .padding(.horizontal, 11).padding(.vertical, 7)
        .background(Palette.surfaceHi, in: RoundedRectangle(cornerRadius: 9))
        .overlay(RoundedRectangle(cornerRadius: 9)
            .stroke(searchFocused ? Palette.accent : Palette.border, lineWidth: searchFocused ? 1.5 : 1))
        .padding(.horizontal, 14).padding(.top, 12).padding(.bottom, 4)
    }

    /// Enter in the search field copies the top visible code.
    private func copyTopHit() {
        guard let first = visible.first else { return }
        if first.type == .hotp { model.advanceHOTP(first) } else { copy(first) }
    }

    /// Distinguish "your search found nothing" from "this list is empty".
    @ViewBuilder private var emptyResults: some View {
        let searching = !query.isEmpty
        VStack(spacing: 8) {
            Image(systemName: searching ? "magnifyingglass" : "tray")
                .font(.system(size: 24)).foregroundStyle(Palette.textFaint)
            Text(searching ? "No matches" : emptyListText)
                .font(Typo.label(13)).foregroundStyle(Palette.textSecondary)
        }.frame(maxWidth: .infinity, maxHeight: .infinity)
    }

    private var emptyListText: String {
        switch selection ?? .all {
        case .pinned: return "No pinned accounts"
        case .folder(let f): return "No accounts in \(f)"
        case .all: return "No accounts"
        }
    }

    @ViewBuilder private var detailContent: some View {
        if model.accounts.isEmpty {
            EmptyState(add: { showingAdd = true },
                       openExisting: model.isExternalVault ? nil : { model.openExistingVault() })
        } else if visible.isEmpty {
            emptyResults
        } else {
            List {
                ForEach(visible, id: \.id) { account in
                    row(account)
                        .listRowSeparator(.hidden)
                        .listRowInsets(EdgeInsets(top: 3, leading: compact ? 6 : 8, bottom: 3, trailing: compact ? 6 : 8))
                        .listRowBackground(Color.clear)
                        .moveDisabled(!reorderable)   // no phantom drag in filtered/searched views
                }
                .onMove { from, to in if reorderable { model.move(fromOffsets: from, toOffset: to) } }
            }
            .listStyle(.plain)
            .scrollContentBackground(.hidden)
            .environment(\.defaultMinListRowHeight, 0)
            .frame(maxWidth: .infinity)   // fills the centered column from detailScaffold
        }
    }

    /// Shown only in compact mode: the All / Pinned pill plus a Folders menu —
    /// everything the sidebar offers, folded into one row.
    @ViewBuilder private var compactFilterBar: some View {
        let sel = selection ?? .all
        HStack(spacing: 8) {
            HStack(spacing: 2) {
                FilterPill(title: "All", selected: sel == .all) { selection = .all }
                FilterPill(title: "Pinned", selected: sel == .pinned) { selection = .pinned }
            }
            .padding(2)
            .background(Palette.surfaceHi, in: RoundedRectangle(cornerRadius: 9))
            .overlay(RoundedRectangle(cornerRadius: 9).stroke(Palette.border, lineWidth: 1))
            Spacer()
            if !folders.isEmpty {
                Menu {
                    ForEach(folders, id: \.self) { f in
                        Button(f) { selection = .folder(f) }
                    }
                } label: {
                    HStack(spacing: 6) {
                        Text(folderLabel(sel)).font(Typo.label(12.5, .medium))
                        Image(systemName: "chevron.down").font(.system(size: 9, weight: .semibold))
                    }
                    .foregroundStyle(isFolder(sel) ? Palette.accent : Palette.textSecondary)
                    .padding(.horizontal, 11).padding(.vertical, 6)
                    .background(Palette.surfaceHi, in: RoundedRectangle(cornerRadius: 9))
                    .overlay(RoundedRectangle(cornerRadius: 9).stroke(Palette.border, lineWidth: 1))
                }
                .menuStyle(.borderlessButton).menuIndicator(.hidden).fixedSize()
            }
        }
        .padding(.horizontal, 14).padding(.top, 10).padding(.bottom, 2)
    }

    private func isFolder(_ s: SidebarItem) -> Bool { if case .folder = s { return true }; return false }
    private func folderLabel(_ s: SidebarItem) -> String {
        if case .folder(let f) = s { return f }
        return "Folders"
    }

    @ViewBuilder private func row(_ account: Account) -> some View {
        AccountRowView(account: account,
                       remaining: model.remaining(for: account),
                       code: model.code(for: account),
                       copied: copiedID == account.id,
                       compact: compact,
                       reduceMotion: reduceMotion,
                       onCopy: { copy(account) },
                       onAdvance: { model.advanceHOTP(account); markCopied(account) })
            .contextMenu { accountMenu(account) }
            .swipeActions(edge: .leading, allowsFullSwipe: true) {
                Button { model.togglePin(account) } label: {
                    Label(account.pinned ? "Unpin" : "Pin", systemImage: account.pinned ? "pin.slash" : "pin")
                }.tint(Palette.accent)
            }
            .swipeActions(edge: .trailing, allowsFullSwipe: false) {
                Button(role: .destructive) { model.remove(account) } label: { Label("Delete", systemImage: "trash") }
                Button { qrAccount = account } label: { Label("QR", systemImage: "qrcode") }.tint(.gray)
            }
    }

    /// Full action set for an account — the right-click menu and the source of
    /// truth for what a row can do (pin, copy, QR, list membership, delete).
    @ViewBuilder private func accountMenu(_ account: Account) -> some View {
        Button(account.pinned ? "Unpin" : "Pin", systemImage: "pin") { model.togglePin(account) }
        if account.type == .hotp { Button("Advance code", systemImage: "forward") { model.advanceHOTP(account) } }
        Button("Copy code", systemImage: "doc.on.doc") { copy(account) }
        Button("Show QR code", systemImage: "qrcode") { qrAccount = account }
        Menu("Add to List") {
            ForEach(folders, id: \.self) { f in
                Button { model.setFolder(account, to: f) } label: {
                    if account.folder == f { Label(f, systemImage: "checkmark") } else { Text(f) }
                }
            }
            if !folders.isEmpty { Divider() }
            Button("New List…") { newListName = ""; listSheetAccount = account }
            if !account.folder.isEmpty {
                Divider()
                Button("Remove from \(account.folder)", role: .destructive) { model.setFolder(account, to: "") }
            }
        }
        Divider()
        Button("Delete", systemImage: "trash", role: .destructive) { model.remove(account) }
    }

    private func copy(_ account: Account) {
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(model.code(for: account), forType: .string)
        model.status = "Copied \(account.displayName)"
        markCopied(account)
    }

    /// Flash the row's code gold for ~1.3s. Shared by TOTP copy and HOTP advance
    /// so both give the same visual confirmation.
    private func markCopied(_ account: Account) {
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
    var compact: Bool = false
    var reduceMotion: Bool
    var onCopy: () -> Void
    var onAdvance: () -> Void
    @State private var hovering = false

    private var isHOTP: Bool { account.type == .hotp }
    private var fraction: Double {
        guard !isHOTP, account.period > 0 else { return 1 }
        return max(0, min(1, Double(remaining) / Double(account.period)))
    }
    private var low: Bool { !isHOTP && remaining <= 5 }
    private var glyphSize: CGFloat { compact ? 36 : 40 }
    private var ringSize: CGFloat { compact ? 26 : 30 }
    private var codeSize: CGFloat { compact ? 20 : 24 }
    private var primaryName: String { account.displayName }

    // Whole row is one button: click copies (or advances HOTP). Pin / QR / list /
    // delete live in the right-click menu and swipe actions on the row.
    var body: some View {
        Button(action: { isHOTP ? onAdvance() : onCopy() }) {
            HStack(spacing: compact ? 12 : 16) {
                GlyphSquare(account: account, size: glyphSize)
                VStack(alignment: .leading, spacing: 2) {
                    HStack(spacing: 5) {
                        Text(primaryName)
                            .font(Typo.label(compact ? 13.5 : 14.5, .semibold))
                            .foregroundStyle(Palette.textPrimary).lineLimit(1)
                        if account.pinned {
                            Image(systemName: "star.fill")
                                .font(.system(size: compact ? 8 : 9))
                                .foregroundStyle(Palette.accent)
                        }
                    }
                    if !account.issuer.isEmpty {
                        Text(account.account)
                            .font(Typo.label(compact ? 11 : 12))
                            .foregroundStyle(Palette.textSecondary).lineLimit(1)
                    }
                }
                Spacer(minLength: 8)
                Text(groupCode(code))
                    .font(Typo.code(codeSize)).monospacedDigit()
                    .foregroundStyle(copied ? Palette.accent : Palette.textPrimary)
                    .contentTransition(.numericText())
                trailing
            }
            .padding(.horizontal, compact ? 12 : 16)
            .padding(.vertical, compact ? 11 : 13)
            .contentShape(Rectangle())
            .background(
                RoundedRectangle(cornerRadius: 13, style: .continuous)
                    .fill(hovering ? Palette.surface : Color.clear)
            )
        }
        .buttonStyle(.plain)
        .onHover { hovering = $0 }
        // Spell the code out digit-by-digit so VoiceOver doesn't read "482913"
        // as a single large number.
        .accessibilityLabel("\(primaryName), code \(code.map(String.init).joined(separator: " "))")
        .accessibilityHint(isHOTP ? "Advances and copies" : "Copies code")
    }

    @ViewBuilder private var trailing: some View {
        if isHOTP {
            Image(systemName: "arrow.clockwise")
                .font(.system(size: compact ? 13 : 14, weight: .medium))
                .foregroundStyle(Palette.textSecondary)
                .frame(width: ringSize, height: ringSize)
        } else {
            CountdownRing(fraction: fraction, remaining: remaining, low: low,
                          size: ringSize, reduceMotion: reduceMotion)
        }
    }
}

// MARK: Add account

struct AddAccountView: View {
    @EnvironmentObject var model: AppModel
    @Environment(\.dismiss) var dismiss
    @State private var input = ""
    @State private var showManual = false
    @State private var manualSecret = ""
    @State private var scanning = false
    @State private var dropTargeted = false

    private var trimmed: String { input.trimmingCharacters(in: .whitespacesAndNewlines) }
    private var kind: InputDetect.InputKind { InputDetect.classify(input) }
    private var isSetupKey: Bool { kind == .setupKey }

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            HStack { Text("Add accounts").font(Typo.display(18)).foregroundStyle(Palette.textPrimary); Spacer() }
            if showManual {
                ManualEntryForm(prefillSecret: manualSecret) { dismiss() }
                Button("Back to paste") { showManual = false; model.errorMessage = nil }
                    .buttonStyle(.plain).font(Typo.label(12)).foregroundStyle(Palette.textSecondary)
            } else {
                primaryField
                secondaryActions
                resultArea
                footer
            }
        }
        .padding(20).frame(width: 440).background(Palette.background)
        .onDrop(of: [.fileURL], isTargeted: $dropTargeted) { handleDrop($0) }
        .overlay { if dropTargeted { dropOverlay } }
        .onAppear { model.errorMessage = nil; model.status = nil; model.importReport = nil }
    }

    @ViewBuilder private var primaryField: some View {
        Text("Paste an otpauth link, an app or Google Authenticator export, or a setup key. One per line.")
            .font(Typo.label(12)).foregroundStyle(Palette.textSecondary)
        FieldBox { TextField("otpauth://…", text: $input, axis: .vertical).lineLimit(3...8) }
        if let r = readout {
            Text(r).font(Typo.label(11, .medium)).foregroundStyle(Palette.textSecondary)
        }
    }

    @ViewBuilder private var secondaryActions: some View {
        HStack(spacing: 16) {
            Button {
                scanning = true
                Task { defer { scanning = false }
                    do {
                        let payloads = try await QRCapture.scanScreen()
                        finish(model.importReporting(payloads.joined(separator: "\n")))
                    } catch { model.errorMessage = "\(error)" } }
            } label: {
                Label(scanning ? "Scanning…" : "Scan screen", systemImage: "qrcode.viewfinder").font(Typo.label(12, .medium))
            }.buttonStyle(.plain).foregroundStyle(Palette.accent).disabled(scanning)
            Button { finish(model.importPickedFiles()) } label: {
                Label("Import from images or files", systemImage: "doc.text").font(Typo.label(12, .medium))
            }.buttonStyle(.plain).foregroundStyle(Palette.accent)
        }
    }

    @ViewBuilder private var resultArea: some View {
        if let report = model.importReport, !report.isEmpty {
            ImportReportView(report: report)
        } else if let e = model.errorMessage {
            ErrorLine(e)
        }
    }

    private var footer: some View {
        HStack {
            Spacer()
            Button("Cancel") { dismiss() }.buttonStyle(.plain).foregroundStyle(Palette.textSecondary)
                .keyboardShortcut(.cancelAction)
            ActionButton(isSetupKey ? "Continue" : "Add", enabled: !trimmed.isEmpty) { primaryAction() }
        }
    }

    private func primaryAction() {
        if isSetupKey {
            manualSecret = trimmed
            model.errorMessage = nil
            model.importReport = nil
            showManual = true
        } else {
            finish(model.importReporting(input))
        }
    }

    /// Dismiss on a clean success; stay so the per-item report stays visible when
    /// anything failed.
    private func finish(_ added: Bool) {
        if added, model.importReport?.hasDetail == false { dismiss() }
    }

    /// A factual one-line readout of what the field currently holds.
    private var readout: String? {
        if trimmed.isEmpty { return nil }
        switch kind {
        case .otpauth: return "otpauth link"
        case .migration:
            let n = (try? Migration.parse(input).count) ?? 0
            return n > 0 ? "Google Authenticator export (\(n) account\(n == 1 ? "" : "s"))"
                         : "Google Authenticator export"
        case .exportJSON:
            if let found = try? Importers.parse(Data(input.utf8)) {
                let n = found.accounts.count
                return "\(found.source) export (\(n) account\(n == 1 ? "" : "s"))"
            }
            return "App export"
        case .setupKey: return "Setup key"
        case .invalid:
            return (trimmed.count >= 4 && !input.contains(where: \.isNewline)) ? "Not recognized" : nil
        }
    }

    private func handleDrop(_ providers: [NSItemProvider]) -> Bool {
        loadDroppedURLs(providers) { urls in
            guard !urls.isEmpty else { return }
            _ = model.importDroppedFiles(urls)
        }
        return true
    }

    private var dropOverlay: some View {
        RoundedRectangle(cornerRadius: 12)
            .stroke(Palette.accent, style: StrokeStyle(lineWidth: 2, dash: [6]))
            .background(Palette.accentWash.opacity(0.4), in: RoundedRectangle(cornerRadius: 12))
            .overlay(Text("Drop images or export files").font(Typo.label(12, .medium)).foregroundStyle(Palette.accent))
            .allowsHitTesting(false)
    }
}

/// Per-item outcome of a bulk import: a summary line plus a scrollable list of
/// anything that did not import (source and reason, never a full secret).
struct ImportReportView: View {
    let report: ImportSummary
    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            if report.added > 0 { StatusLine(report.line) } else { ErrorLine(report.line) }
            if report.hasDetail {
                ScrollView {
                    VStack(alignment: .leading, spacing: 4) {
                        ForEach(report.failures) { f in
                            HStack(alignment: .firstTextBaseline, spacing: 6) {
                                Text(f.source).font(Typo.label(11, .medium)).foregroundStyle(Palette.textSecondary)
                                Text(f.display.isEmpty ? f.reason : "\(f.display): \(f.reason)")
                                    .font(Typo.label(11)).foregroundStyle(Palette.textFaint)
                                    .lineLimit(1).truncationMode(.middle)
                                Spacer(minLength: 0)
                            }
                        }
                    }.frame(maxWidth: .infinity, alignment: .leading)
                }.frame(maxHeight: 120)
            }
        }
        .padding(.horizontal, 11).padding(.vertical, 9)
        .background(Palette.surfaceHi, in: RoundedRectangle(cornerRadius: 9))
        .overlay(RoundedRectangle(cornerRadius: 9).stroke(Palette.border, lineWidth: 1))
    }
}

struct ManualEntryForm: View {
    @EnvironmentObject var model: AppModel
    var prefillSecret: String = ""
    var onAdded: () -> Void
    @State private var type: OTPType = .totp
    @State private var issuer = ""
    @State private var account = ""
    @State private var secret = ""
    @State private var algorithm = "SHA1"
    @State private var digits = 6
    @State private var period = 30
    @State private var counter: Int64 = 0

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            Picker("Type", selection: $type) {
                Text("Time-based (TOTP)").tag(OTPType.totp)
                Text("Counter (HOTP)").tag(OTPType.hotp)
            }
            FieldBox { TextField("Issuer (e.g. GitHub)", text: $issuer) }
            FieldBox { TextField("Account (e.g. you@example.com)", text: $account) }
            FieldBox { TextField("Setup key (base32 secret)", text: $secret) }
            HStack(spacing: 10) {
                Picker("Algorithm", selection: $algorithm) {
                    ForEach(["SHA1", "SHA256", "SHA512"], id: \.self) { Text($0).tag($0) }
                }.fixedSize()
                Stepper("Digits: \(digits)", value: $digits, in: 6...8)
                if type == .hotp {
                    Stepper("Counter: \(counter)", value: $counter, in: 0...Int64.max)
                } else {
                    Stepper("Period: \(period)s", value: $period, in: 15...120, step: 15)
                }
            }.font(Typo.label(12)).foregroundStyle(Palette.textSecondary)
            if let e = model.errorMessage { ErrorLine(e) }
            HStack {
                Spacer()
                ActionButton("Add", enabled: !secret.isEmpty && !(issuer.isEmpty && account.isEmpty)) {
                    model.addManual(type: type, issuer: issuer, account: account, secretBase32: secret,
                                    algorithm: algorithm, digits: digits, period: period, counter: counter)
                    if model.errorMessage == nil { onAdded() }
                }
            }
        }
        .onAppear { if secret.isEmpty { secret = prefillSecret } }
    }
}

/// Resolve dropped file URLs from item providers, then call `completion` on the
/// main actor once every provider has loaded.
func loadDroppedURLs(_ providers: [NSItemProvider], completion: @escaping @MainActor ([URL]) -> Void) {
    let group = DispatchGroup()
    let lock = NSLock()
    var urls: [URL] = []
    for p in providers where p.canLoadObject(ofClass: URL.self) {
        group.enter()
        _ = p.loadObject(ofClass: URL.self) { url, _ in
            if let url { lock.lock(); urls.append(url); lock.unlock() }
            group.leave()
        }
    }
    group.notify(queue: .main) {
        let resolved = urls
        Task { @MainActor in completion(resolved) }
    }
}

struct QRExportView: View {
    @EnvironmentObject var model: AppModel
    @Environment(\.dismiss) var dismiss
    let account: Account
    private enum Field { case secret, link }
    @State private var copied: Field?
    // The account is fixed for the sheet's lifetime, but body re-evaluates every
    // second with the ticking model — render the QR once, not per tick.
    @State private var qrImage: NSImage?
    @State private var saveError: String?

    private var name: String { account.displayName }

    var body: some View {
        VStack(spacing: 12) {
            VStack(spacing: 2) {
                Text(name).font(Typo.display(16)).foregroundStyle(Palette.textPrimary)
                Text("Move this account to another app").font(Typo.label(11)).foregroundStyle(Palette.textSecondary)
            }

            // Export copies the SECRET / setup link (for migration), not the
            // rotating code — copying the code here would be useless.
            copyButton("Copy secret key", field: .secret, value: model.secretBase32(for: account))
            copyButton("Copy setup link", field: .link, value: model.otpauthURI(for: account))

            if let img = qrImage {
                Image(nsImage: img).interpolation(.none).resizable()
                    .frame(width: 190, height: 190)
                    .background(.white, in: RoundedRectangle(cornerRadius: 8))
                Text("Or scan this QR with another authenticator. It contains the secret.")
                    .multilineTextAlignment(.center).font(Typo.label(11)).foregroundStyle(Palette.textSecondary)
                    .frame(maxWidth: 240)
                if let e = saveError { ErrorLine(e) }
                HStack {
                    Button("Save image…") { save(img) }.buttonStyle(.plain).foregroundStyle(Palette.accent)
                    Spacer()
                    Button("Done") { dismiss() }.buttonStyle(.plain).foregroundStyle(Palette.textSecondary)
                }.font(Typo.label(12, .medium)).frame(maxWidth: 240)
            } else {
                Button("Done") { dismiss() }.buttonStyle(.plain).foregroundStyle(Palette.textSecondary)
            }
        }
        .padding(24).frame(width: 300).background(Palette.background)
        .onAppear { qrImage = QRImage.generate(from: model.otpauthURI(for: account)) }
    }

    private func copyButton(_ title: String, field: Field, value: String) -> some View {
        Button { copy(value, field) } label: {
            HStack(spacing: 8) {
                Image(systemName: copied == field ? "checkmark" : "doc.on.doc")
                    .font(.system(size: 12, weight: .semibold)).foregroundStyle(Palette.accent)
                Text(copied == field ? "Copied" : title)
                    .font(Typo.label(12, .medium)).foregroundStyle(Palette.textPrimary)
                Spacer()
            }
            .padding(.horizontal, 12).padding(.vertical, 9).frame(maxWidth: 240)
            .background(Palette.surfaceHi, in: RoundedRectangle(cornerRadius: 9))
            .overlay(RoundedRectangle(cornerRadius: 9).stroke(Palette.border, lineWidth: 1))
        }
        .buttonStyle(.plain)
    }

    private func copy(_ value: String, _ field: Field) {
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(value, forType: .string)
        withAnimation { copied = field }
        Task { @MainActor in
            try? await Task.sleep(for: .seconds(1.3))
            withAnimation { if copied == field { copied = nil } }
        }
    }

    private func save(_ img: NSImage) {
        let panel = NSSavePanel()
        panel.nameFieldStringValue = "\(name).png"
        panel.allowedContentTypes = [.png]
        guard panel.runModal() == .OK, let url = panel.url else { return }
        do {
            try QRImage.writePNG(img, to: url)
            saveError = nil
        } catch {
            saveError = "Couldn't save the image: \(error.localizedDescription)"
        }
    }
}

// MARK: Settings

struct SettingsView: View {
    @EnvironmentObject var model: AppModel
    @AppStorage("tessera.theme") private var theme: AppTheme = .system
    @AppStorage("tessera.launchAtLogin") private var launchAtLogin = false
    @State private var exporting = false
    @State private var restoring = false
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
                }
            }
            Section("Backup") {
                Button("Export encrypted backup…") { exporting = true }
                    .disabled(model.isLocked || model.accounts.isEmpty)
                Button("Export as otpauth links…") { model.exportPlaintextLinks() }
                    .disabled(model.isLocked || model.accounts.isEmpty)
            }
            Section("Accounts") {
                Button("Remove duplicates") { dedupeResult = model.removeDuplicates() }
                    .disabled(model.isLocked || model.accounts.isEmpty)
                if let n = dedupeResult {
                    Text(n == 0 ? "No duplicates found" : "Removed \(n) duplicate\(n == 1 ? "" : "s")")
                        .font(Typo.label(11)).foregroundStyle(Palette.textSecondary)
                }
            }
            Section("Vault") {
                LabeledContent("Location", value: model.vaultPathDisplay)
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
        .resetTesseraDialog(isPresented: $confirmReset, model: model,
                            message: "This deletes all accounts and the vault key on this Mac. Export a backup first if you want to keep them.")
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

/// Native prominent action button — the default (Return) action in a dialog.
/// Replaces the custom fixed-size gold pill, which rendered cramped.
struct ActionButton: View {
    let title: String; var enabled: Bool; var action: () -> Void
    init(_ title: String, enabled: Bool = true, action: @escaping () -> Void) {
        self.title = title; self.enabled = enabled; self.action = action
    }
    var body: some View {
        Button(title, action: action)
            .buttonStyle(.borderedProminent)
            .tint(Palette.accent)
            .keyboardShortcut(.defaultAction)
            .disabled(!enabled)
    }
}

struct PrimaryButton: View {
    let title: String; var enabled: Bool; var action: () -> Void
    init(_ title: String, enabled: Bool = true, action: @escaping () -> Void) {
        self.title = title; self.enabled = enabled; self.action = action
    }
    var body: some View {
        Button(action: action) {
            Text(title).font(Typo.label(13, .semibold)).foregroundStyle(Palette.onAccent)
                .frame(maxWidth: .infinity).padding(.vertical, 9)
                .background(Palette.accent, in: RoundedRectangle(cornerRadius: 9))
        }
        .buttonStyle(.plain)
        .opacity(enabled ? 1 : 0.45)   // dim the whole control; never blend the fill toward the bg
        .disabled(!enabled)
    }
}

/// A Settings row action carrying a subtitle that spells out what it does — used
/// to separate switching the active vault from restoring a backup, two flows that
/// otherwise look identical.
struct VaultActionButton: View {
    let title: String
    let subtitle: String
    let action: () -> Void
    var body: some View {
        Button(action: action) {
            VStack(alignment: .leading, spacing: 2) {
                Text(title).font(Typo.label(13)).foregroundStyle(Palette.accent)
                Text(subtitle).font(Typo.label(11)).foregroundStyle(Palette.textSecondary)
                    .fixedSize(horizontal: false, vertical: true)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
    }
}

/// Segmented-style filter chip used in the compact filter row.
struct FilterPill: View {
    let title: String
    let selected: Bool
    let action: () -> Void
    var body: some View {
        Button(action: action) {
            Text(title)
                .font(Typo.label(12.5, .medium))
                .foregroundStyle(selected ? Palette.accent : Palette.textSecondary)
                .padding(.horizontal, 12).padding(.vertical, 5)
                .background(selected ? Palette.accentWash : Color.clear,
                            in: RoundedRectangle(cornerRadius: 7))
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
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

struct StatusLine: View {
    let text: String
    init(_ text: String) { self.text = text }
    var body: some View {
        Label(text, systemImage: "checkmark.circle.fill").font(Typo.label(11)).foregroundStyle(Palette.accent)
    }
}

struct EmptyState: View {
    var add: () -> Void
    var openExisting: (() -> Void)? = nil
    var body: some View {
        VStack(spacing: 12) {
            MosaicMark(side: 44)
            Text("No accounts yet").font(Typo.display(16)).foregroundStyle(Palette.textPrimary)
            Text("Add your first account from an otpauth link or a QR code on screen.")
                .multilineTextAlignment(.center).font(Typo.label(12)).foregroundStyle(Palette.textSecondary).frame(maxWidth: 320)
            Button(action: add) {
                Label("Add account", systemImage: "plus").font(Typo.label(12, .semibold)).foregroundStyle(Palette.accent)
            }.buttonStyle(.plain).padding(.top, 2)
            if let openExisting {
                Button(action: openExisting) {
                    Text("Use a vault created by the tess CLI").font(Typo.label(12)).foregroundStyle(Palette.textSecondary)
                }.buttonStyle(.plain)
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }
}

// MARK: Window chrome

/// Reaches the hosting NSWindow to give Tessera one seamless titlebar — the
/// content background runs continuously under the traffic lights instead of
/// leaving a detached strip — and narrows the window when Compact is on.
///
/// Xcode-gated: titlebar appearance and window resizing only render under a real
/// AppKit window, so this is verified by building the app, not in a preview.
struct WindowConfigurator: NSViewRepresentable {
    var compact: Bool

    // Tracks the last density we resized for. updateNSView runs ~once/second
    // (RootView observes the ticking model), so resizing must be gated on an
    // actual density change — otherwise it would fight the user's manual resize
    // every tick.
    final class Coordinator { var lastCompact: Bool?; var titlebarDone = false }
    func makeCoordinator() -> Coordinator { Coordinator() }

    func makeNSView(context: Context) -> NSView {
        // Seed with the default (window) density: a window-mode launch respects
        // the restored frame, while a compact-persisted launch resizes once to
        // the compact width instead of opening wide.
        context.coordinator.lastCompact = false
        let view = NSView()
        let coordinator = context.coordinator
        DispatchQueue.main.async { [weak view] in
            coordinator.titlebarDone = applyTitlebar(view?.window)
        }
        return view
    }

    func updateNSView(_ nsView: NSView, context: Context) {
        let changed = context.coordinator.lastCompact != compact
        context.coordinator.lastCompact = compact
        // Ticks are a no-op once the titlebar is styled and density is unchanged.
        guard changed || !context.coordinator.titlebarDone else { return }
        let coordinator = context.coordinator
        DispatchQueue.main.async { [weak nsView] in
            if !coordinator.titlebarDone {
                coordinator.titlebarDone = applyTitlebar(nsView?.window)
            }
            if changed { resize(nsView?.window) }
        }
    }

    @discardableResult
    private func applyTitlebar(_ window: NSWindow?) -> Bool {
        guard let window else { return false }
        window.titlebarAppearsTransparent = true
        window.titleVisibility = .hidden
        return true
    }

    /// Snap to the mode's nominal width on a density toggle. Manual sizing
    /// between toggles is left untouched.
    private func resize(_ window: NSWindow?) {
        guard let window else { return }
        let target: CGFloat = compact ? 480 : 920
        guard abs(window.frame.width - target) > 1 else { return }
        var frame = window.frame
        let delta = frame.width - target
        frame.origin.x += delta / 2          // keep the window visually centered
        frame.size.width = target
        window.setFrame(frame, display: true, animate: true)
    }
}
