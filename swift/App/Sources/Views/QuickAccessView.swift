import SwiftUI
import TesseraCore

struct QuickAccessView: View {
    @EnvironmentObject var model: AppModel
    @ObservedObject var clock: QuickAccessClock
    var onClose: () -> Void
    var onOpenApp: () -> Void
    @State private var list: QuickAccessList
    @AppStorage(AppModel.hideCodesPrefKey) private var hideCodes = false
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @FocusState private var searchFocused: Bool
    private let hideCodesOverride: Bool?

    static let width: CGFloat = 400
    static let listHeight: CGFloat = 340

    init(clock: QuickAccessClock, onClose: @escaping () -> Void, onOpenApp: @escaping () -> Void) {
        self.clock = clock
        self.onClose = onClose
        self.onOpenApp = onOpenApp
        _list = State(initialValue: QuickAccessList())
        hideCodesOverride = nil
    }

    #if DEBUG
    init(clock: QuickAccessClock, query: String = "", cursor: String? = nil, hideCodes: Bool? = nil) {
        var preset = QuickAccessList()
        preset.query = query
        if let cursor { preset.point(at: cursor) }
        self.clock = clock
        onClose = {}
        onOpenApp = {}
        _list = State(initialValue: preset)
        hideCodesOverride = hideCodes
    }
    #endif

    private var hidesCodes: Bool { hideCodesOverride ?? hideCodes }
    private var visible: [Account] { list.visible(model.accounts) }

    var body: some View {
        Group {
            switch QuickAccessLockState(model) {
            case .open: openContent
            case .locked(let touchID): lockedContent(touchID: touchID)
            case .needsWindow: needsWindowContent
            }
        }
        .frame(width: Self.width)
        .background(Palette.background)
        .background(CaptureShield())
        .onExitCommand(perform: onClose)
    }

    // MARK: Open

    private var openContent: some View {
        let accounts = visible
        let order = accounts.map(\.id)
        return VStack(spacing: 0) {
            searchHeader(order: order, accounts: accounts)
                .padding(.horizontal, 12).padding(.top, 12).padding(.bottom, 8)
            Group {
                if accounts.isEmpty {
                    emptyResults
                } else {
                    rows(accounts, order: order)
                }
            }
            .frame(height: Self.listHeight)
            Divider().overlay(Palette.border)
            footer
        }
        .onAppear { DispatchQueue.main.async { searchFocused = true } }
        .onKeyPress(phases: .down) { press in
            guard let delta = QuickAccessList.moveDelta(key: press.key, modifiers: press.modifiers) else {
                return .ignored
            }
            list.move(by: delta, in: order)
            return .handled
        }
    }

    private func searchHeader(order: [String], accounts: [Account]) -> some View {
        HStack(spacing: 8) {
            Image(systemName: "magnifyingglass")
                .font(.system(size: 13)).foregroundStyle(Palette.textFaint)
                .accessibilityHidden(true)
            TextField("Search", text: $list.query)
                .textFieldStyle(.plain).font(Typo.label(13))
                .focused($searchFocused)
                .accessibilityLabel("Search accounts")
                .accessibilityIdentifier("QuickAccessSearch")
                .onSubmit { copy(list.target(in: order), from: accounts) }
            if !list.query.isEmpty {
                Button { list.query = "" } label: {
                    Image(systemName: "xmark.circle.fill").font(.system(size: 12)).foregroundStyle(Palette.textFaint)
                }
                .buttonStyle(.plain)
                .accessibilityLabel("Clear search")
            }
        }
        .padding(.horizontal, 11).padding(.vertical, 7)
        .background(Palette.surfaceHi, in: RoundedRectangle(cornerRadius: 9))
        .overlay(RoundedRectangle(cornerRadius: 9).stroke(searchFocused ? Palette.accent : Palette.border, lineWidth: 1))
    }

