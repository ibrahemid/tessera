import AppKit
import SwiftUI
import XCTest
@testable import Tessera

@MainActor
private final class SelectionProbe {
    var ids: [String] = []
}

private struct SelectionSpy: View {
    let probe: SelectionProbe
    @FocusedValue(\.vaultMenu) private var menu

    var body: some View {
        Color.clear
            .allowsHitTesting(false)
            .onChange(of: menu?.selected.map(\.id) ?? [], initial: true) { _, ids in probe.ids = ids }
    }
}

@MainActor
final class VaultKeyboardTests: XCTestCase {
    @MainActor private struct Harness {
        let window: NSWindow
        let probe: SelectionProbe
        let suite: String

        var searchText: String? { (window.firstResponder as? NSTextView)?.string }
        var selected: Set<String> { Set(probe.ids) }

        func close() {
            window.close()
            UserDefaults().removePersistentDomain(forName: suite)
        }
    }

    private static let down = String(UnicodeScalar(UInt16(NSDownArrowFunctionKey))!)
    private static let up = String(UnicodeScalar(UInt16(NSUpArrowFunctionKey))!)

    private func makeHarness() async throws -> Harness {
        let suite = "tessera.tests.\(UUID().uuidString)"
        let defaults = try XCTUnwrap(UserDefaults(suiteName: suite))
        let probe = SelectionProbe()
        let window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 920, height: 620),
                              styleMask: [.titled, .resizable], backing: .buffered, defer: false)
        window.isReleasedWhenClosed = false
        window.contentView = NSHostingView(rootView: VaultView()
            .overlay(SelectionSpy(probe: probe))
            .environmentObject(AppModel(demo: .populated))
            .defaultAppStorage(defaults))
        window.orderFrontRegardless()
        window.makeKey()
        let harness = Harness(window: window, probe: probe, suite: suite)
        await waitUntil("search field focus") { harness.window.firstResponder is NSTextView }
        return harness
    }

    private func press(_ harness: Harness, _ characters: String, keyCode: UInt16,
                       modifiers: NSEvent.ModifierFlags = []) {
        for type in [NSEvent.EventType.keyDown, .keyUp] {
            guard let event = NSEvent.keyEvent(with: type, location: .zero, modifierFlags: modifiers,
                                               timestamp: ProcessInfo.processInfo.systemUptime,
                                               windowNumber: harness.window.windowNumber, context: nil,
                                               characters: characters, charactersIgnoringModifiers: characters,
                                               isARepeat: false, keyCode: keyCode)
            else { return XCTFail("could not build a key event for keyCode \(keyCode)") }
            harness.window.sendEvent(event)
        }
    }

    private func arrow(_ harness: Harness, down isDown: Bool, shift: Bool = false) {
        var flags: NSEvent.ModifierFlags = [.numericPad, .function]
        if shift { flags.insert(.shift) }
        press(harness, isDown ? Self.down : Self.up, keyCode: isDown ? 125 : 126, modifiers: flags)
    }

    private func waitUntil(_ what: String, timeout: Duration = .seconds(3), _ condition: () -> Bool) async {
        let clock = ContinuousClock()
        let deadline = clock.now + timeout
        while !condition() {
            guard clock.now < deadline else { return XCTFail("timed out waiting for \(what)") }
            try? await Task.sleep(for: .milliseconds(20))
        }
    }

    private func outline(in view: NSView) -> NSOutlineView? {
        if let found = view as? NSOutlineView { return found }
        return view.subviews.lazy.compactMap { self.outline(in: $0) }.first
    }

    func testArrowKeysMoveTheSelectionWhileSearchHasFocus() async throws {
        let h = try await makeHarness()
        defer { h.close() }

        arrow(h, down: true)
        await waitUntil("first row selected") { h.selected == ["1"] }
        arrow(h, down: true)
        await waitUntil("second row selected") { h.selected == ["2"] }
        arrow(h, down: false)
        await waitUntil("selection moved back up") { h.selected == ["1"] }
        arrow(h, down: true, shift: true)
        await waitUntil("selection extended") { h.selected == ["1", "2"] }
        XCTAssertTrue(h.window.firstResponder is NSTextView, "arrows took focus out of search")

        press(h, "\u{1B}", keyCode: 53)
        await waitUntil("Escape cleared the selection") { h.selected.isEmpty }
    }

    func testTypingOutsideSearchFiltersAndEscapeClearsSearchThenSelection() async throws {
        let h = try await makeHarness()
        defer { h.close() }
        let sidebar = try XCTUnwrap(h.window.contentView.flatMap(outline(in:)), "sidebar list missing")
        XCTAssertTrue(h.window.makeFirstResponder(sidebar))
        try await Task.sleep(for: .milliseconds(300))

        press(h, "c", keyCode: 8)
        press(h, "l", keyCode: 37)
        await waitUntil("typed text reached search") { h.searchText == "cl" }
        await waitUntil("first match selected") { h.selected == ["2"] }

        press(h, "\u{1B}", keyCode: 53)
        await waitUntil("Escape cleared the search") { h.searchText == "" }
        XCTAssertEqual(h.selected, ["2"], "first Escape cleared the selection")

        press(h, "\u{1B}", keyCode: 53)
        await waitUntil("second Escape cleared the selection") { h.selected.isEmpty }
    }
}
