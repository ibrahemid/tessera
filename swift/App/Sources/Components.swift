import SwiftUI
import TesseraCore

/// The one naming rule for rows, toasts, and sheets: issuer, or the account
/// label when there is no issuer.
extension Account {
    var displayName: String { issuer.isEmpty ? account : issuer }
}

/// The leading mark on every row: a colored rounded square holding the issuer's
/// monogram. Color is deterministic per issuer (see Palette.tileColor).
struct GlyphSquare: View {
    let account: Account
    var size: CGFloat = 40

    private var hue: Color { Palette.tileColor(for: account.issuer + account.account) }
    private var monogram: String {
        String(account.displayName.prefix(1)).uppercased()
    }

    var body: some View {
        RoundedRectangle(cornerRadius: size * 0.28, style: .continuous)
            .fill(hue)
            .frame(width: size, height: size)
            .overlay(
                Text(monogram)
                    .font(.system(size: size * 0.4, weight: .bold, design: .rounded))
                    .foregroundStyle(.white)
            )
    }
}

/// The signature element: a depleting ring with the seconds-remaining inside it.
/// Sits at a row's trailing edge and warms from gold to burnt-orange in the final
/// seconds. HOTP rows show an advance glyph instead of this ring.
struct CountdownRing: View {
    let fraction: Double
    let remaining: Int
    let low: Bool
    var size: CGFloat = 30
    var reduceMotion: Bool = false

    var body: some View {
        ZStack {
            Circle().stroke(Palette.ringTrack, lineWidth: 2.6)
            Circle()
                .trim(from: 0, to: fraction)
                .stroke(low ? Palette.warning : Palette.accent,
                        style: StrokeStyle(lineWidth: 2.6, lineCap: .round))
                .rotationEffect(.degrees(-90))
                .animation(reduceMotion ? nil : .linear(duration: 1), value: fraction)
            Text("\(remaining)")
                .font(.system(size: size * 0.34, weight: .medium, design: .monospaced))
                .monospacedDigit()
                .foregroundStyle(low ? Palette.warning : Palette.textSecondary)
        }
        .frame(width: size, height: size)
    }
}

/// Groups a 6-digit code as "123 456" for readability; leaves others intact.
func groupCode(_ code: String) -> String {
    guard code.count == 6 else { return code }
    let i = code.index(code.startIndex, offsetBy: 3)
    return "\(code[..<i]) \(code[i...])"
}
