import CoreGraphics

enum NoticeLayout {
    static let gap: CGFloat = 8
    static let bottomPadding: CGFloat = 16
    static let bottomPaddingAboveBulkBar: CGFloat = 4

    static func bottomPadding(bulkBarVisible: Bool) -> CGFloat {
        bulkBarVisible ? bottomPaddingAboveBulkBar : bottomPadding
    }

    static func bottomInset(noticeHeight: CGFloat, bulkBarVisible: Bool) -> CGFloat {
        guard noticeHeight.isFinite, noticeHeight > 0 else { return 0 }
        return noticeHeight + bottomPadding(bulkBarVisible: bulkBarVisible) + gap
    }
}
