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
        themed(light: light, dark: dark, lightHC: light, darkHC: dark)
    }

    static func themed(light: UInt32, dark: UInt32, lightHC: UInt32, darkHC: UInt32) -> Color {
        Color(nsColor: NSColor(name: nil) { appearance in
            let match = appearance.bestMatch(from: [.aqua, .darkAqua,
                                                    .accessibilityHighContrastAqua,
                                                    .accessibilityHighContrastDarkAqua])
            let hex: UInt32
            if match == .darkAqua { hex = dark }
            else if match == .accessibilityHighContrastAqua { hex = lightHC }
            else if match == .accessibilityHighContrastDarkAqua { hex = darkHC }
            else { hex = light }
            return NSColor(Color(hex: hex))
        })
    }
}

enum Palette {
    // Surfaces: cool paper in light, ink (not pure black) in dark.
    static let background = Color.themed(light: 0xFCFCFD, dark: 0x131419)
    static let surface    = Color.themed(light: 0xFFFFFF, dark: 0x1C1E26)
    static let surfaceHi  = Color.themed(light: 0xF4F4F6, dark: 0x242732)
    static let border     = Color.themed(light: 0xECECEF, dark: 0x2A2D38, lightHC: 0x8E929C, darkHC: 0x6A6E7A)

    static let textPrimary   = Color.themed(light: 0x1A1C22, dark: 0xF2F2F5)
    static let textSecondary = Color.themed(light: 0x636671, dark: 0x9DA1AD, lightHC: 0x4E525C, darkHC: 0x9DA1AD)
    static let textFaint     = Color.themed(light: 0x858994, dark: 0x737785, lightHC: 0x4E525C, darkHC: 0x9DA1AD)

    // Antique gold accent — the tessera.
    static let accent     = Color.themed(light: 0xA9791F, dark: 0xE3B23C)
    static let accentInk  = Color.themed(light: 0x7B5818, dark: 0xE3B23C)
    static let accentFill = Color.themed(light: 0x8E6516, dark: 0xE3B23C)
    static let accentSoft = Color.themed(light: 0xEBD9AE, dark: 0x4A3C1C)
    // Tinted accent surface for selected nav / toggle states.
    static let accentWash = Color.themed(light: 0xF2E6C8, dark: 0x2A2616)
    // Readable label on top of `accentFill` (dark ink on bright gold, white on deep gold).
    static let onAccent   = Color.themed(light: 0xFFFFFF, dark: 0x1C1505)
    static let ringTrack  = Color.themed(light: 0xE8E8EC, dark: 0x2E313C)
    static let warning    = Color.themed(light: 0xB63D0B, dark: 0xF97316)

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
    static func code(_ size: CGFloat) -> Font {
        guard let style = textStyle(for: size) else { return .system(size: size, weight: .medium, design: .monospaced) }
        return .custom(NSFont.monospacedSystemFont(ofSize: size, weight: .medium).fontName, size: size, relativeTo: style)
    }

    // Text-style fonts carry taller line metrics on macOS; the system face at the same size keeps the layout.
    static func label(_ size: CGFloat, _ weight: Font.Weight = .regular) -> Font {
        guard let style = textStyle(for: size) else { return .system(size: size, weight: weight) }
        return .custom(NSFont.systemFont(ofSize: size).fontName, size: size, relativeTo: style).weight(weight)
    }

    static func textStyle(for size: CGFloat) -> Font.TextStyle? {
        switch size {
        case 13..<15: .body
        case 12..<13: .callout
        case 11..<12: .subheadline
        case 8..<11: .footnote
        default: nil
        }
    }
}

enum Metrics {
    static let windowWidth: CGFloat = 380
    static let windowHeight: CGFloat = 500
    static let tileRadius: CGFloat = 14
    static let pad: CGFloat = 14
}
