import AppKit
import Foundation
import SwiftUI

/// Holds notification observer tokens and unregisters them when its owner is
/// released. Kept as a separate object so the owner needs no `deinit` (which
/// cannot touch main-actor state under Swift 6 isolation).
final class NotificationObservers: @unchecked Sendable {
    private var entries: [(NotificationCenter, NSObjectProtocol)] = []

    func add(_ center: NotificationCenter, _ token: NSObjectProtocol) {
        entries.append((center, token))
    }

    deinit {
        for (center, token) in entries { center.removeObserver(token) }
    }
}

/// The 1 Hz clock behind countdown rings and rotating codes.
///
/// Deliberately its own observable object: only the views that render a
/// countdown observe it, so a tick never re-renders the window chrome, the
/// sidebar, the settings form, or an open sheet. The timer runs in the
/// `.common` run-loop mode (menus and scrolling would otherwise freeze the
/// rings) with a 0.1s tolerance, and it stops entirely while the vault is
/// locked or the app has no visible, unoccluded window.
@MainActor
final class Ticker: ObservableObject {
    @Published private(set) var now = Date()

    private var timer: Timer?
    private var isEnabled = false
    private let observers = NotificationObservers()

    init() {
        let app = NotificationCenter.default
        for name: Notification.Name in [NSApplication.didChangeOcclusionStateNotification,
                                        NSApplication.didBecomeActiveNotification,
                                        NSWindow.didBecomeKeyNotification,
                                        NSWindow.didDeminiaturizeNotification,
                                        NSWindow.didMiniaturizeNotification,
                                        NSWindow.willCloseNotification] {
            observers.add(app, app.addObserver(forName: name, object: nil, queue: .main) { [weak self] _ in
                // willClose fires before the window leaves the list; recheck on
                // the next hop so the count is accurate.
                DispatchQueue.main.async { Task { @MainActor in self?.sync() } }
            })
        }
    }

    /// Ticking is on only while there is something to render: the caller passes
    /// the unlocked state, visibility is tracked here.
    func setEnabled(_ on: Bool) {
        guard isEnabled != on else { return }
        isEnabled = on
        sync()
    }

    /// Whether the app currently has a window worth animating.
    private var hasVisibleWindow: Bool {
        guard let app = NSApp else { return true }
        guard app.occlusionState.contains(.visible) else { return false }
        return app.windows.contains { $0.isVisible && !$0.isMiniaturized }
    }

    private func sync() {
        (isEnabled && hasVisibleWindow) ? start() : stop()
    }

    private func start() {
        guard timer == nil else { return }
        now = Date()
        let t = Timer(timeInterval: 1, repeats: true) { [weak self] _ in
            Task { @MainActor in self?.now = Date() }
        }
        t.tolerance = 0.1
        RunLoop.main.add(t, forMode: .common)
        timer = t
    }

    private func stop() {
        timer?.invalidate()
        timer = nil
    }
}
