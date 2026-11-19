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
    var compact: Bool
    var reduceMotion: Bool
    var onCopy: () -> Void
    var onAdvance: () -> Void

    var body: some View {
        let now = ticker.now
        AccountRowView(account: account,
                       remaining: model.remaining(for: account, at: now),
                       code: model.code(for: account, at: now),
                       copied: copied,
                       compact: compact,
                       reduceMotion: reduceMotion,
                       onCopy: onCopy,
                       onAdvance: onAdvance)
    }
}

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
    @ScaledMetric(relativeTo: .title) private var glyphBase: CGFloat = 40
    @ScaledMetric(relativeTo: .title) private var ringBase: CGFloat = 30
    @ScaledMetric(relativeTo: .title) private var codeBase: CGFloat = 24

    private var isHOTP: Bool { account.type == .hotp }
    private var fraction: Double {
        guard !isHOTP, account.period > 0 else { return 1 }
        return max(0, min(1, Double(remaining) / Double(account.period)))
    }
    private var low: Bool { !isHOTP && remaining <= 5 }
    private var glyphSize: CGFloat { glyphBase * (compact ? 36 : 40) / 40 }
    private var ringSize: CGFloat { ringBase * (compact ? 26 : 30) / 30 }
    private var codeSize: CGFloat { codeBase * (compact ? 20 : 24) / 24 }
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
                        if !account.handle.isEmpty {
                            Text(account.handle)
                                .font(Typo.code(compact ? 10.5 : 11.5))
                                .foregroundStyle(Palette.accentInk)
                                .padding(.horizontal, 5).padding(.vertical, 1)
                                .background(RoundedRectangle(cornerRadius: 5, style: .continuous)
                                    .fill(Palette.accentWash))
                                .fixedSize()
                        }
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
                    .foregroundStyle(copied ? Palette.accentInk : Palette.textPrimary)
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
