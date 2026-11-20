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
    @State private var linkPrefill: LinkPrefill?
    @State private var folderRemoval: FolderRemoval?
    @State private var noticeHeight: CGFloat = 0
    @ScaledMetric(relativeTo: .body) private var emptyIconSize: CGFloat = 24
    /// Density: roomy windowed layout (sidebar) vs a narrow menu-bar-friendly
    /// one (sidebar folded into a filter row). Persisted across launches.
    @AppStorage(AppModel.compactPrefKey) private var compact = false
    @AppStorage(AppModel.hideCodesPrefKey) private var hideCodes = false
    @State private var columnVisibility: NavigationSplitViewVisibility = .all
    @State private var hidesSidebarToggle = false
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
            || linkPrefill != nil || folderRemoval != nil
    }

    private var menuContext: VaultMenuContext {
        VaultMenuContext(selected: selectedAccounts, folders: folders, tags: model.allTags) { action in
            perform(action, targets: selectedAccounts)
        }
    }

    var body: some View {
        backupSheets
            .focusedSceneValue(\.vaultMenu, isPresenting ? nil : menuContext)
    }

    private var navigation: some View {
        NavigationSplitView(columnVisibility: $columnVisibility) {
            sidebar
        } detail: {
            detailScaffold
                .navigationTitle("")
                .toolbar { detailToolbar }
        }
        // Sidebar is fixed open in window mode; folded entirely in compact. No
        // collapse toggle — the density button is the only control.
        .toolbar(removing: .sidebarToggle)
    }

    @ToolbarContentBuilder private var detailToolbar: some ToolbarContent {
        ToolbarItem(placement: .primaryAction) {
            Button { showingAdd = true } label: { Image(systemName: "plus") }
                .help("Add account")
                .accessibilityLabel("Add account")
        }
        ToolbarItem(placement: .automatic) {
            Button { setCompact(!compact) } label: { Image(systemName: densityToggleSymbol) }
                .help(densityToggleTitle)
                .accessibilityLabel(densityToggleTitle)
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

    private var layoutObservers: some View {
        navigation
            .onAppear {
                columnVisibility = compact ? .detailOnly : .all
                hidesSidebarToggle = compact
            }
            .onChange(of: compact) { (_: Bool, isCompact: Bool) in updateColumns(compact: isCompact) }
    }

    private var dataObservers: some View {
        layoutObservers
            // A list vanishes when its last account leaves it — don't strand the user
            // on a dead selection.
            .onChange(of: folders) { (_: [String], f: [String]) in leaveScopeIfGone(folders: f) }
            .onChange(of: model.allTags) { (_: [String], t: [String]) in leaveScopeIfGone(tags: t) }
            .onChange(of: model.pendingLink, initial: true) { presentPendingLink() }
            .onChange(of: isPresenting) { presentPendingLink() }
            .onChange(of: visibleIDs) { (_: [String], ids: [String]) in selection.prune(to: ids) }
            .onChange(of: query) { (_: String, q: String) in selectFirstMatch(for: q) }
    }

    // Arrows are routed here: a key handler inside the split view's detail column never receives them.
    private var keyHandling: some View {
        dataObservers
            .onKeyPress(phases: .down) { (press: KeyPress) -> KeyPress.Result in handleKeyPress(press) }
    }

    private var accountSheets: some View {
        keyHandling
            .sheet(isPresented: $showingAdd) { AddAccountView() }
            .sheet(item: $qrAccount) { (account: Account) in QRExportView(account: account) }
            .sheet(item: $editAccount) { (account: Account) in EditAccountView(account: account) }
            .sheet(item: $linkPrefill) { (prefill: LinkPrefill) in AddAccountView(prefill: prefill.text) }
    }

    private var dialogs: some View {
        accountSheets
            .fileDropImport(isEnabled: acceptsDroppedFiles, picked: $pickedImport)
            .alert(prompt?.title ?? "", isPresented: promptPresented, presenting: prompt,
                   actions: promptActions)
            .confirmationDialog(folderRemoval?.title ?? "", isPresented: folderRemovalPresented,
                                titleVisibility: .visible, presenting: folderRemoval,
                                actions: folderRemovalActions, message: folderRemovalMessage)
    }

    private var backupSheets: some View {
        dialogs
            .sheet(isPresented: $exportingBackup) { ExportBackupView(onSaved: { (_: URL) in }) }
            .sheet(isPresented: $restoringBackup) { RestoreBackupView() }
    }

    private var acceptsDroppedFiles: Bool {
        !showingAdd && qrAccount == nil && editAccount == nil && prompt == nil
            && linkPrefill == nil && folderRemoval == nil
    }

    private var promptPresented: Binding<Bool> {
        Binding(get: { prompt != nil }, set: { if !$0 { dismissPrompt() } })
    }

    private var folderRemovalPresented: Binding<Bool> {
        Binding(get: { folderRemoval != nil }, set: { if !$0 { folderRemoval = nil } })
    }

    @ViewBuilder private func promptActions(_ p: TextPrompt) -> some View {
        TextField(p.field, text: $promptText)
        Button(p.confirm) { submit(p) }
        Button("Cancel", role: .cancel) { dismissPrompt() }
    }

    @ViewBuilder private func folderRemovalActions(_ removal: FolderRemoval) -> some View {
        Button(FolderRemoval.action, role: .destructive) { model.removeFolder(removal.folder) }
        Button("Cancel", role: .cancel) {}
    }

    private func folderRemovalMessage(_ removal: FolderRemoval) -> Text {
        Text(removal.message)
    }

    private func dismissPrompt() {
        prompt = nil
        promptText = ""
    }

    private func updateColumns(compact isCompact: Bool) {
        hidesSidebarToggle = isCompact
        if isCompact {
            withAnimation(reduceMotion ? nil : .easeInOut(duration: 0.25)) { columnVisibility = .detailOnly }
        } else {
            // The toggle has to be back before the sidebar opens, or a sidebar opened soon after launch covers the detail.
            DispatchQueue.main.async {
                guard !hidesSidebarToggle else { return }
                withAnimation(reduceMotion ? nil : .easeInOut(duration: 0.25)) { columnVisibility = .all }
            }
        }
    }

    private func leaveScopeIfGone(folders f: [String]) {
        if case .folder(let name) = currentScope, !f.contains(name) { scope = .all }
    }

    private func leaveScopeIfGone(tags t: [String]) {
        if case .tag(let name) = currentScope, !t.contains(name) { scope = .all }
    }

    private func selectFirstMatch(for q: String) {
        guard !q.trimmingCharacters(in: .whitespaces).isEmpty else { return }
        if let first = visibleIDs.first { selection.select(first) } else { selection.clear() }
    }

    private func handleKeyPress(_ press: KeyPress) -> KeyPress.Result {
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
                                Button("Remove Folder…") {
                                    folderRemoval = FolderRemoval(folder: f, accounts: model.accounts)
                                }
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
            if !model.allTags.isEmpty {
                Section("Tags") {
                    ForEach(model.allTags, id: \.self) { t in
                        Label(t, systemImage: "tag")
                            .badge(counts.tags[t] ?? 0)
                            .tag(VaultScope.tag(t))
                    }
                }
            }
        }
        .listItemTint(.fixed(Palette.accentInk))
        // Stays inside the column width: outside it, the removal drops the width preference.
        .toolbar(removing: hidesSidebarToggle ? .sidebarToggle : nil)
        .navigationSplitViewColumnWidth(min: 180, ideal: 210, max: 280)
        .safeAreaInset(edge: .bottom) {
            HStack {
                Wordmark(size: 13)
                Spacer()
            }.padding(10)
        }
    }

    private var densityToggleTitle: String { compact ? "Expand to full window" : "Shrink to compact window" }

    private var densityToggleSymbol: String {
        compact ? "arrow.up.left.and.arrow.down.right" : "arrow.down.right.and.arrow.up.left"
    }

    private var noticeInset: CGFloat {
        NoticeLayout.bottomInset(noticeHeight: model.notice == nil || showingAdd ? 0 : noticeHeight,
                                 bulkBarVisible: selection.isMultiple)
    }

    private func noticeCapsule(_ n: AppModel.Notice) -> some View {
        let isFailure = if case .failure = n { true } else { false }
        let shape = RoundedRectangle(cornerRadius: 16, style: .continuous)
        return NoticeLine(n)
            .fixedSize(horizontal: false, vertical: true)
            .padding(.horizontal, 14).padding(.vertical, 9)
            .background(Palette.surfaceHi, in: shape)
            .overlay(shape.stroke(Palette.border, lineWidth: 1))
            .contentShape(shape)
            .onTapGesture { if isFailure { dismissNotice(n) } }
            .accessibilityElement(children: .combine)
            .accessibilityAddTraits(isFailure ? .isButton : [])
            .accessibilityHint(isFailure ? "Dismisses this message" : "")
            .accessibilityAction(named: "Dismiss") { dismissNotice(n) }
            .onGeometryChange(for: CGFloat.self) { $0.size.height } action: { noticeHeight = $0 }
            .padding(.horizontal, compact ? 10 : 14)
            .padding(.bottom, NoticeLayout.bottomPadding(bulkBarVisible: selection.isMultiple))
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
                .overlay(alignment: .bottom) {
                    if let n = model.notice, !showingAdd { noticeCapsule(n) }
                }
                .safeAreaInset(edge: .bottom) {
                    if selection.isMultiple { bulkBar }
                }
                .animation(reduceMotion ? nil : .spring(response: 0.35, dampingFraction: 0.8), value: model.notice)
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
            searchFocused = true
            // Repeated next turn: the field can refuse focus while it is still being created.
            DispatchQueue.main.async { searchFocused = true }
        }
    }

    /// In-content search field — fixed position (never reflows like a toolbar
    /// item), focused on launch. Arrows move the selection, Return acts on it.
    private var searchHeader: some View {
        HStack(spacing: 8) {
            Image(systemName: "magnifyingglass")
                .font(Typo.label(13)).foregroundStyle(Palette.textFaint)
                .accessibilityHidden(true)
            TextField("Search", text: $query)
                .textFieldStyle(.plain).font(Typo.label(13))
                .focused($searchFocused)
                .accessibilityIdentifier("VaultSearch")
                .onSubmit { activateCursor() }
                .onExitCommand { escape() }
            if !query.isEmpty {
                Button { query = ""; searchFocused = true } label: {
                    Image(systemName: "xmark.circle.fill").font(Typo.label(13))
                        .foregroundStyle(Palette.textFaint)
                        .frame(width: 24, height: 24)
                        .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .accessibilityLabel("Clear search")
                .padding(.vertical, -4)
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
                .font(.system(size: emptyIconSize)).foregroundStyle(Palette.textFaint)
            Text(searching ? "No matches" : emptyListText)
                .font(Typo.label(13)).foregroundStyle(Palette.textSecondary)
        }.frame(maxWidth: .infinity, maxHeight: .infinity)
    }

    private var emptyListText: String {
        switch currentScope {
        case .pinned: return "No pinned accounts"
        case .folder(let f): return "No accounts in \(f)"
        case .tag(let t): return "No accounts tagged \(t)"
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
            let template = RowLayout.nextTemplate(for: model.accounts)
            ScrollViewReader { proxy in
                List {
                    ForEach(sections) { section in
                        if section.kind == .single {
                            sectionRows(section, template: template)
                        } else {
                            Section {
                                sectionRows(section, template: template)
                            } header: {
                                SectionLabel(section.kind == .pinned ? "Pinned" : "Accounts")
                                    .padding(.horizontal, compact ? 6 : 8)
                            }
                        }
                    }
                }
                .listStyle(.plain)
                .scrollContentBackground(.hidden)
                .environment(\.defaultMinListRowHeight, 1)
                .safeAreaPadding(.bottom, noticeInset)
                .frame(maxWidth: .infinity)   // fills the centered column from detailScaffold
                .onChange(of: selection.cursor) { _, cursor in
                    guard let cursor else { return }
                    withAnimation(reduceMotion ? nil : .easeOut(duration: 0.15)) { proxy.scrollTo(cursor) }
                }
            }
        }
    }

    private func sectionRows(_ section: VaultSection, template: String) -> some View {
        let ids = section.accounts.map(\.id)
        return ForEach(section.accounts, id: \.id) { account in
            row(account, template: template)
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
                        Image(systemName: "chevron.down").font(Typo.label(9, .semibold))
                    }
                    .foregroundStyle(isFolder(sel) ? Palette.accentInk : Palette.textSecondary)
                    .padding(.horizontal, 11).padding(.vertical, 6)
                    .background(Palette.surfaceHi, in: RoundedRectangle(cornerRadius: 9))
                    .overlay(RoundedRectangle(cornerRadius: 9).stroke(Palette.border, lineWidth: 1))
                }
                .menuStyle(.borderlessButton).menuIndicator(.hidden).fixedSize()
            }
            if !model.allTags.isEmpty {
                Menu {
                    ForEach(model.allTags, id: \.self) { t in
                        Button(t) { scope = .tag(t) }
                    }
                } label: {
                    HStack(spacing: 6) {
                        Text(tagLabel(sel)).font(Typo.label(12.5, .medium))
                        Image(systemName: "chevron.down").font(Typo.label(9, .semibold))
                    }
                    .foregroundStyle(tagLabel(sel) != "Tags" ? Palette.accentInk : Palette.textSecondary)
                    .padding(.horizontal, 11).padding(.vertical, 6)
                    .background(Palette.surfaceHi, in: RoundedRectangle(cornerRadius: 9))
                    .overlay(RoundedRectangle(cornerRadius: 9).stroke(Palette.border, lineWidth: 1))
                }
                .menuStyle(.borderlessButton).menuIndicator(.hidden).fixedSize()
            }
        }
        .padding(.horizontal, 14).padding(.top, 10).padding(.bottom, 2)
    }

    private func tagLabel(_ s: VaultScope) -> String {
        if case .tag(let t) = s { return t }
        return "Tags"
    }

    private func presentPendingLink() {
        guard let link = model.pendingLink, !isPresenting else { return }
        model.pendingLink = nil
        linkPrefill = link
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
                Label(state.pinTitle, systemImage: state.pinSymbol)
            }
            .buttonStyle(.plain)
            .help(state.pinTitle)
            .accessibilityLabel(state.pinTitle)
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
            .help("Move to Folder")
            .accessibilityLabel("Move to Folder")
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
            .help("Tags")
            .accessibilityLabel("Tags")
            Button { selection.clear() } label: {
                Image(systemName: "xmark").font(Typo.label(11, .semibold))
                    .frame(width: 24, height: 24)
                    .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .padding(.vertical, -4)
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

    @ViewBuilder private func row(_ account: Account, template: String) -> some View {
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
                       nextTemplate: template,
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
                    let state = AccountMenuState(targets: [account])
                    Label(state.pinTitle, systemImage: state.pinSymbol)
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
            let now = model.ticker.refresh()
            Task { @MainActor in
                if await model.copyConfirmed(account, at: now) { markCopied(account, nextUntil: nil) }
            }
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
