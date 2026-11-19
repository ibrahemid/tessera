import SwiftUI
import UniformTypeIdentifiers
import TesseraCore

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
    @State private var editAccount: Account?
    /// Density: roomy windowed layout (sidebar) vs a narrow menu-bar-friendly
    /// one (sidebar folded into a filter row). Persisted across launches.
    @AppStorage(AppModel.compactPrefKey) private var compact = false
    @State private var columnVisibility: NavigationSplitViewVisibility = .all
    @FocusState private var searchFocused: Bool
    @State private var didFocusSearch = false
    @State private var listSheetAccount: Account?   // account being assigned to a new list
    @State private var newListName = ""

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
            list = list.filter { "\($0.issuer) \($0.account) \($0.handle)".lowercased().contains(q) }
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
        .sheet(item: $editAccount) { EditAccountView(account: $0) }
        .fileDropImport(isEnabled: !showingAdd && qrAccount == nil && editAccount == nil && listSheetAccount == nil)
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
            if let n = model.notice, !showingAdd { noticeCapsule(n) }
        }
        .animation(reduceMotion ? nil : .spring(response: 0.35, dampingFraction: 0.8), value: model.notice)
    }

    private func noticeCapsule(_ n: AppModel.Notice) -> some View {
        let isFailure = if case .failure = n { true } else { false }
        return NoticeLine(n)
            .padding(.horizontal, 14).padding(.vertical, 9)
            .background(Palette.surfaceHi, in: Capsule())
            .overlay(Capsule().stroke(Palette.border, lineWidth: 1))
            .contentShape(Capsule())
            .onTapGesture { if isFailure { dismissNotice(n) } }
            .accessibilityElement(children: .combine)
            .accessibilityAddTraits(isFailure ? .isButton : [])
            .accessibilityHint(isFailure ? "Dismisses this message" : "")
            .accessibilityAction(named: "Dismiss") { dismissNotice(n) }
            .padding(.bottom, 16)
            .transition(.move(edge: .bottom).combined(with: .opacity))
            // Keyed on the token, not the text: repeating an identical notice
            // restarts the countdown instead of inheriting the previous one's.
            .task(id: model.noticeToken) {
                do { try await Task.sleep(for: .seconds(isFailure ? 6 : 2.6)) } catch { return }
                dismissNotice(n)
            }
    }

    private func dismissNotice(_ n: AppModel.Notice) {
        guard model.notice == n else { return }
        if reduceMotion { model.notice = nil } else { withAnimation { model.notice = nil } }
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
            // Land focus in search on first appearance so you can type-to-find
            // immediately. Later appearances (density toggle, window reopen)
            // must not steal focus back from wherever the user put it.
            guard !didFocusSearch else { return }
            didFocusSearch = true
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
                       openExisting: model.isExternalVault ? nil : {
                           if let e = model.openExistingVault() { model.notice = .failure(e) }
                       })
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
                    .foregroundStyle(isFolder(sel) ? Palette.accentInk : Palette.textSecondary)
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
        LiveAccountRow(ticker: model.ticker,
                       account: account,
                       copied: copiedID == account.id,
                       compact: compact,
                       reduceMotion: reduceMotion,
                       onCopy: { copy(account) },
                       onAdvance: { model.advanceHOTP(account); markCopied(account) })
            .contextMenu { accountMenu(account) }
            .swipeActions(edge: .leading, allowsFullSwipe: true) {
                Button { model.togglePin(account) } label: {
                    Label(account.pinned ? "Unpin" : "Pin", systemImage: account.pinned ? "pin.slash" : "pin")
                }.tint(Palette.accentFill)
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
        Button("Edit…", systemImage: "pencil") { editAccount = account }
        Divider()
        Menu {
            Button("Copy setup link", systemImage: "link") { model.copySetupLink(account) }
            Button("Copy setup key", systemImage: "key") { model.copySecretKey(account) }
            Button("Show QR code", systemImage: "qrcode") { qrAccount = account }
        } label: { Label("Setup secret", systemImage: "qrcode.viewfinder") }
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
        model.copyToPasteboard(model.code(for: account, at: Date()), transient: true)
        model.notice = .info("Copied \(account.displayName)")
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
