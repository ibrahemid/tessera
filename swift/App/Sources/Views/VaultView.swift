import SwiftUI
import UniformTypeIdentifiers
import TesseraCore

// MARK: Vault (windowed)

private enum TextPrompt: Identifiable, Equatable {
    case newFolder(Set<String>)
    case renameFolder(String)
    case newTag(Set<String>)

    var id: String {
        switch self {
        case .newFolder: return "newFolder"
        case .renameFolder(let f): return "renameFolder:\(f)"
        case .newTag: return "newTag"
        }
    }

    var title: String {
        switch self {
        case .newFolder: return "New Folder"
        case .renameFolder: return "Rename Folder"
        case .newTag: return "New Tag"
        }
    }

    var field: String {
        if case .newTag = self { return "Tag" }
        return "Folder name"
    }

    var confirm: String {
        switch self {
        case .newFolder: return "Create"
        case .renameFolder: return "Rename"
        case .newTag: return "Add"
        }
    }
}

#if DEBUG
struct VaultPreset {
    var scope: VaultScope = .all
    var query = ""
    var selected: [String] = []
    var hoverID: String?
    var hideCodes: Bool?
    var copiedID: String?
    var copiedNext = false
}
#endif

struct VaultView: View {
    @EnvironmentObject var model: AppModel
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.openSettings) private var openSettings
    @State private var scope: VaultScope? = .all
    @State private var query = ""
    @State private var selection = VaultSelection()
    @State private var showingAdd = false
    @State private var copiedID: String?
    @State private var copiedNextUntil: Date?
    @State private var qrAccount: Account?
    @State private var editAccount: Account?
    @State private var exportingBackup = false
    @State private var restoringBackup = false
    @State private var prompt: TextPrompt?
    @State private var pickedImport: [URL] = []
    @State private var promptText = ""
    @State private var scanning = false
    /// Density: roomy windowed layout (sidebar) vs a narrow menu-bar-friendly
    /// one (sidebar folded into a filter row). Persisted across launches.
    @AppStorage(AppModel.compactPrefKey) private var compact = false
    @AppStorage(AppModel.hideCodesPrefKey) private var hideCodes = false
    @State private var columnVisibility: NavigationSplitViewVisibility = .all
    @FocusState private var searchFocused: Bool
    @State private var didFocusSearch = false
    private let previewHoverID: String?
    private let hideCodesOverride: Bool?

    init() {
        previewHoverID = nil
        hideCodesOverride = nil
    }

    #if DEBUG
    init(preset: VaultPreset) {
        var picked = VaultSelection()
        for id in preset.selected { picked.toggle(id, in: preset.selected) }
        _scope = State(initialValue: preset.scope)
        _query = State(initialValue: preset.query)
        _selection = State(initialValue: picked)
        _copiedID = State(initialValue: preset.copiedID)
        _copiedNextUntil = State(initialValue: preset.copiedNext ? .distantFuture : nil)
        previewHoverID = preset.hoverID
        hideCodesOverride = preset.hideCodes
    }
    #endif

    private var folders: [String] { model.folders }
    private var currentScope: VaultScope { scope ?? .all }
    private var hidesCodes: Bool { hideCodesOverride ?? hideCodes }
    private var sections: [VaultSection] { VaultList.sections(model.accounts, scope: currentScope, query: query) }
    private var visible: [Account] { sections.flatMap(\.accounts) }
    private var visibleIDs: [String] { visible.map(\.id) }
    private var reorderable: Bool { VaultList.isReorderable(scope: currentScope, query: query) }
    private var showsFilterBar: Bool { compact || columnVisibility == .detailOnly }

    private var selectedAccounts: [Account] {
        let byID = Dictionary(model.accounts.map { ($0.id, $0) }, uniquingKeysWith: { a, _ in a })
        return selection.ordered(in: visibleIDs).compactMap { byID[$0] }
    }

    private var isPresenting: Bool {
        showingAdd || qrAccount != nil || editAccount != nil || exportingBackup || restoringBackup || prompt != nil
    }

    private var menuContext: VaultMenuContext {
        VaultMenuContext(selected: selectedAccounts, folders: folders, tags: model.allTags) { action in
            perform(action, targets: selectedAccounts)
        }
    }

    var body: some View {
        NavigationSplitView(columnVisibility: $columnVisibility) {
            sidebar
        } detail: {
            detailScaffold
                .navigationTitle("")
                .toolbar {
                    ToolbarItem(placement: .primaryAction) {
                        Button { showingAdd = true } label: { Image(systemName: "plus") }
                            .help("Add account")
                    }
                    ToolbarItem(placement: .automatic) {
                        Button { setCompact(!compact) } label: {
                            Image(systemName: compact ? "arrow.up.left.and.arrow.down.right" : "arrow.down.right.and.arrow.up.left")
                        }
                        .help(compact ? "Expand to the full window" : "Shrink to a compact window")
                        .accessibilityLabel(compact ? "Expand to full window" : "Shrink to compact window")
                    }
                    ToolbarItem(placement: .automatic) {
                        Button { openSettings() } label: { Image(systemName: "gearshape") }
                            .help("Settings")
                            .accessibilityLabel("Settings")
                    }
                    ToolbarItem(placement: .automatic) {
                        Button { model.lock() } label: { Image(systemName: "lock") }
                            .help("Lock")
                            .accessibilityLabel("Lock")
                    }
                }
        }
        // Sidebar is fixed open in window mode; folded entirely in compact. No
        // collapse toggle — the density button is the only control.
        .toolbar(removing: .sidebarToggle)
        .onAppear { columnVisibility = compact ? .detailOnly : .all }
        .onChange(of: compact) { _, c in
            withAnimation(reduceMotion ? nil : .easeInOut(duration: 0.25)) {
                columnVisibility = c ? .detailOnly : .all
            }
        }
        // A list vanishes when its last account leaves it — don't strand the user
        // on a dead selection.
        .onChange(of: folders) { _, f in
            if case .folder(let name) = currentScope, !f.contains(name) { scope = .all }
        }
        .onChange(of: visibleIDs) { _, ids in selection.prune(to: ids) }
        .onChange(of: query) { _, q in
            guard !q.trimmingCharacters(in: .whitespaces).isEmpty else { return }
            if let first = visibleIDs.first { selection.select(first) } else { selection.clear() }
        }
        // Arrows are routed here: a key handler inside the split view's detail column never receives them.
        .onKeyPress(phases: .down) { press in
            guard !isPresenting,
                  let route = VaultKeyRoute.route(key: press.key, characters: press.characters,
                                                  modifiers: press.modifiers, searchFocused: searchFocused)
            else { return .ignored }
            switch route {
            case .move(let delta, let extending):
                moveSelection(delta, extending: extending)
            case .type(let text):
                query.append(text)
                searchFocused = true
            }
            return .handled
        }
        .sheet(isPresented: $showingAdd) { AddAccountView() }
        .sheet(item: $qrAccount) { QRExportView(account: $0) }
        .sheet(item: $editAccount) { EditAccountView(account: $0) }
        .fileDropImport(isEnabled: !showingAdd && qrAccount == nil && editAccount == nil && prompt == nil,
                        picked: $pickedImport)
        .alert(prompt?.title ?? "", isPresented: Binding(
            get: { prompt != nil },
            set: { if !$0 { prompt = nil; promptText = "" } }
        ), presenting: prompt) { p in
            TextField(p.field, text: $promptText)
            Button(p.confirm) { submit(p) }
            Button("Cancel", role: .cancel) { prompt = nil; promptText = "" }
        }
        .sheet(isPresented: $exportingBackup) { ExportBackupView() }
        .sheet(isPresented: $restoringBackup) { RestoreBackupView() }
        .overlay(alignment: .bottom) {
            if let n = model.notice, !showingAdd { noticeCapsule(n) }
        }
        .animation(reduceMotion ? nil : .spring(response: 0.35, dampingFraction: 0.8), value: model.notice)
        .focusedSceneValue(\.vaultMenu, isPresenting ? nil : menuContext)
    }

    private var sidebar: some View {
        let counts = VaultList.counts(model.accounts)
        return List(selection: $scope) {
            Section {
                Label("All", systemImage: "square.grid.2x2").badge(counts.all).tag(VaultScope.all)
                Label("Pinned", systemImage: "star").badge(counts.pinned).tag(VaultScope.pinned)
            }
            if !folders.isEmpty {
                Section("Folders") {
                    ForEach(folders, id: \.self) { f in
                        Label(f, systemImage: "folder")
                            .badge(counts.folders[f] ?? 0)
                            .tag(VaultScope.folder(f))
                            .contextMenu {
                                Button("Rename Folder…") { promptText = f; prompt = .renameFolder(f) }
                                Button("Remove Folder") { model.removeFolder(f) }
                            }
                            .dropDestination(for: String.self) { items, _ in
                                let ids = VaultList.droppedIDs(items, known: Set(model.accounts.map(\.id)))
                                guard !ids.isEmpty else { return false }
                                model.setFolder(ids: ids, to: f)
                                return true
                            }
                    }
                }
            }
        }
        .listItemTint(.fixed(Palette.accentInk))
        .navigationSplitViewColumnWidth(min: 180, ideal: 210, max: 280)
        .safeAreaInset(edge: .bottom) {
            HStack {
                Wordmark(size: 13)
                Spacer()
            }.padding(10)
        }
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
            .padding(.bottom, selection.isMultiple ? 68 : 16)
            .transition(reduceMotion ? .opacity : .move(edge: .bottom).combined(with: .opacity))
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

    /// Detail = a fixed search header, an optional filter row (the sidebar's job
    /// when folded), then the account list.
    @ViewBuilder private var detailScaffold: some View {
        VStack(spacing: 0) {
            if !model.accounts.isEmpty { searchHeader }
            if showsFilterBar && !model.accounts.isEmpty { compactFilterBar }
            detailContent
        }
        // One centered content column so the search field, filter row, and list
        // rows share the same left/right edges (was: full-width search over a
        // 680-capped list).
        .frame(maxWidth: compact ? .infinity : 680)
        .frame(maxWidth: .infinity)
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
    /// item), focused on launch. Arrows move the selection, Return acts on it.
    private var searchHeader: some View {
        HStack(spacing: 8) {
            Image(systemName: "magnifyingglass")
                .font(.system(size: 13)).foregroundStyle(Palette.textFaint)
                .accessibilityHidden(true)
            TextField("Search", text: $query)
                .textFieldStyle(.plain).font(Typo.label(13))
                .focused($searchFocused)
                .accessibilityIdentifier("VaultSearch")
                .onSubmit { activateCursor() }
                .onExitCommand { escape() }
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
        switch currentScope {
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
            ScrollViewReader { proxy in
                List {
                    ForEach(sections) { section in
                        if section.kind == .single {
                            sectionRows(section)
                        } else {
                            Section {
                                sectionRows(section)
                            } header: {
                                SectionLabel(section.kind == .pinned ? "Pinned" : "Accounts")
                                    .padding(.horizontal, compact ? 6 : 8)
                            }
                        }
                    }
                }
                .listStyle(.plain)
                .scrollContentBackground(.hidden)
                .environment(\.defaultMinListRowHeight, 0)
                .frame(maxWidth: .infinity)   // fills the centered column from detailScaffold
                .onChange(of: selection.cursor) { _, cursor in
                    guard let cursor else { return }
                    withAnimation(reduceMotion ? nil : .easeOut(duration: 0.15)) { proxy.scrollTo(cursor) }
                }
                .safeAreaInset(edge: .bottom) {
                    if selection.isMultiple { bulkBar }
                }
            }
        }
    }

    private func sectionRows(_ section: VaultSection) -> some View {
        let ids = section.accounts.map(\.id)
        return ForEach(section.accounts, id: \.id) { account in
            row(account)
                .listRowSeparator(.hidden)
                .listRowInsets(EdgeInsets(top: 3, leading: compact ? 6 : 8, bottom: 3, trailing: compact ? 6 : 8))
                .listRowBackground(Color.clear)
                .moveDisabled(!reorderable)   // no phantom drag in filtered/searched views
        }
        .onMove(perform: reorderable ? { from, to in model.move(inSection: ids, fromOffsets: from, toOffset: to) } : nil)
    }

    /// Shown in compact mode, or when the sidebar is collapsed: the All / Pinned
    /// pill plus a Folders menu: everything the sidebar offers, in one row.
    @ViewBuilder private var compactFilterBar: some View {
        let sel = currentScope
        HStack(spacing: 8) {
            HStack(spacing: 2) {
                FilterPill(title: "All", selected: sel == .all) { scope = .all }
                FilterPill(title: "Pinned", selected: sel == .pinned) { scope = .pinned }
            }
            .padding(2)
            .background(Palette.surfaceHi, in: RoundedRectangle(cornerRadius: 9))
            .overlay(RoundedRectangle(cornerRadius: 9).stroke(Palette.border, lineWidth: 1))
            Spacer()
            if !folders.isEmpty {
                Menu {
                    ForEach(folders, id: \.self) { f in
                        Button(f) { scope = .folder(f) }
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

    private func isFolder(_ s: VaultScope) -> Bool { if case .folder = s { return true }; return false }
    private func folderLabel(_ s: VaultScope) -> String {
        if case .folder(let f) = s { return f }
        return "Folders"
    }

    private var bulkBar: some View {
        let targets = selectedAccounts
        let state = AccountMenuState(targets: targets)
        let tags = model.allTags
        return HStack(spacing: 14) {
            Text("\(selection.count) selected")
                .font(Typo.label(12.5, .semibold))
                .foregroundStyle(Palette.textPrimary)
                .lineLimit(1)
                .fixedSize()
            Spacer(minLength: 4)
            Button { perform(.togglePin, targets: targets) } label: {
                Label(state.pins ? "Pin" : "Unpin", systemImage: state.pins ? "pin" : "pin.slash")
            }
            .buttonStyle(.plain)
            Menu {
                ForEach(folders, id: \.self) { f in
                    Button { perform(.moveToFolder(f), targets: targets) } label: {
                        if state.sharedFolder == f { Label(f, systemImage: "checkmark") } else { Text(f) }
                    }
                }
                if !folders.isEmpty { Divider() }
                Button("New Folder…") { perform(.newFolder, targets: targets) }
                if state.hasFolder {
                    Divider()
                    Button(state.removeFromFolderTitle) { perform(.removeFromFolder, targets: targets) }
                }
            } label: {
                Label("Move to Folder", systemImage: "folder")
            }
            .menuStyle(.borderlessButton).menuIndicator(.hidden).fixedSize()
            Menu {
                ForEach(tags, id: \.self) { t in
                    Button { perform(.addTag(t), targets: targets) } label: {
                        if state.hasTagOnAll(t) { Label("#\(t)", systemImage: "checkmark") } else { Text("#\(t)") }
                    }
                }
                if !tags.isEmpty { Divider() }
                Button("New Tag…") { perform(.newTag, targets: targets) }
                if !state.removableTags.isEmpty {
                    Divider()
                    Menu("Remove Tag") {
                        ForEach(state.removableTags, id: \.self) { t in
                            Button("#\(t)") { perform(.removeTag(t), targets: targets) }
                        }
                    }
                }
            } label: {
                Label("Tags", systemImage: "tag")
            }
            .menuStyle(.borderlessButton).menuIndicator(.hidden).fixedSize()
            Button { selection.clear() } label: {
                Image(systemName: "xmark").font(.system(size: 11, weight: .semibold))
            }
            .buttonStyle(.plain)
            .foregroundStyle(Palette.textSecondary)
            .help("Clear selection")
            .accessibilityLabel("Clear selection")
        }
        .labelStyle(BulkLabelStyle(iconOnly: compact))
        .font(Typo.label(12.5, .medium))
        .foregroundStyle(Palette.accentInk)
        .padding(.horizontal, 14).padding(.vertical, 9)
        .background(Palette.surfaceHi, in: RoundedRectangle(cornerRadius: 12, style: .continuous))
        .overlay(RoundedRectangle(cornerRadius: 12, style: .continuous).stroke(Palette.border, lineWidth: 1))
        .padding(.horizontal, compact ? 10 : 14).padding(.top, 6).padding(.bottom, 12)
        .background(Palette.background)
    }

    @ViewBuilder private func row(_ account: Account) -> some View {
        let menuTargets = targets(for: account)
        LiveAccountRow(ticker: model.ticker,
                       account: account,
                       copied: copiedID == account.id,
                       copiedNextUntil: copiedID == account.id ? copiedNextUntil : nil,
                       selected: selection.contains(account.id),
                       hideCodes: hidesCodes,
                       compact: compact,
                       reduceMotion: reduceMotion,
                       hovering: previewHoverID == account.id,
                       onClick: { click(account, $0) })
            .contextMenu {
                AccountActionsMenu(state: AccountMenuState(targets: menuTargets),
                                   folders: folders, tags: model.allTags) { perform($0, targets: menuTargets) }
            }
            .itemProvider {
                NSItemProvider(object: VaultList.dragPayload(for: account.id, selection: selection,
                                                             order: visibleIDs) as NSString)
            }
            .swipeActions(edge: .leading, allowsFullSwipe: true) {
                Button { model.togglePin(account) } label: {
                    Label(account.pinned ? "Unpin" : "Pin", systemImage: account.pinned ? "pin.slash" : "pin")
                }.tint(Palette.accentFill)
            }
            .swipeActions(edge: .trailing, allowsFullSwipe: false) {
                Button(role: .destructive) { model.remove(account) } label: { Label("Delete", systemImage: "trash") }
                Button { qrAccount = account } label: { Label("QR", systemImage: "qrcode") }.tint(.gray)
            }
            .accessibilityAction(named: account.pinned ? "Unpin" : "Pin") { model.togglePin(account) }
            .accessibilityAction(named: "Edit") { editAccount = account }
            .accessibilityAction(named: "Show QR Code") { qrAccount = account }
            .accessibilityAction(named: "Delete") { model.remove(account) }
    }

    private func targets(for account: Account) -> [Account] {
        let byID = Dictionary(model.accounts.map { ($0.id, $0) }, uniquingKeysWith: { a, _ in a })
        return selection.targets(clicked: account.id, in: visibleIDs).compactMap { byID[$0] }
    }

    private func setCompact(_ value: Bool) {
        if reduceMotion { compact = value } else { withAnimation(.easeInOut(duration: 0.25)) { compact = value } }
    }

    private func click(_ account: Account, _ kind: RowClick) {
        selection.click(account.id, kind, in: visibleIDs)
        if kind == .activate { activate(account) }
        searchFocused = true
    }

    private func moveSelection(_ delta: Int, extending: Bool) {
        selection.move(by: delta, in: visibleIDs, extending: extending)
    }

    private func escape() {
        if !query.isEmpty { query = "" } else if !selection.isEmpty { selection.clear() }
    }

    private func activateCursor() {
        guard let id = selection.activationTarget(in: visibleIDs),
              let account = visible.first(where: { $0.id == id }) else { return }
        if !selection.contains(id) { selection.select(id) }
        activate(account)
    }

    private func activate(_ account: Account) {
        if account.type == .hotp {
            model.advanceHOTP(account)
            markCopied(account, nextUntil: nil)
        } else {
            copy(account)
        }
    }

    private func copy(_ account: Account) {
        let now = model.ticker.refresh()
        let nextUntil = CodeTiming.shouldCopyNext(account, at: now) ? CodeTiming.windowEnd(account, at: now) : nil
        guard model.copyCode(account, at: now) != nil else { return }
        markCopied(account, nextUntil: nextUntil)
    }

    private func perform(_ action: VaultAction, targets: [Account]) {
        let ids = Set(targets.map(\.id))
        let single = targets.count == 1 ? targets.first : nil
        switch action {
        case .add: showingAdd = true
        case .scan: scanScreen()
        case .importFiles: pickedImport = model.chooseImportFiles()
        case .exportBackup: exportingBackup = true
        case .restoreBackup: restoringBackup = true
        case .find: searchFocused = true
        case .copy: if let a = single { copy(a) }
        case .advance: if let a = single, a.type == .hotp { activate(a) }
        case .edit: if let a = single { editAccount = a }
        case .togglePin: model.setPinned(AccountMenuState(targets: targets).pins, ids: ids)
        case .showQR: if let a = single { qrAccount = a }
        case .copySetupLink: if let a = single { model.copySetupLink(a) }
        case .copySetupKey: if let a = single { model.copySecretKey(a) }
        case .delete: if let a = single { model.remove(a) }
        case .moveToFolder(let f): model.setFolder(ids: ids, to: f)
        case .newFolder: promptText = ""; prompt = .newFolder(ids)
        case .removeFromFolder: model.setFolder(ids: ids, to: "")
        case .addTag(let t): model.addTag(t, ids: ids)
        case .newTag: promptText = ""; prompt = .newTag(ids)
        case .removeTag(let t): model.removeTag(t, ids: ids)
        }
    }

    private func submit(_ p: TextPrompt) {
        let text = promptText.trimmingCharacters(in: .whitespaces)
        prompt = nil
        promptText = ""
        guard !text.isEmpty else { return }
        switch p {
        case .newFolder(let ids): model.setFolder(ids: ids, to: text)
        case .renameFolder(let old):
            model.renameFolder(old, to: text)
            if currentScope == .folder(old), model.folders.contains(text) { scope = .folder(text) }
        case .newTag(let ids): model.addTag(text, ids: ids)
        }
    }

    private func scanScreen() {
        guard !scanning else { return }
        scanning = true
        Task { @MainActor in
            defer { scanning = false }
            do {
                let payloads = try await QRCapture.scanScreen()
                _ = model.importReporting(payloads.joined(separator: "\n"))
                surfaceImportResult()
            } catch {
                if let text = UserMessage.textUnlessCancelled(for: error, context: .scan) {
                    model.notice = .failure(text)
                }
            }
        }
    }

    private func surfaceImportResult() {
        if let e = model.errorMessage {
            model.errorMessage = nil
            model.notice = .failure(e)
        } else if let r = model.importReport, !r.isEmpty {
            model.notice = .info(r.line)
        }
    }

    /// Flash the copied code gold for ~1.3s. Shared by TOTP copy and HOTP advance
    /// so both give the same visual confirmation.
    private func markCopied(_ account: Account, nextUntil: Date?) {
        withAnimation(reduceMotion ? nil : .spring(response: 0.3, dampingFraction: 0.7)) {
            copiedID = account.id
            copiedNextUntil = nextUntil
        }
        let id = account.id
        Task { @MainActor in
            try? await Task.sleep(for: .seconds(1.3))
            guard copiedID == id else { return }
            withAnimation(reduceMotion ? nil : .default) {
                copiedID = nil
                copiedNextUntil = nil
            }
        }
    }
}

private struct BulkLabelStyle: LabelStyle {
    let iconOnly: Bool

    func makeBody(configuration: Configuration) -> some View {
        if iconOnly {
            configuration.icon
        } else {
            HStack(spacing: 5) {
                configuration.icon
                configuration.title
            }
        }
    }
}
