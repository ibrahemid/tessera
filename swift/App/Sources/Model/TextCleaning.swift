import Foundation

enum TextCleaning {
    static func clean(_ raw: String) -> String {
        var scalars = String.UnicodeScalarView()
        for s in raw.unicodeScalars where !isStripped(s) { scalars.append(s) }
        let kept = Array(scalars)
        guard let first = kept.firstIndex(where: { !$0.properties.isWhitespace }),
              let last = kept.lastIndex(where: { !$0.properties.isWhitespace }) else { return "" }
        return String(String.UnicodeScalarView(kept[first...last]))
    }

    static func isStripped(_ s: Unicode.Scalar) -> Bool {
        switch s.value {
        case 0x00...0x1F, 0x7F...0x9F, 0x202A...0x202E, 0x2066...0x2069, 0xFEFF: return true
        default: return false
        }
    }
}
