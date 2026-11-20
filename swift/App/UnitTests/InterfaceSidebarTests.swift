import AppKit
import SwiftUI
import XCTest
@testable import Tessera

@MainActor
final class InterfaceSidebarTests: XCTestCase {
    private let sidebarIdealWidth: CGFloat = 210
    private let toggleIdentifier = "toggleSidebar"

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

    private func waitUntilSteady(_ what: String, holding hold: Duration = .milliseconds(500),
                                 timeout: Duration = .seconds(4), _ condition: () -> Bool) async {
        let clock = ContinuousClock()
        let deadline = clock.now + timeout
        var heldSince: ContinuousClock.Instant?
        while true {
            let now = clock.now
            if condition() {
                if let since = heldSince, now - since >= hold { return }
                if heldSince == nil { heldSince = now }
            } else {
                heldSince = nil
            }
            guard now < deadline else { return XCTFail("timed out waiting for \(what) to hold") }
            try? await Task.sleep(for: .milliseconds(20))
        }
    }

    private func hostVault(compact: Bool) throws -> (NSWindow, UserDefaults, String) {
        let suite = "tessera.tests.\(UUID().uuidString)"
        let defaults = try XCTUnwrap(UserDefaults(suiteName: suite))
        defaults.set(compact, forKey: AppModel.compactPrefKey)
        let window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 920, height: 620),
                              styleMask: [.titled, .closable, .resizable, .fullSizeContentView],
                              backing: .buffered, defer: false)
        window.isReleasedWhenClosed = false
        window.contentView = NSHostingView(rootView: VaultView()
            .environmentObject(AppModel(demo: .populated))
            .defaultAppStorage(defaults))
        window.orderFrontRegardless()
        return (window, defaults, suite)
    }

    private func columns(in window: NSWindow) -> (sidebar: CGRect, detail: CGRect)? {
        let frames = (window.contentView.map { scrollViews(in: $0) } ?? []).map { $0.convert($0.bounds, to: nil) }
        guard frames.count >= 2,
              let sidebar = frames.min(by: { $0.width < $1.width }),
              let detail = frames.max(by: { $0.width < $1.width }) else { return nil }
        return (sidebar, detail)
    }

    private func showsSidebarAtItsIdealWidth(_ window: NSWindow) -> Bool {
        guard let c = columns(in: window) else { return false }
        return c.sidebar.minX == 0 && c.sidebar.width == sidebarIdealWidth && c.detail.minX >= c.sidebar.maxX
    }

    private func showsDetailAcrossTheWindow(_ window: NSWindow) -> Bool {
        columns(in: window)?.detail.minX == 0
    }

    private func hasSidebarToggle(_ window: NSWindow) -> Bool {
        window.toolbar?.items.contains { $0.itemIdentifier.rawValue.contains(toggleIdentifier) } ?? false
    }

    func testWindowLayoutShowsTheSidebarAtItsIdealWidthWithItsToggle() async throws {
        let (window, _, suite) = try hostVault(compact: false)
        defer {
            window.close()
            UserDefaults().removePersistentDomain(forName: suite)
        }
        await waitUntilSteady("sidebar at its ideal width") { showsSidebarAtItsIdealWidth(window) }
        XCTAssertTrue(hasSidebarToggle(window))
    }

    func testCompactLayoutHasNoSidebarToggle() async throws {
        let (window, _, suite) = try hostVault(compact: true)
        defer {
            window.close()
            UserDefaults().removePersistentDomain(forName: suite)
        }
        await waitUntil("toolbar items") { (window.toolbar?.items.count ?? 0) > 0 }
        await waitUntil("detail column across the window") { showsDetailAcrossTheWindow(window) }
        XCTAssertFalse(hasSidebarToggle(window))
    }

    func testLeavingCompactRightAfterLaunchShowsTheSidebarAtItsIdealWidthWithItsToggle() async throws {
        let (window, defaults, suite) = try hostVault(compact: true)
        defer {
            window.close()
            UserDefaults().removePersistentDomain(forName: suite)
        }
        await waitUntil("toolbar items") { (window.toolbar?.items.count ?? 0) > 0 }
        await waitUntil("detail column across the window") { showsDetailAcrossTheWindow(window) }

        defaults.set(false, forKey: AppModel.compactPrefKey)
        await waitUntilSteady("sidebar at its ideal width beside the detail") { showsSidebarAtItsIdealWidth(window) }
        await waitUntil("sidebar toggle back in the toolbar") { hasSidebarToggle(window) }
    }

    func testCompactRoundTripRestoresTheSidebarAndItsToggle() async throws {
        let (window, defaults, suite) = try hostVault(compact: false)
        defer {
            window.close()
            UserDefaults().removePersistentDomain(forName: suite)
        }
        await waitUntilSteady("sidebar at its ideal width") { showsSidebarAtItsIdealWidth(window) }

        defaults.set(true, forKey: AppModel.compactPrefKey)
        await waitUntilSteady("detail column across the window") { showsDetailAcrossTheWindow(window) }
        await waitUntil("sidebar toggle removed") { !hasSidebarToggle(window) }

        defaults.set(false, forKey: AppModel.compactPrefKey)
        await waitUntilSteady("sidebar at its ideal width beside the detail") { showsSidebarAtItsIdealWidth(window) }
        await waitUntil("sidebar toggle back in the toolbar") { hasSidebarToggle(window) }
    }
}
