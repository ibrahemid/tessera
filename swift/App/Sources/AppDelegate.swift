import AppKit

final class AppDelegate: NSObject, NSApplicationDelegate {
    /// Scene id of the main window (see TesseraApp), used to reopen the right one.
    static let mainWindowID = "main"

    func applicationDidFinishLaunching(_ notification: Notification) {
        if SelfTest.runIfRequested() { return }
        if SelfTestSE.runIfRequested() { return }
        #if DEBUG
        if MarketingShot.runIfRequested() {
            DispatchQueue.main.asyncAfter(deadline: .now() + 0.3) { NSApp.terminate(nil) }
            return
        }
        if Screenshots.isRequested {
            Task { @MainActor in
                await Screenshots.run()
                NSApp.terminate(nil)
            }
        }
        #endif
    }

    func applicationShouldHandleReopen(_ sender: NSApplication, hasVisibleWindows flag: Bool) -> Bool {
        guard !flag else { return true }
        // Target the main scene by id: "first window that can become key" can be
        // the Settings window, which would reopen to the wrong surface.
        let window = sender.windows.first { $0.identifier?.rawValue == Self.mainWindowID }
            ?? sender.windows.first { $0.canBecomeKey }
        window?.makeKeyAndOrderFront(nil)
        return true
    }
}
