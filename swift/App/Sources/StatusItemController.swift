import AppKit
import Combine
import SwiftUI

@MainActor
final class StatusItemController: NSObject, NSPopoverDelegate {
    private let model: AppModel
    private let settings: QuickAccessSettings
    private let clock = QuickAccessClock()
    private let popover = NSPopover()
    private let showMainWindow: @MainActor () -> Void
    private var item: NSStatusItem?
    private var isTemporary = false
    private var subscription: AnyCancellable?

    init(model: AppModel, settings: QuickAccessSettings, showMainWindow: @escaping @MainActor () -> Void) {
        self.model = model
        self.settings = settings
        self.showMainWindow = showMainWindow
        super.init()
        popover.behavior = .transient
        popover.animates = !NSWorkspace.shared.accessibilityDisplayShouldReduceMotion
        popover.delegate = self
    }

    var isShown: Bool { popover.isShown }
    var hasItem: Bool { item != nil }

    func install() {
        subscription = settings.$showsInMenuBar.sink { [weak self] shows in self?.setPinned(shows) }
    }

    func toggleFromShortcut() {
        if popover.isShown {
            popover.performClose(nil)
            return
        }
        if item == nil {
            addItem()
            isTemporary = true
        }
        show()
    }

    private func setPinned(_ shows: Bool) {
        if shows {
            addItem()
            isTemporary = false
        } else if popover.isShown {
            isTemporary = true
        } else {
            removeItem()
        }
    }

    private func addItem() {
        guard item == nil else { return }
        let statusItem = NSStatusBar.system.statusItem(withLength: NSStatusItem.squareLength)
        if let button = statusItem.button {
            button.image = Self.icon
            button.setAccessibilityLabel("Tessera")
            button.toolTip = "Tessera"
            button.target = self
            button.action = #selector(togglePopover(_:))
        }
        item = statusItem
    }

    private func removeItem() {
        guard let item else { return }
        NSStatusBar.system.removeStatusItem(item)
        self.item = nil
        isTemporary = false
    }

    @objc private func togglePopover(_ sender: Any?) {
        if popover.isShown { popover.performClose(sender) } else { show() }
    }

    private func show() {
        guard let button = item?.button else { return }
        clock.start()
        let content = QuickAccessView(clock: clock,
                                      onClose: { [weak self] in self?.popover.performClose(nil) },
                                      onOpenApp: { [weak self] in
                                          self?.popover.performClose(nil)
                                          self?.showMainWindow()
                                      })
            .environmentObject(model)
            .preferredColorScheme(AppTheme(rawValue: UserDefaults.standard.string(forKey: "tessera.theme") ?? "")?.colorScheme)
        popover.contentViewController = NSHostingController(rootView: content)
        NSApp.activate()
        popover.show(relativeTo: button.bounds, of: button, preferredEdge: .minY)
        popover.contentViewController?.view.window?.makeKey()
    }

    func popoverDidClose(_ notification: Notification) {
        clock.stop()
        popover.contentViewController = nil
        if isTemporary { removeItem() }
    }

    private static let icon: NSImage = {
        let image = NSImage(size: NSSize(width: 16, height: 16), flipped: false, drawingHandler: drawIcon)
        image.isTemplate = true
        image.accessibilityDescription = "Tessera"
        return image
    }()

    nonisolated private static func drawIcon(_ rect: NSRect) -> Bool {
        let inset: CGFloat = 1.5
        let gap: CGFloat = 1.6
        let cell = (rect.width - inset * 2 - gap) / 2
        let alphas: [CGFloat] = [1, 0.55, 0.55, 0.3]
        for (i, alpha) in alphas.enumerated() {
            let col = CGFloat(i % 2)
            let row = CGFloat(1 - i / 2)
            let tile = NSRect(x: inset + col * (cell + gap), y: inset + row * (cell + gap), width: cell, height: cell)
            NSColor.black.withAlphaComponent(alpha).setFill()
            NSBezierPath(roundedRect: tile, xRadius: cell * 0.28, yRadius: cell * 0.28).fill()
        }
        return true
    }
}
