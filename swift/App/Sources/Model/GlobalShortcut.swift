import AppKit
import Carbon.HIToolbox

struct GlobalShortcut: Codable, Equatable, Sendable {
    let keyCode: UInt32
    let modifiers: UInt32
    let key: String

    static let carbonMask = UInt32(controlKey | optionKey | shiftKey | cmdKey)

    init(keyCode: UInt32, modifiers: UInt32, key: String) {
        self.keyCode = keyCode
        self.modifiers = modifiers & Self.carbonMask
        self.key = Self.specialKeyNames[keyCode] ?? key.uppercased()
    }

    init(keyCode: UInt16, flags: NSEvent.ModifierFlags, characters: String?) {
        self.init(keyCode: UInt32(keyCode), modifiers: Self.carbonModifiers(flags), key: characters ?? "")
    }

    static func carbonModifiers(_ flags: NSEvent.ModifierFlags) -> UInt32 {
        var m: UInt32 = 0
        if flags.contains(.control) { m |= UInt32(controlKey) }
        if flags.contains(.option) { m |= UInt32(optionKey) }
        if flags.contains(.shift) { m |= UInt32(shiftKey) }
        if flags.contains(.command) { m |= UInt32(cmdKey) }
        return m
    }

    var flags: NSEvent.ModifierFlags {
        var f: NSEvent.ModifierFlags = []
        if modifiers & UInt32(controlKey) != 0 { f.insert(.control) }
        if modifiers & UInt32(optionKey) != 0 { f.insert(.option) }
        if modifiers & UInt32(shiftKey) != 0 { f.insert(.shift) }
        if modifiers & UInt32(cmdKey) != 0 { f.insert(.command) }
        return f
    }

    var hasRequiredModifier: Bool {
        modifiers & UInt32(controlKey | optionKey | cmdKey) != 0
    }

    var isValid: Bool { hasRequiredModifier && !key.isEmpty }

    var display: String {
        var s = ""
        if modifiers & UInt32(controlKey) != 0 { s += "⌃" }
        if modifiers & UInt32(optionKey) != 0 { s += "⌥" }
        if modifiers & UInt32(shiftKey) != 0 { s += "⇧" }
        if modifiers & UInt32(cmdKey) != 0 { s += "⌘" }
        return s + key
    }

    var accessibilityName: String {
        var parts: [String] = []
        if modifiers & UInt32(controlKey) != 0 { parts.append("Control") }
        if modifiers & UInt32(optionKey) != 0 { parts.append("Option") }
        if modifiers & UInt32(shiftKey) != 0 { parts.append("Shift") }
        if modifiers & UInt32(cmdKey) != 0 { parts.append("Command") }
        return (parts + [key]).joined(separator: "-")
    }

    var menuCharacter: String? {
        if keyCode == UInt32(kVK_Space) { return " " }
        guard Self.specialKeyNames[keyCode] == nil, key.count == 1 else { return nil }
        return key.lowercased()
    }

    static let specialKeyNames: [UInt32: String] = {
        var names: [Int: String] = [
            kVK_Space: "Space", kVK_Return: "↩", kVK_ANSI_KeypadEnter: "⌤", kVK_Tab: "⇥",
            kVK_Delete: "⌫", kVK_ForwardDelete: "⌦", kVK_Escape: "⎋",
            kVK_LeftArrow: "←", kVK_RightArrow: "→", kVK_UpArrow: "↑", kVK_DownArrow: "↓",
            kVK_Home: "↖", kVK_End: "↘", kVK_PageUp: "⇞", kVK_PageDown: "⇟",
        ]
        let functionKeys = [kVK_F1, kVK_F2, kVK_F3, kVK_F4, kVK_F5, kVK_F6, kVK_F7, kVK_F8, kVK_F9, kVK_F10,
                            kVK_F11, kVK_F12, kVK_F13, kVK_F14, kVK_F15, kVK_F16, kVK_F17, kVK_F18, kVK_F19, kVK_F20]
        for (i, code) in functionKeys.enumerated() { names[code] = "F\(i + 1)" }
        return Dictionary(uniqueKeysWithValues: names.map { (UInt32($0.key), $0.value) })
    }()
}

enum ShortcutIssue: Equatable, Sendable {
    case needsModifier
    case reservedBySystem(String)
    case usedByMenu(String, item: String)
    case taken(String)
    case failed(String)

    var text: String {
        switch self {
        case .needsModifier: return "Include ⌘, ⌥ or ⌃ in the shortcut."
        case .reservedBySystem(let s): return "macOS uses \(s). Choose another shortcut."
        case .usedByMenu(let s, let item): return "Tessera uses \(s) for \(item). Choose another shortcut."
        case .taken(let s): return "Another app uses \(s). Choose another shortcut."
        case .failed(let s): return "Couldn’t set \(s) as the shortcut. Choose another."
        }
    }
}

struct RecordedKey: Equatable, Sendable {
    let keyCode: UInt16
    let flags: NSEvent.ModifierFlags
    let characters: String?

    init(keyCode: UInt16, flags: NSEvent.ModifierFlags, characters: String?) {
        self.keyCode = keyCode
        self.flags = flags.intersection([.control, .option, .shift, .command])
        self.characters = characters
    }

    init(_ event: NSEvent) {
        self.init(keyCode: event.keyCode, flags: event.modifierFlags,
                  characters: event.characters(byApplyingModifiers: []))
    }

    var cancelsRecording: Bool { keyCode == UInt16(kVK_Escape) && flags.isEmpty }
    var shortcut: GlobalShortcut { GlobalShortcut(keyCode: keyCode, flags: flags, characters: characters) }
}
