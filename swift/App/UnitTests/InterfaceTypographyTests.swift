import XCTest
import SwiftUI
@testable import Tessera

final class InterfaceTypographyTests: XCTestCase {
    func testEveryInterfaceSizeFollowsATextStyle() {
        let expected: [(CGFloat, Font.TextStyle)] = [
            (14.5, .body), (13.5, .body), (13, .body),
            (12.5, .callout), (12, .callout),
            (11.5, .subheadline), (11, .subheadline),
            (10.5, .footnote), (10, .footnote), (9, .footnote), (8, .footnote),
        ]
        for (size, style) in expected {
            XCTAssertEqual(Typo.textStyle(for: size), style, "\(size)")
        }
    }

    func testDisplaySizesStayFixed() {
        for size: CGFloat in [7, 15, 16, 18, 24] {
            XCTAssertNil(Typo.textStyle(for: size), "\(size)")
        }
    }
}
