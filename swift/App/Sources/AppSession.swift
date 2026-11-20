import AppKit
import SwiftUI

@MainActor
final class AppSession {
    private(set) static var shared: AppSession?

    let model: AppModel
    var openWindow: OpenWindowAction?
    private var statusItem: StatusItemController?
    private let links: LinkEventHandler
    private var observers: [NSObjectProtocol] = []

    static func start(model: AppModel) {
        guard shared == nil else { return }
        shared = AppSession(model: model)
    }

    static var isEnabled: Bool {
        let args = CommandLine.arguments
        if args.contains("--selftest") || args.contains("--selftest-se") { return false }
        #if DEBUG
        if Screenshots.isRequested || args.contains("--marketing") { return false }
        #endif
        return true
    }

    private init(model: AppModel) {
        self.model = model
        links = LinkEventHandler()
        links.receive = { [weak self] urls in self?.openLinks(urls) }
        let center = NotificationCenter.default
        observers.append(center.addObserver(forName: NSApplication.willFinishLaunchingNotification,
                                            object: nil, queue: .main) { [weak self] _ in
            MainActor.assumeIsolated { self?.links.install() }
        })
        observers.append(center.addObserver(forName: NSApplication.didFinishLaunchingNotification,
                                            object: nil, queue: .main) { [weak self] _ in
            MainActor.assumeIsolated { self?.finishLaunching() }
        })
    }

    private func finishLaunching() {
        let controller = StatusItemController(model: model, settings: model.quickAccess) { [weak self] in
            self?.showMainWindow()
        }
        controller.install()
        statusItem = controller
        model.quickAccess.activate { [weak controller] in controller?.toggleFromShortcut() }
    }

    private var mainWindow: NSWindow? {
        NSApp.windows.first { $0.identifier?.rawValue == AppDelegate.mainWindowID }
    }

    var isMainWindowVisible: Bool { mainWindow?.isVisible == true }

    func showMainWindow() {
        if let window = mainWindow {
            window.makeKeyAndOrderFront(nil)
        } else {
            openWindow?(id: AppDelegate.mainWindowID)
        }
        NSApp.activate()
    }

    func openLinks(_ urls: [URL]) {
        guard model.receiveLinks(urls) else { return }
        showMainWindow()
    }
}

@MainActor
final class LinkEventHandler: NSObject {
    var receive: (@MainActor ([URL]) -> Void)?

    func install() {
        NSAppleEventManager.shared().setEventHandler(self, andSelector: #selector(handle(_:reply:)),
                                                     forEventClass: AEEventClass(kInternetEventClass),
                                                     andEventID: AEEventID(kAEGetURL))
    }

    @objc func handle(_ event: NSAppleEventDescriptor, reply: NSAppleEventDescriptor) {
        guard let text = event.paramDescriptor(forKeyword: keyDirectObject)?.stringValue,
              let url = URL(string: text) else { return }
        receive?([url])
    }
}

struct MainWindowBridge: View {
    @Environment(\.openWindow) private var openWindow

    var body: some View {
        Color.clear
            .frame(width: 0, height: 0)
            .accessibilityHidden(true)
            .onAppear { AppSession.shared?.openWindow = openWindow }
    }
}
