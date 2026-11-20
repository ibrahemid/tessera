import AppKit
import SwiftUI
import XCTest
@testable import Tessera

@MainActor
final class InterfaceNoticeInsetTests: XCTestCase {
    private func scrollViews(in view: NSView) -> [NSScrollView] {
        var found: [NSScrollView] = []
        if let scroll = view as? NSScrollView { found.append(scroll) }
        for sub in view.subviews { found += scrollViews(in: sub) }
        return found
    }

    private func waitUntil(_ what: String, timeout: Duration = .seconds(3), _ condition: () -> Bool) async {
        let clock = ContinuousClock()
        let deadline = clock.now + timeout
        while !condition() {
            guard clock.now < deadline else { return XCTFail("timed out waiting for \(what)") }
            try? await Task.sleep(for: .milliseconds(20))
        }
    }

    func testAccountListScrollsClearOfTheNoticeWhileItShows() async throws {
        let suite = "tessera.tests.\(UUID().uuidString)"
        let defaults = try XCTUnwrap(UserDefaults(suiteName: suite))
        let model = AppModel(demo: .populated)
        let window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 920, height: 620),
                              styleMask: [.titled, .resizable], backing: .buffered, defer: false)
        window.isReleasedWhenClosed = false
        window.contentView = NSHostingView(rootView: VaultView()
            .environmentObject(model)
            .defaultAppStorage(defaults))
        window.orderFrontRegardless()
        defer {
            window.close()
            UserDefaults().removePersistentDomain(forName: suite)
        }

        var list: NSScrollView?
        await waitUntil("account list") {
            list = window.contentView.flatMap { scrollViews(in: $0).max { $0.frame.width < $1.frame.width } }
            return list != nil
        }
        let scroll = try XCTUnwrap(list)
        XCTAssertEqual(scroll.contentInsets.bottom, 0)

        model.notice = .failure("The file couldn’t be saved because there isn’t enough space.")
        let minimum = NoticeLayout.bottomInset(noticeHeight: 1, bulkBarVisible: false)
        await waitUntil("bottom inset for the notice") { scroll.contentInsets.bottom > minimum }

        model.notice = nil
        await waitUntil("bottom inset removed with the notice") { scroll.contentInsets.bottom == 0 }
    }
}
