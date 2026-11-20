import AppKit
import SwiftUI
import XCTest
import TesseraCore
@testable import Tessera

final class InterfaceRowLayoutTests: XCTestCase {
    private func account(_ id: String, type: OTPType = .totp, digits: Int = 6) -> Account {
        Account(id: id, type: type, issuer: id, account: "", secret: Data("12345678901234567890".utf8), digits: digits)
    }

    private let sixDigits = "000 000"
    private let eightDigits = "00000000"

    func testMinimumWidthAddsUpTheRowParts() {
        XCTAssertEqual(RowLayout.minimumWidth(compact: false, template: eightDigits), 496)
        XCTAssertEqual(RowLayout.minimumWidth(compact: false, template: sixDigits), 473)
        XCTAssertEqual(RowLayout.minimumWidth(compact: true, template: eightDigits), 411)
        XCTAssertEqual(RowLayout.minimumWidth(compact: true, template: sixDigits), 391)
    }

    @MainActor
    func testAdvancesCoverTheRenderedCodeText() {
        let cases: [(CGFloat, CGFloat)] = [
            (RowLayout.regular.codeSize, RowLayout.codeAdvance), (RowLayout.compact.codeSize, RowLayout.codeAdvance),
            (RowLayout.regular.nextSize, RowLayout.nextAdvance), (RowLayout.compact.nextSize, RowLayout.nextAdvance)
        ]
        for (size, advance) in cases {
            for template in [sixDigits, eightDigits] {
                let text = NSHostingView(rootView: Text(template).font(Typo.code(size)).monospacedDigit().fixedSize())
                XCTAssertLessThanOrEqual(text.fittingSize.width, CGFloat(template.count) * advance * size,
                                         "\(template) at \(size)")
            }
        }
    }

    func testLongerCodesNeedAWiderRow() {
        for compact in [false, true] {
            XCTAssertLessThan(RowLayout.minimumWidth(compact: compact, template: "00000"),
                              RowLayout.minimumWidth(compact: compact, template: sixDigits))
            XCTAssertLessThan(RowLayout.minimumWidth(compact: compact, template: sixDigits),
                              RowLayout.minimumWidth(compact: compact, template: eightDigits))
        }
    }

    func testTextSizeScalesOnlyTheGlyphRingAndCodes() {
        let base = RowLayout.minimumWidth(compact: false, template: sixDigits)
        let larger = RowLayout.minimumWidth(compact: false, template: sixDigits, scale: 1.5)
        let smaller = RowLayout.minimumWidth(compact: false, template: sixDigits, scale: 0.5)
        XCTAssertGreaterThan(larger, base)
        XCTAssertLessThan(larger, base * 1.5)
        XCTAssertLessThan(smaller, base)
        XCTAssertGreaterThan(smaller, base * 0.5)
    }

    func testInvalidScalesKeepTheBaseMinimumWidth() {
        for compact in [false, true] {
            let base = RowLayout.minimumWidth(compact: compact, template: sixDigits)
            for scale in [0, -1, CGFloat.nan, .infinity] {
                XCTAssertEqual(RowLayout.minimumWidth(compact: compact, template: sixDigits, scale: scale), base)
            }
        }
    }

    func testNextColumnShowsFromTheMinimumWidthUp() {
        for compact in [false, true] {
            let minimum = RowLayout.minimumWidth(compact: compact, template: sixDigits)
            XCTAssertTrue(RowLayout.showsNext(width: minimum, compact: compact, template: sixDigits))
            XCTAssertTrue(RowLayout.showsNext(width: minimum + 200, compact: compact, template: sixDigits))
            XCTAssertFalse(RowLayout.showsNext(width: minimum - 0.5, compact: compact, template: sixDigits))
        }
    }

    func testCompactRowsKeepTheNextColumnAtWidthsWhereRegularRowsDropIt() {
        let width = RowLayout.minimumWidth(compact: true, template: eightDigits)
        XCTAssertTrue(RowLayout.showsNext(width: width, compact: true, template: eightDigits))
        XCTAssertFalse(RowLayout.showsNext(width: width, compact: false, template: eightDigits))
    }

    func testEightDigitVaultsDropTheNextColumnBeforeSixDigitVaults() {
        let width = RowLayout.minimumWidth(compact: false, template: sixDigits)
        XCTAssertTrue(RowLayout.showsNext(width: width, compact: false, template: sixDigits))
        XCTAssertFalse(RowLayout.showsNext(width: width, compact: false, template: eightDigits))
    }

