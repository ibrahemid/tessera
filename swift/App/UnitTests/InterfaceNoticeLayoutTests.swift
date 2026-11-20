import XCTest
@testable import Tessera

final class InterfaceNoticeLayoutTests: XCTestCase {
    func testNoNoticeLeavesNoMargin() {
        XCTAssertEqual(NoticeLayout.bottomInset(noticeHeight: 0, bulkBarVisible: false), 0)
        XCTAssertEqual(NoticeLayout.bottomInset(noticeHeight: 0, bulkBarVisible: true), 0)
        XCTAssertEqual(NoticeLayout.bottomInset(noticeHeight: -5, bulkBarVisible: false), 0)
        XCTAssertEqual(NoticeLayout.bottomInset(noticeHeight: .nan, bulkBarVisible: false), 0)
        XCTAssertEqual(NoticeLayout.bottomInset(noticeHeight: .infinity, bulkBarVisible: false), 0)
    }

    func testMarginClearsTheNoticeAndItsPadding() {
        XCTAssertEqual(NoticeLayout.bottomInset(noticeHeight: 34, bulkBarVisible: false),
                       34 + NoticeLayout.bottomPadding + NoticeLayout.gap)
    }

    func testMarginAboveTheBulkBarUsesTheSmallerPadding() {
        XCTAssertEqual(NoticeLayout.bottomInset(noticeHeight: 52, bulkBarVisible: true),
                       52 + NoticeLayout.bottomPaddingAboveBulkBar + NoticeLayout.gap)
        XCTAssertEqual(NoticeLayout.bottomPadding(bulkBarVisible: true), NoticeLayout.bottomPaddingAboveBulkBar)
        XCTAssertEqual(NoticeLayout.bottomPadding(bulkBarVisible: false), NoticeLayout.bottomPadding)
    }

    func testTallerNoticesGetALargerMargin() {
        let one = NoticeLayout.bottomInset(noticeHeight: 34, bulkBarVisible: false)
        let three = NoticeLayout.bottomInset(noticeHeight: 70, bulkBarVisible: false)
        XCTAssertEqual(three - one, 36)
    }
}
