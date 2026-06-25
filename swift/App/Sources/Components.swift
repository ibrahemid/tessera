import SwiftUI
import TesseraCore

/// The signature element: a depleting ring around a colored issuer monogram.
/// The ring warms from gold to burnt-orange in the final seconds.
struct TesseraTile: View {
    let account: Account
    let remaining: Int
    var reduceMotion: Bool

    private var fraction: Double {
        guard account.type != .hotp, account.period > 0 else { return 1 }
        return max(0, min(1, Double(remaining) / Double(account.period)))
    }
    private var low: Bool { account.type != .hotp && remaining <= 5 }
    private var monogram: String {
        let s = account.issuer.isEmpty ? account.account : account.issuer
        return String(s.prefix(1)).uppercased()
    }
    private var hue: Color { Palette.tileColor(for: account.issuer + account.account) }

    var body: some View {
        ZStack {
            if account.type == .hotp {
                Circle().fill(hue.opacity(0.16))
                Image(systemName: "number")
                    .font(.system(size: 15, weight: .bold))
                    .foregroundStyle(hue)
            } else {
                Circle().stroke(Palette.ringTrack, lineWidth: 3)
                Circle()
                    .trim(from: 0, to: fraction)
                    .stroke(low ? Palette.warning : Palette.accent,
                            style: StrokeStyle(lineWidth: 3, lineCap: .round))
                    .rotationEffect(.degrees(-90))
                    .animation(reduceMotion ? nil : .linear(duration: 1), value: fraction)
                Circle().fill(hue.opacity(0.16)).padding(6)
                Text(monogram)
                    .font(.system(size: 15, weight: .bold, design: .rounded))
                    .foregroundStyle(hue)
            }
        }
        .frame(width: 38, height: 38)
    }
}

/// Pressable container that gives tactile feedback on hover/press.
struct PressableTile<Content: View>: View {
    var action: () -> Void
    @ViewBuilder var content: Content
    @State private var hovering = false

    var body: some View {
        Button(action: action) { content }
            .buttonStyle(.plain)
            .background(
                RoundedRectangle(cornerRadius: Metrics.tileRadius)
                    .fill(Palette.surface)
                    .shadow(color: .black.opacity(hovering ? 0.10 : 0.05),
                            radius: hovering ? 7 : 3, y: hovering ? 3 : 1)
            )
            .overlay(
                RoundedRectangle(cornerRadius: Metrics.tileRadius)
                    .stroke(Palette.border, lineWidth: 1)
            )
            .onHover { hovering = $0 }
            .animation(.easeOut(duration: 0.15), value: hovering)
    }
}

/// Groups a 6-digit code as "123 456" for readability; leaves others intact.
func groupCode(_ code: String) -> String {
    guard code.count == 6 else { return code }
    let i = code.index(code.startIndex, offsetBy: 3)
    return "\(code[..<i]) \(code[i...])"
}