    func testLargerTextNeedsAWiderRowForTheNextColumn() {
        let width = RowLayout.minimumWidth(compact: false, template: sixDigits)
        XCTAssertFalse(RowLayout.showsNext(width: width, compact: false, template: sixDigits, scale: 1.2))
        XCTAssertTrue(RowLayout.showsNext(width: width * 1.2, compact: false, template: sixDigits, scale: 1.2))
    }

    func testUnmeasuredWidthsHideTheNextColumn() {
        for width in [CGFloat.nan, .infinity, 0, -1] {
            XCTAssertFalse(RowLayout.showsNext(width: width, compact: false, template: sixDigits))
        }
    }

    @MainActor
    private func hostRow(compact: Bool, digits: Int, width: CGFloat) -> (NSWindow, NSHostingView<AccountRowView>) {
        let row = account("A", digits: digits)
        let view = NSHostingView(rootView: AccountRowView(account: row, remaining: 20,
                                                          code: String(repeating: "1", count: digits),
                                                          copied: false, compact: compact, reduceMotion: true,
                                                          nextTemplate: RowLayout.nextTemplate(for: [row]),
                                                          onClick: { _ in }))
        let window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: width, height: 120),
                              styleMask: [.borderless], backing: .buffered, defer: false)
        window.isReleasedWhenClosed = false
        window.contentView = view
        window.orderFrontRegardless()
        view.layoutSubtreeIfNeeded()
        return (window, view)
    }

    @MainActor
    private func settledFittingWidth(of view: NSView) async -> CGFloat {
        var last = view.fittingSize.width
        var steady = 0
        for _ in 0..<40 where steady < 3 {
            try? await Task.sleep(for: .milliseconds(25))
            view.layoutSubtreeIfNeeded()
            let now = view.fittingSize.width
            steady = now == last ? steady + 1 : 0
            last = now
        }
        return last
    }

    @MainActor
    func testMinimumWidthCoversTheRenderedRowWithItsNextColumn() async {
        for compact in [false, true] {
            for digits in [6, 8] {
                let (window, view) = hostRow(compact: compact, digits: digits, width: 900)
                defer { window.close() }
                let rendered = await settledFittingWidth(of: view)
                let minimum = RowLayout.minimumWidth(compact: compact, template: RowLayout.nextTemplate(for: [account("A", digits: digits)]))
                XCTAssertLessThanOrEqual(rendered, minimum, "compact \(compact), \(digits) digits")
                XCTAssertGreaterThan(rendered, minimum - 6, "compact \(compact), \(digits) digits")
            }
        }
    }

    @MainActor
    func testNextColumnFollowsTheOfferedWidthNotTheContentWidth() async {
        for compact in [false, true] {
            let template = RowLayout.nextTemplate(for: [account("A")])
            let minimum = RowLayout.minimumWidth(compact: compact, template: template)
            let (window, view) = hostRow(compact: compact, digits: 6, width: minimum - 20)
            defer { window.close() }
            let narrow = await settledFittingWidth(of: view)
            XCTAssertLessThan(narrow, minimum - 40, "compact \(compact)")

            window.setContentSize(NSSize(width: minimum, height: 120))
            view.layoutSubtreeIfNeeded()
            let wide = await settledFittingWidth(of: view)
            XCTAssertGreaterThan(wide, minimum - 6, "compact \(compact)")
        }
    }

    func testTemplateFitsTheLongestCodeInTheVault() {
        let accounts = [account("a"), account("b", digits: 8), account("c", type: .steam)]
        XCTAssertEqual(RowLayout.nextTemplate(for: accounts), "00000000")
    }

    func testTemplateForSixDigitCodesKeepsTheGroupingSpace() {
        XCTAssertEqual(RowLayout.nextTemplate(for: [account("a"), account("s", type: .steam)]), "000 000")
    }

    func testSteamCodesCountFiveCharactersWhateverTheirDigits() {
        XCTAssertEqual(RowLayout.codeLength(of: account("s", type: .steam, digits: 8)), 5)
        XCTAssertEqual(RowLayout.nextTemplate(for: [account("s", type: .steam)]), "00000")
    }

    func testEmptyVaultUsesTheDefaultSixDigitTemplate() {
        XCTAssertEqual(RowLayout.nextTemplate(for: []), "000 000")
    }
}
