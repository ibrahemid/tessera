import AppKit
import SwiftUI
import TesseraCore

struct QuickAccessList: Equatable {
    var query = "" {
        didSet { if query != oldValue { cursor = nil } }
    }
    private(set) var cursor: String?

    func visible(_ accounts: [Account]) -> [Account] {
        VaultList.visible(accounts, scope: .all, query: query)
    }

    func target(in order: [String]) -> String? {
        if let cursor, order.contains(cursor) { return cursor }
        return query.isEmpty ? nil : order.first
    }

    func isHighlighted(_ id: String, in order: [String]) -> Bool {
        target(in: order) == id
    }

    mutating func move(by delta: Int, in order: [String]) {
        guard !order.isEmpty, delta != 0 else { return }
        if let current = target(in: order), let i = order.firstIndex(of: current) {
            cursor = order[max(0, min(order.count - 1, i + delta))]
        } else {
            cursor = delta > 0 ? order[0] : order[order.count - 1]
        }
    }

    mutating func point(at id: String) { cursor = id }

    static func moveDelta(key: KeyEquivalent, modifiers: EventModifiers) -> Int? {
        guard modifiers.isDisjoint(with: [.command, .control, .option, .shift]) else { return nil }
        switch key {
        case .downArrow: return 1
        case .upArrow: return -1
        default: return nil
        }
    }
}

// The main ticker stops when no window is visible, and an open popover does not count as one.
@MainActor
final class QuickAccessClock: ObservableObject {
    @Published private(set) var now = Date()
    private var timer: Timer?

    var isRunning: Bool { timer != nil }

    func start() {
        #if DEBUG
        if isFrozen { return }
        #endif
        guard timer == nil else { return }
        now = Date()
        let t = Timer(fire: TickSchedule.nextFire(after: now), interval: 1, repeats: true) { [weak self] _ in
            Task { @MainActor in self?.now = Date() }
        }
        t.tolerance = 0.03
        RunLoop.main.add(t, forMode: .common)
        timer = t
    }

    func stop() {
        timer?.invalidate()
        timer = nil
    }

    @discardableResult
    func refresh() -> Date {
        #if DEBUG
        if isFrozen { return now }
        #endif
        now = Date()
        return now
    }

    #if DEBUG
    private var isFrozen = false

    func freeze(at date: Date) {
        stop()
        isFrozen = true
        now = date
    }
    #endif
}

enum QuickAccessLockState: Equatable {
    case open
    case locked(touchID: Bool)
    case needsWindow

    @MainActor
    init(_ model: AppModel) {
        if model.needsReset || model.needsRecovery || model.vaultUnreachable || model.needsPassphrase {
            self = .needsWindow
        } else if model.isLocked || model.isOpening {
            self = .locked(touchID: model.requireBiometrics)
        } else {
            self = .open
        }
    }
}

extension AppModel {
    @discardableResult
    func copyFromQuickAccess(_ account: Account, at time: Date, to pasteboard: NSPasteboard = .general) -> Bool {
        guard !isLocked else { return false }
        if account.type == .hotp {
            let before = accounts.first { $0.id == account.id }?.counter
            advanceHOTP(account)
            return accounts.first { $0.id == account.id }?.counter != before
        }
        return copyCode(account, at: time, to: pasteboard) != nil
    }
}
