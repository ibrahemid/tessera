import AppKit
import SwiftUI
import TesseraCore

// MARK: Account row

/// The only view that observes the 1 Hz clock. It reads the tick, asks the model
/// for the current code (memoized per period slot) and seconds remaining, and
/// hands both to the pure `AccountRowView`. Keeping the observation here is what
/// stops a tick from re-rendering the window, the sidebar, or any open sheet.
struct LiveAccountRow: View {
    @EnvironmentObject var model: AppModel
    @ObservedObject var ticker: Ticker
    let account: Account
    let copied: Bool
    var copiedNextUntil: Date?
    var selected = false
    var hideCodes = false
    var compact: Bool
    var reduceMotion: Bool
    var hovering = false
    var nextTemplate = RowLayout.nextTemplate(for: [])
    var onClick: (RowClick) -> Void

    var body: some View {
        let now = ticker.now
        AccountRowView(account: account,
                       remaining: model.remaining(for: account, at: now),
                       code: model.code(for: account, at: now),
                       nextCode: CodeTiming.showsNext(account, at: now, selected: selected)
                           ? model.nextCode(for: account, at: now) : nil,
                       copied: copied,
                       copiedNext: copiedNextUntil.map { now < $0 } ?? false,
                       selected: selected,
                       hideCodes: hideCodes,
                       compact: compact,
                       reduceMotion: reduceMotion,
                       hovering: hovering,
                       nextTemplate: nextTemplate,
                       onClick: onClick)
    }
}

struct AccountRowView: View {
    let account: Account
    let remaining: Int
    let code: String
    var nextCode: String?
    let copied: Bool
    var copiedNext = false
    var selected = false
    var hideCodes = false
    var compact: Bool = false
    var reduceMotion: Bool
    var nextTemplate: String
    var onClick: (RowClick) -> Void
    @State private var hovering: Bool
    @State private var showsNextColumn = true
    @Environment(\.colorSchemeContrast) private var contrast
    @ScaledMetric(relativeTo: .title) private var glyphBase: CGFloat = 40
    @ScaledMetric(relativeTo: .title) private var ringBase: CGFloat = 30
    @ScaledMetric(relativeTo: .title) private var codeBase: CGFloat = 24

    init(account: Account, remaining: Int, code: String, nextCode: String? = nil,
         copied: Bool, copiedNext: Bool = false, selected: Bool = false, hideCodes: Bool = false,
         compact: Bool = false, reduceMotion: Bool, hovering: Bool = false,
         nextTemplate: String = RowLayout.nextTemplate(for: []),
         onClick: @escaping (RowClick) -> Void) {
        self.account = account
        self.remaining = remaining
        self.code = code
        self.nextCode = nextCode
        self.copied = copied
        self.copiedNext = copiedNext
        self.selected = selected
        self.hideCodes = hideCodes
        self.compact = compact
        self.reduceMotion = reduceMotion
        self.nextTemplate = nextTemplate
        self.onClick = onClick
        _hovering = State(initialValue: hovering)
    }

    private var isHOTP: Bool { account.type == .hotp }
    private var period: Int { CodeTiming.period(of: account) }
    private var fraction: Double {
        guard !isHOTP, period > 0 else { return 1 }
        return max(0, min(1, Double(remaining) / Double(period)))
    }
    private var low: Bool { !isHOTP && remaining <= CodeTiming.showNextWithin }
    private var masked: Bool { hideCodes && !hovering && !selected }
    private var metrics: RowLayout.Metrics { RowLayout.metrics(compact: compact) }
    private var glyphSize: CGFloat { glyphBase * metrics.glyph / 40 }
    private var ringSize: CGFloat { ringBase * metrics.ring / 30 }
    private var codeSize: CGFloat { codeBase * metrics.codeSize / 24 }
    private var primaryName: String { account.displayName }
    private var shownNext: String? { masked ? nil : nextCode.flatMap { $0.isEmpty ? nil : $0 } }
    private var tagLine: String { account.tags.map { "#\($0)" }.joined(separator: " ") }
    private var rowFill: Color {
        if selected { return Palette.accentWash }
        return hovering ? Palette.surfaceHi : .clear
    }

    // Whole row is one button: click copies (or advances HOTP); Command-click and
    // Shift-click change the selection. Everything else lives in the menus.
    var body: some View {
        let isCompact = compact
        let template = nextTemplate
        let textScale = codeBase / 24
        Button(action: { onClick(RowClick(modifiers: NSApp?.currentEvent?.modifierFlags ?? [])) }) {
            ZStack(alignment: .leading) {
                // Measures the offered width; the content itself overflows it while the Next column is too wide.
                Color.clear
                    .frame(height: 0)
                    .onGeometryChange(for: Bool.self) { proxy in
                        RowLayout.showsNext(width: proxy.size.width, compact: isCompact, template: template, scale: textScale)
                    } action: { fits in
                        if showsNextColumn != fits { showsNextColumn = fits }
                    }
                rowContent
                    .contentShape(Rectangle())
                    .background(
                        RoundedRectangle(cornerRadius: 13, style: .continuous)
                            .fill(rowFill)
                    )
                    .overlay {
                        if selected {
                            RoundedRectangle(cornerRadius: 13, style: .continuous)
                                .stroke(contrast == .increased ? Palette.accentInk : Palette.accentSoft,
                                        lineWidth: contrast == .increased ? 1.5 : 1)
                        }
                    }
            }
        }
        .buttonStyle(.plain)
        .onHover { hovering = $0 }
        .accessibilityIdentifier("AccountRow")
        .accessibilityLabel(accessibilityName)
        .accessibilityValue(accessibilityCode)
        .accessibilityHint(isHOTP ? "Advances the counter and copies the new code" : "Copies the code")
        .accessibilityAddTraits(selected ? [.updatesFrequently, .isSelected] : .updatesFrequently)
    }