    private func rows(_ accounts: [Account], order: [String]) -> some View {
        let now = clock.now
        return ScrollViewReader { proxy in
            ScrollView {
                LazyVStack(spacing: 4) {
                    ForEach(accounts, id: \.id) { account in
                        let highlighted = list.isHighlighted(account.id, in: order)
                        AccountRowView(account: account,
                                       remaining: model.remaining(for: account, at: now),
                                       code: model.code(for: account, at: now),
                                       nextCode: CodeTiming.showsNext(account, at: now, selected: highlighted)
                                           ? model.nextCode(for: account, at: now) : nil,
                                       copied: false,
                                       selected: highlighted,
                                       hideCodes: hidesCodes,
                                       compact: true,
                                       reduceMotion: reduceMotion) { _ in
                            copy(account.id, from: accounts)
                        }
                        .id(account.id)
                    }
                }
                .padding(.horizontal, 8).padding(.vertical, 4)
            }
            .onChange(of: list.cursor) { _, cursor in
                guard let cursor else { return }
                withAnimation(reduceMotion ? nil : .easeOut(duration: 0.15)) { proxy.scrollTo(cursor) }
            }
        }
    }

    private var emptyResults: some View {
        let searching = !list.query.isEmpty
        return VStack(spacing: 8) {
            Image(systemName: searching ? "magnifyingglass" : "tray")
                .font(.system(size: 22)).foregroundStyle(Palette.textFaint)
                .accessibilityHidden(true)
            Text(searching ? "No matches" : "No accounts")
                .font(Typo.label(13)).foregroundStyle(Palette.textSecondary)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }

    private var footer: some View {
        HStack {
            Button("Open Tessera") { onOpenApp() }
                .buttonStyle(.plain).font(Typo.label(12)).foregroundStyle(Palette.textSecondary)
            Spacer()
        }
        .padding(.horizontal, 14).padding(.vertical, 9)
    }

    private func copy(_ id: String?, from accounts: [Account]) {
        guard let id, let account = accounts.first(where: { $0.id == id }) else { return }
        if model.copyFromQuickAccess(account, at: clock.refresh()) { onClose() }
    }

    // MARK: Locked

    private func lockedContent(touchID: Bool) -> some View {
        VStack(spacing: 14) {
            MosaicMark(side: 40)
                .accessibilityHidden(true)
            Text("Tessera is locked").font(Typo.display(17)).foregroundStyle(Palette.textPrimary)
            Button { model.unlock() } label: {
                Label(touchID ? "Unlock with Touch ID" : "Unlock", systemImage: touchID ? "touchid" : "lock.open")
                    .font(Typo.label(13, .semibold)).foregroundStyle(Palette.onAccent)
                    .frame(maxWidth: 220).padding(.vertical, 9)
                    .background(Palette.accentFill, in: RoundedRectangle(cornerRadius: 10))
            }
            .buttonStyle(.plain)
            .keyboardShortcut(.defaultAction)
            if let e = model.errorMessage { ErrorLine(e) }
        }
        .padding(28)
        .frame(maxWidth: .infinity)
    }

    private var needsWindowContent: some View {
        VStack(spacing: 14) {
            MosaicMark(side: 40)
                .accessibilityHidden(true)
            Text("Tessera is locked").font(Typo.display(17)).foregroundStyle(Palette.textPrimary)
            Text("Open Tessera to unlock your vault.")
                .font(Typo.label(12)).foregroundStyle(Palette.textSecondary)
            Button { onOpenApp() } label: {
                Text("Open Tessera")
                    .font(Typo.label(13, .semibold)).foregroundStyle(Palette.onAccent)
                    .frame(maxWidth: 220).padding(.vertical, 9)
                    .background(Palette.accentFill, in: RoundedRectangle(cornerRadius: 10))
            }
            .buttonStyle(.plain)
            .keyboardShortcut(.defaultAction)
        }
        .padding(28)
        .frame(maxWidth: .infinity)
    }
}
