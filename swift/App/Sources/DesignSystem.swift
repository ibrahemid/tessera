import SwiftUI

// Tessera's visual system. Identity: gold tesserae (Byzantine mosaic tiles) on
// calm ink/slate neutrals. Every value adapts to light/dark; nothing defaults to
// a hardcoded dark look.

extension Color {
    init(hex: UInt32) {
        self.init(.sRGB,
                  red: Double((hex >> 16) & 0xff) / 255,
                  green: Double((hex >> 8) & 0xff) / 255,
                  blue: Double(hex & 0xff) / 255,
                  opacity: 1)
    }

    /// A color that resolves differently in light vs dark appearance.
    static func themed(light: UInt32, dark: UInt32) -> Color {
        Color(nsColor: NSColor(name: nil) { appearance in
            let isDark = appearance.bestMatch(from: [.aqua, .darkAqua]) == .darkAqua
            return NSColor(Color(hex: isDark ? dark : light))
        })
    }
}

enum Palette {
    // Surfaces: cool paper in light, ink (not pure black) in dark.
    static let background = Color.themed(light: 0xFCFCFD, dark: 0x131419)
    static let surface    = Color.themed(light: 0xFFFFFF, dark: 0x1C1E26)
    static let surfaceHi  = Color.themed(light: 0xF4F4F6, dark: 0x242732)
    static let border     = Color.themed(light: 0xECECEF, dark: 0x2A2D38)

    static let textPrimary   = Color.themed(light: 0x1A1C22, dark: 0xF2F2F5)
    static let textSecondary = Color.themed(light: 0x6B6F7B, dark: 0x9DA1AD)
    static let textFaint     = Color.themed(light: 0xA4A8B2, dark: 0x6A6E7A)

    // Antique gold accent — the tessera.
    static let accent     = Color.themed(light: 0xA9791F, dark: 0xE3B23C)
    static let accentSoft = Color.themed(light: 0xEBD9AE, dark: 0x4A3C1C)
    // Tinted accent surface for selected nav / toggle states.
    static let accentWash = Color.themed(light: 0xF2E6C8, dark: 0x2A2616)
    // Readable label on top of `accent` (dark ink on bright gold, white on deep gold).
    static let onAccent   = Color.themed(light: 0xFFFFFF, dark: 0x1C1505)
    static let ringTrack  = Color.themed(light: 0xE8E8EC, dark: 0x2E313C)
    static let warning    = Color.themed(light: 0xC2410C, dark: 0xF97316)

    // Curated, non-rainbow hues for per-issuer tesserae (deterministic by issuer).
    static let tileHues: [UInt32] = [
        0x3B6FB0, // blue
        0x2A8C7E, // teal
        0x8E5BA6, // plum
        0xB5562E, // rust
        0x3E7D4F, // forest
        0x4F5BB0, // indigo
        0xB0506E, // rose
        0xB08A2E, // amber
    ]

    /// Deterministic tile color for an issuer/account label.
    static func tileColor(for key: String) -> Color {
        var hash: UInt32 = 2166136261
        for byte in key.utf8 { hash = (hash ^ UInt32(byte)) &* 16777619 }
        return Color(hex: tileHues[Int(hash % UInt32(tileHues.count))])
    }
}

enum Typo {
    static func display(_ size: CGFloat) -> Font { .system(size: size, weight: .semibold, design: .rounded) }
    static func code(_ size: CGFloat) -> Font { .system(size: size, weight: .medium, design: .monospaced) }
    static func label(_ size: CGFloat, _ weight: Font.Weight = .regular) -> Font { .system(size: size, weight: weight) }
}

enum Metrics {
    static let windowWidth: CGFloat = 380
    static let windowHeight: CGFloat = 500
    static let tileRadius: CGFloat = 14
    static let pad: CGFloat = 14
}