    private var rowContent: some View {
        HStack(spacing: metrics.spacing) {
            GlyphSquare(account: account, size: glyphSize)
            VStack(alignment: .leading, spacing: 2) {
                HStack(spacing: 5) {
                    Text(primaryName)
                        .font(Typo.label(compact ? 13.5 : 14.5, .semibold))
                        .foregroundStyle(Palette.textPrimary).lineLimit(1)
                    if !account.handle.isEmpty {
                        Text(account.handle)
                            .font(Typo.code(compact ? 10.5 : 11.5))
                            .foregroundStyle(Palette.accentInk)
                            .padding(.horizontal, 5).padding(.vertical, 1)
                            .background(RoundedRectangle(cornerRadius: 5, style: .continuous)
                                .fill(selected ? Palette.accentSoft : Palette.accentWash))
                            .fixedSize()
                            .layoutPriority(1)
                    }
                    if account.pinned {
                        Image(systemName: "star.fill")
                            .font(Typo.label(compact ? 8 : 9))
                            .foregroundStyle(Palette.accent)
                            .fixedSize()
                            .layoutPriority(1)
                    }
                }
                if !account.issuer.isEmpty || !account.tags.isEmpty {
                    HStack(spacing: 6) {
                        if !account.issuer.isEmpty {
                            Text(account.account).lineLimit(1)
                        }
                        if !account.tags.isEmpty {
                            Text(tagLine).lineLimit(1)
                        }
                    }
                    .font(Typo.label(compact ? 11 : 12))
                    .foregroundStyle(Palette.textSecondary)
                }
            }
            .frame(minWidth: metrics.nameMinimum, alignment: .leading)
            Spacer(minLength: RowLayout.spacerMinimum)
            HStack(spacing: metrics.spacing) {
                codeText
                if showsNextColumn { nextColumn }
            }
            .layoutPriority(1)
            trailing
        }
        .padding(.horizontal, metrics.padding)
        .padding(.vertical, compact ? 11 : 13)
    }

    private var codeText: some View {
        Text(groupCode(masked ? String(repeating: "•", count: code.count) : code))
            .font(Typo.code(codeSize)).monospacedDigit()
            .foregroundStyle(copied && !copiedNext ? Palette.accentInk : Palette.textPrimary)
            .contentTransition(reduceMotion || masked ? .identity : .numericText())
            .lineLimit(1)
            .fixedSize()
    }

    private var nextColumn: some View {
        ZStack(alignment: .leading) {
            nextBlock(nextTemplate).hidden()
            if let next = shownNext { nextBlock(groupCode(next)) }
        }
    }

    private func nextBlock(_ grouped: String) -> some View {
        VStack(alignment: .leading, spacing: 0) {
            Text("Next")
                .font(Typo.label(10, .medium))
                .foregroundStyle(Palette.textSecondary)
            Text(grouped)
                .font(Typo.code(metrics.nextSize)).monospacedDigit()
                .foregroundStyle(copied && copiedNext ? Palette.accentInk : Palette.textSecondary)
                .lineLimit(1)
        }
        .fixedSize()
    }

    private var accessibilityName: String {
        let base = account.issuer.isEmpty || account.account.isEmpty
            ? primaryName : "\(account.issuer), \(account.account)"
        let pinned = account.pinned ? "\(base), pinned" : base
        return account.tags.isEmpty ? pinned : "\(pinned), tags \(account.tags.joined(separator: ", "))"
    }

    private var accessibilityCode: String {
        if masked { return "Code hidden" }
        let spelled = code.map(String.init).joined(separator: " ")
        if isHOTP { return "\(spelled), counter \(account.counter)" }
        let base = "\(spelled), \(remaining) seconds left"
        guard showsNextColumn, let next = shownNext else { return base }
        return "\(base), next code \(next.map(String.init).joined(separator: " "))"
    }

    @ViewBuilder private var trailing: some View {
        if isHOTP {
            Image(systemName: "arrow.clockwise")
                .font(Typo.label(compact ? 13 : 14, .medium))
                .foregroundStyle(Palette.textSecondary)
                .frame(width: ringSize, height: ringSize)
                .accessibilityHidden(true)
        } else {
            CountdownRing(fraction: fraction, remaining: remaining, low: low,
                          size: ringSize, reduceMotion: reduceMotion)
                .accessibilityHidden(true)
        }
    }
}
