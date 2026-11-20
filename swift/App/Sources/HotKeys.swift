import AppKit
import Carbon.HIToolbox

struct HotKeyToken: Hashable, Sendable {
    let id: UInt32
}

enum HotKeyError: Error, Equatable {
    case taken
    case failed(OSStatus)
}

@MainActor
protocol HotKeyRegistering: AnyObject {
    func register(_ shortcut: GlobalShortcut, action: @escaping @MainActor () -> Void) throws -> HotKeyToken
    func unregister(_ token: HotKeyToken)
}

@MainActor
protocol ShortcutConflictChecking {
    func conflict(for shortcut: GlobalShortcut) -> ShortcutIssue?
}

// Carbon hot keys need no accessibility permission in the sandbox, unlike an event tap.
@MainActor
final class CarbonHotKeyRegistrar: HotKeyRegistering {
    static let shared = CarbonHotKeyRegistrar()

    private static let signature: OSType = 0x5445_5353
    private var handler: EventHandlerRef?
    private var entries: [UInt32: (ref: EventHotKeyRef, action: @MainActor () -> Void)] = [:]
    private var nextID: UInt32 = 1

    private init() {}

    func register(_ shortcut: GlobalShortcut, action: @escaping @MainActor () -> Void) throws -> HotKeyToken {
        try installHandler()
        let id = nextID
        nextID += 1
        var ref: EventHotKeyRef?
        let status = RegisterEventHotKey(shortcut.keyCode, shortcut.modifiers,
                                         EventHotKeyID(signature: Self.signature, id: id),
                                         GetApplicationEventTarget(), OptionBits(kEventHotKeyExclusive), &ref)
        if status == OSStatus(eventHotKeyExistsErr) { throw HotKeyError.taken }
        guard status == noErr, let ref else { throw HotKeyError.failed(status) }
        entries[id] = (ref, action)
        return HotKeyToken(id: id)
    }

    func unregister(_ token: HotKeyToken) {
        guard let entry = entries.removeValue(forKey: token.id) else { return }
        UnregisterEventHotKey(entry.ref)
    }

    fileprivate func fire(_ id: UInt32) {
        entries[id]?.action()
    }

    private func installHandler() throws {
        guard handler == nil else { return }
        var spec = EventTypeSpec(eventClass: OSType(kEventClassKeyboard), eventKind: UInt32(kEventHotKeyPressed))
        let status = InstallEventHandler(GetApplicationEventTarget(), hotKeyCallback, 1, &spec,
                                         Unmanaged.passUnretained(self).toOpaque(), &handler)
        guard status == noErr else { throw HotKeyError.failed(status) }
    }
}

private func hotKeyCallback(_: EventHandlerCallRef?, _ event: EventRef?, _ userData: UnsafeMutableRawPointer?) -> OSStatus {
    guard let event, let userData else { return OSStatus(eventNotHandledErr) }
    var hotKey = EventHotKeyID()
    let status = GetEventParameter(event, EventParamName(kEventParamDirectObject), EventParamType(typeEventHotKeyID),
                                   nil, MemoryLayout<EventHotKeyID>.size, nil, &hotKey)
    guard status == noErr else { return status }
    let id = hotKey.id
    let registrar = Unmanaged<CarbonHotKeyRegistrar>.fromOpaque(userData).takeUnretainedValue()
    MainActor.assumeIsolated { registrar.fire(id) }
    return noErr
}

@MainActor
struct SystemShortcutConflicts: ShortcutConflictChecking {
    func conflict(for shortcut: GlobalShortcut) -> ShortcutIssue? {
        if Self.isReservedBySystem(shortcut) { return .reservedBySystem(shortcut.display) }
        if let item = Self.menuItem(using: shortcut, in: NSApp?.mainMenu) {
            return .usedByMenu(shortcut.display, item: item)
        }
        return nil
    }

    static func isReservedBySystem(_ shortcut: GlobalShortcut) -> Bool {
        var unmanaged: Unmanaged<CFArray>?
        guard CopySymbolicHotKeys(&unmanaged) == noErr,
              let entries = unmanaged?.takeRetainedValue() as? [[String: Any]] else { return false }
        return entries.contains { entry in
            guard (entry[kHISymbolicHotKeyEnabled as String] as? Bool) == true,
                  let code = (entry[kHISymbolicHotKeyCode as String] as? NSNumber)?.uint32Value,
                  let mods = (entry[kHISymbolicHotKeyModifiers as String] as? NSNumber)?.uint32Value else { return false }
            return code == shortcut.keyCode && mods & GlobalShortcut.carbonMask == shortcut.modifiers
        }
    }

    static func menuItem(using shortcut: GlobalShortcut, in menu: NSMenu?) -> String? {
        guard let menu, let character = shortcut.menuCharacter else { return nil }
        for item in menu.items {
            if let title = menuItem(using: shortcut, in: item.submenu) { return title }
            guard !item.keyEquivalent.isEmpty else { continue }
            var flags = item.keyEquivalentModifierMask.intersection([.control, .option, .shift, .command])
            var equivalent = item.keyEquivalent
            if equivalent.lowercased() != equivalent {
                flags.insert(.shift)
                equivalent = equivalent.lowercased()
            }
            if equivalent == character && flags == shortcut.flags {
                return item.title.trimmingCharacters(in: CharacterSet(charactersIn: "…. "))
            }
        }
        return nil
    }
}
