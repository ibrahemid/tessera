import AppKit
import Foundation

@MainActor
final class QuickAccessSettings: ObservableObject {
    static let menuBarKey = "tessera.menuBar"
    static let shortcutKey = "tessera.globalShortcut"

    @Published private(set) var showsInMenuBar: Bool
    @Published private(set) var shortcut: GlobalShortcut?
    @Published private(set) var issue: ShortcutIssue?
    @Published private(set) var isRecording = false

    private let defaults: UserDefaults
    private let registrar: HotKeyRegistering
    private let conflicts: ShortcutConflictChecking
    private var token: HotKeyToken?
    private var action: (@MainActor () -> Void)?

    init(defaults: UserDefaults = .standard,
         registrar: HotKeyRegistering? = nil,
         conflicts: ShortcutConflictChecking = SystemShortcutConflicts()) {
        self.defaults = defaults
        self.registrar = registrar ?? CarbonHotKeyRegistrar.shared
        self.conflicts = conflicts
        showsInMenuBar = defaults.object(forKey: Self.menuBarKey) as? Bool ?? true
        shortcut = defaults.data(forKey: Self.shortcutKey)
            .flatMap { try? JSONDecoder().decode(GlobalShortcut.self, from: $0) }
            .flatMap { $0.isValid ? $0 : nil }
    }

    var isRegistered: Bool { token != nil }

    func setShowsInMenuBar(_ on: Bool) {
        showsInMenuBar = on
        defaults.set(on, forKey: Self.menuBarKey)
    }

    func activate(action: @escaping @MainActor () -> Void) {
        self.action = action
        registerSaved()
    }

    func deactivate() {
        unregister()
        action = nil
    }

    func beginRecording() {
        guard !isRecording else { return }
        isRecording = true
        issue = nil
        unregister()
    }

    func cancelRecording() {
        guard isRecording else { return }
        isRecording = false
        registerSaved()
    }

    @discardableResult
    func handle(_ key: RecordedKey) -> Bool {
        guard isRecording else { return false }
        if key.cancelsRecording {
            cancelRecording()
            return true
        }
        let candidate = key.shortcut
        guard !candidate.key.isEmpty else { return true }
        guard candidate.hasRequiredModifier else {
            issue = .needsModifier
            return true
        }
        record(candidate)
        return true
    }

    func record(_ candidate: GlobalShortcut) {
        isRecording = false
        if let problem = candidate.hasRequiredModifier ? conflicts.conflict(for: candidate) : .needsModifier {
            registerSaved()
            issue = problem
            return
        }
        if action != nil {
            unregister()
            do {
                try register(candidate)
            } catch {
                registerSaved()
                issue = Self.issue(for: error, shortcut: candidate)
                return
            }
        }
        issue = nil
        shortcut = candidate
        if let data = try? JSONEncoder().encode(candidate) { defaults.set(data, forKey: Self.shortcutKey) }
    }

    func clearShortcut() {
        isRecording = false
        unregister()
        shortcut = nil
        issue = nil
        defaults.removeObject(forKey: Self.shortcutKey)
    }

    private func registerSaved() {
        guard token == nil, !isRecording, action != nil, let saved = shortcut else { return }
        do {
            try register(saved)
        } catch {
            issue = Self.issue(for: error, shortcut: saved)
        }
    }

    private func register(_ shortcut: GlobalShortcut) throws {
        token = try registrar.register(shortcut, action: { [weak self] in self?.action?() })
    }

    private func unregister() {
        if let token { registrar.unregister(token) }
        token = nil
    }

    private static func issue(for error: Error, shortcut: GlobalShortcut) -> ShortcutIssue {
        if let e = error as? HotKeyError, e == .taken { return .taken(shortcut.display) }
        return .failed(shortcut.display)
    }

    #if DEBUG
    func preview(showsInMenuBar: Bool = true, shortcut: GlobalShortcut? = nil,
                 issue: ShortcutIssue? = nil, recording: Bool = false) {
        self.showsInMenuBar = showsInMenuBar
        self.shortcut = shortcut
        self.issue = issue
        self.isRecording = recording
    }
    #endif
}
