import CoreGraphics
import TesseraCore

enum RowLayout {
    struct Metrics: Equatable {
        let padding: CGFloat
        let spacing: CGFloat
        let nameMinimum: CGFloat
        let glyph: CGFloat
        let ring: CGFloat
        let codeSize: CGFloat
        let nextSize: CGFloat
    }

    static let regular = Metrics(padding: 16, spacing: 16, nameMinimum: 120, glyph: 40, ring: 30,
                                 codeSize: 24, nextSize: 12.5)
    static let compact = Metrics(padding: 12, spacing: 12, nameMinimum: 96, glyph: 36, ring: 26,
                                 codeSize: 20, nextSize: 11.5)
    static let spacerMinimum: CGFloat = 8
    static let codeAdvance: CGFloat = 0.625
    static let nextAdvance: CGFloat = 0.65625
    static let defaultCodeLength = 6
    static let steamCodeLength = 5

    static func metrics(compact: Bool) -> Metrics { compact ? Self.compact : regular }

    static func minimumWidth(compact: Bool, template: String, scale: CGFloat = 1) -> CGFloat {
        let m = metrics(compact: compact)
        let textScale = scale.isFinite && scale > 0 ? scale : 1
        let fixed = 2 * m.padding + 5 * m.spacing + m.nameMinimum + spacerMinimum
        let codes = CGFloat(template.count) * (codeAdvance * m.codeSize + nextAdvance * m.nextSize)
        return (fixed + (m.glyph + m.ring + codes) * textScale).rounded(.up)
    }

    static func showsNext(width: CGFloat, compact: Bool, template: String, scale: CGFloat = 1) -> Bool {
        width.isFinite && width >= minimumWidth(compact: compact, template: template, scale: scale)
    }

    static func codeLength(of account: Account) -> Int {
        account.type == .steam ? steamCodeLength : account.digits
    }

    static func nextTemplate(for accounts: [Account]) -> String {
        let longest = accounts.map { groupCode(String(repeating: "0", count: codeLength(of: $0))) }
            .max { $0.count < $1.count }
        return longest ?? groupCode(String(repeating: "0", count: defaultCodeLength))
    }
}
