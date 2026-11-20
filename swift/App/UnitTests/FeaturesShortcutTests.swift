import AppKit
import Carbon.HIToolbox
import XCTest
@testable import Tessera

@MainActor
final class FeaturesHotKeyRegistrar: HotKeyRegistering {
    var taken: Set<String> = []
    var failure: OSStatus?
    private(set) var active: [HotKeyToken: GlobalShortcut] = [:]
    private var actions: [HotKeyToken: @MainActor () -> Void] = [:]
    private var nextID: UInt32 = 1

    func register(_ shortcut: GlobalShortcut, action: @escaping @MainActor () -> Void) throws -> HotKeyToken {
        if taken.contains(shortcut.display) { throw HotKeyError.taken }
        if let failure { throw HotKeyError.failed(failure) }
        let token = HotKeyToken(id: nextID)
        nextID += 1
        active[token] = shortcut
        actions[token] = action
        return token
    }

    func unregister(_ token: HotKeyToken) {
        active[token] = nil
        actions[token] = nil
    }

    func press() { actions.values.forEach { $0() } }
}

@MainActor
final class FeaturesCounter {
    var value = 0
}

@MainActor
struct FeaturesConflicts: ShortcutConflictChecking {
    var issues: [String: ShortcutIssue] = [:]

    func conflict(for shortcut: GlobalShortcut) -> ShortcutIssue? { issues[shortcut.display] }
}

final class FeaturesGlobalShortcutTests: XCTestCase {
    private let all = UInt32(controlKey | optionKey | shiftKey | cmdKey)

    func testDisplayUsesMacModifierOrder() {
        let s = GlobalShortcut(keyCode: UInt32(kVK_ANSI_K), modifiers: all, key: "k")
        XCTAssertEqual(s.display, "⌃⌥⇧⌘K")
        XCTAssertEqual(s.accessibilityName, "Control-Option-Shift-Command-K")
    }

    func testModifiersOutsideTheShortcutSetAreDropped() {
        let s = GlobalShortcut(keyCode: UInt32(kVK_ANSI_T), modifiers: UInt32(cmdKey | alphaLock), key: "t")
        XCTAssertEqual(s.modifiers, UInt32(cmdKey))
        XCTAssertEqual(s.flags, [.command])
    }

    func testSpecialKeysUseTheirNames() {
        let space = GlobalShortcut(keyCode: UInt32(kVK_Space), modifiers: UInt32(optionKey), key: " ")
        XCTAssertEqual(space.display, "⌥Space")
        XCTAssertEqual(space.menuCharacter, " ")
        let f5 = GlobalShortcut(keyCode: UInt32(kVK_F5), modifiers: UInt32(controlKey), key: "\u{F708}")
        XCTAssertEqual(f5.display, "⌃F5")
        XCTAssertNil(f5.menuCharacter)
    }

    func testValidityNeedsCommandOptionOrControl() {
        XCTAssertFalse(GlobalShortcut(keyCode: UInt32(kVK_ANSI_T), modifiers: UInt32(shiftKey), key: "t").isValid)
        XCTAssertFalse(GlobalShortcut(keyCode: UInt32(kVK_ANSI_T), modifiers: 0, key: "t").isValid)
        XCTAssertFalse(GlobalShortcut(keyCode: UInt32(kVK_ANSI_T), modifiers: UInt32(cmdKey), key: "").isValid)
        XCTAssertTrue(GlobalShortcut(keyCode: UInt32(kVK_ANSI_T), modifiers: UInt32(optionKey), key: "t").isValid)
        XCTAssertTrue(GlobalShortcut(keyCode: UInt32(kVK_ANSI_T), modifiers: UInt32(controlKey), key: "t").isValid)
    }

    func testEventFlagsMapToCarbonModifiers() {
        let s = GlobalShortcut(keyCode: UInt16(kVK_ANSI_P), flags: [.command, .shift], characters: "p")
        XCTAssertEqual(s.modifiers, UInt32(cmdKey | shiftKey))
        XCTAssertEqual(s.flags, [.command, .shift])
        XCTAssertEqual(s.display, "⇧⌘P")
    }

    func testCodableRoundTrip() throws {
        let s = GlobalShortcut(keyCode: UInt32(kVK_ANSI_T), modifiers: UInt32(optionKey | cmdKey), key: "t")
        let decoded = try JSONDecoder().decode(GlobalShortcut.self, from: JSONEncoder().encode(s))
        XCTAssertEqual(decoded, s)
    }

    func testOnlyBareEscapeCancelsRecording() {
        XCTAssertTrue(RecordedKey(keyCode: UInt16(kVK_Escape), flags: [], characters: "\u{1B}").cancelsRecording)
        XCTAssertTrue(RecordedKey(keyCode: UInt16(kVK_Escape), flags: [.capsLock], characters: "\u{1B}").cancelsRecording)
        XCTAssertFalse(RecordedKey(keyCode: UInt16(kVK_Escape), flags: [.command], characters: "\u{1B}").cancelsRecording)
        XCTAssertFalse(RecordedKey(keyCode: UInt16(kVK_ANSI_T), flags: [], characters: "t").cancelsRecording)
    }

    func testIssueTextNamesTheShortcut() {
        XCTAssertEqual(ShortcutIssue.taken("⌥⌘T").text, "Another app uses ⌥⌘T. Choose another shortcut.")
        XCTAssertEqual(ShortcutIssue.usedByMenu("⌘N", item: "Add Account").text,
                       "Tessera uses ⌘N for Add Account. Choose another shortcut.")
    }
}

@MainActor
final class FeaturesMenuConflictTests: XCTestCase {
    private func menu() -> NSMenu {
        let main = NSMenu()
        let file = NSMenuItem(title: "File", action: nil, keyEquivalent: "")
        let items = NSMenu(title: "File")
        items.addItem(NSMenuItem(title: "Add Account…", action: nil, keyEquivalent: "n"))
        items.addItem(NSMenuItem(title: "Import", action: nil, keyEquivalent: "I"))
        let lock = NSMenuItem(title: "Lock", action: nil, keyEquivalent: "l")
        lock.keyEquivalentModifierMask = [.command, .control]
        items.addItem(lock)
        file.submenu = items
        main.addItem(file)
        return main
    }

    private func shortcut(_ code: Int, _ mods: Int, _ key: String) -> GlobalShortcut {
        GlobalShortcut(keyCode: UInt32(code), modifiers: UInt32(mods), key: key)
    }

    func testFindsTheMenuItemUsingAShortcut() {
        XCTAssertEqual(SystemShortcutConflicts.menuItem(using: shortcut(kVK_ANSI_N, cmdKey, "n"), in: menu()),
                       "Add Account")
        XCTAssertEqual(SystemShortcutConflicts.menuItem(using: shortcut(kVK_ANSI_I, cmdKey | shiftKey, "i"), in: menu()),
                       "Import")
        XCTAssertEqual(SystemShortcutConflicts.menuItem(using: shortcut(kVK_ANSI_L, cmdKey | controlKey, "l"), in: menu()),
                       "Lock")
    }

    func testDifferentModifiersDoNotConflict() {
        XCTAssertNil(SystemShortcutConflicts.menuItem(using: shortcut(kVK_ANSI_N, cmdKey | optionKey, "n"), in: menu()))
        XCTAssertNil(SystemShortcutConflicts.menuItem(using: shortcut(kVK_ANSI_I, cmdKey, "i"), in: menu()))
        XCTAssertNil(SystemShortcutConflicts.menuItem(using: shortcut(kVK_ANSI_L, cmdKey, "l"), in: menu()))
        XCTAssertNil(SystemShortcutConflicts.menuItem(using: shortcut(kVK_ANSI_N, cmdKey, "n"), in: nil))
    }
}

@MainActor
final class FeaturesQuickAccessSettingsTests: XCTestCase {
    private let optionT = GlobalShortcut(keyCode: UInt32(kVK_ANSI_T), modifiers: UInt32(optionKey | cmdKey), key: "t")
    private let controlK = GlobalShortcut(keyCode: UInt32(kVK_ANSI_K), modifiers: UInt32(controlKey | cmdKey), key: "k")

    private func makeDefaults() throws -> UserDefaults {
        let name = "tessera.tests.\(UUID().uuidString)"
        addTeardownBlock { UserDefaults.standard.removePersistentDomain(forName: name) }
        return try XCTUnwrap(UserDefaults(suiteName: name))
    }

    private func makeSettings(_ defaults: UserDefaults, _ registrar: FeaturesHotKeyRegistrar,
                              conflicts: FeaturesConflicts = FeaturesConflicts()) -> QuickAccessSettings {
        QuickAccessSettings(defaults: defaults, registrar: registrar, conflicts: conflicts)
    }

    private func key(_ code: Int, _ flags: NSEvent.ModifierFlags, _ characters: String) -> RecordedKey {
        RecordedKey(keyCode: UInt16(code), flags: flags, characters: characters)
    }

    func testMenuBarItemIsOnAtFirstRunAndRemembered() throws {
        let defaults = try makeDefaults()
        let settings = makeSettings(defaults, FeaturesHotKeyRegistrar())
        XCTAssertTrue(settings.showsInMenuBar)
        XCTAssertNil(settings.shortcut)
        settings.setShowsInMenuBar(false)
        XCTAssertFalse(makeSettings(defaults, FeaturesHotKeyRegistrar()).showsInMenuBar)
    }

    func testRecordedShortcutIsSavedRegisteredAndOpensTheList() throws {
        let defaults = try makeDefaults()
        let registrar = FeaturesHotKeyRegistrar()
        let settings = makeSettings(defaults, registrar)
        let opened = FeaturesCounter()
        settings.activate { opened.value += 1 }
        settings.beginRecording()
        XCTAssertTrue(settings.handle(key(kVK_ANSI_T, [.option, .command], "t")))
        XCTAssertFalse(settings.isRecording)
        XCTAssertEqual(settings.shortcut, optionT)
        XCTAssertNil(settings.issue)
        XCTAssertTrue(settings.isRegistered)
        XCTAssertEqual(Array(registrar.active.values), [optionT])
        registrar.press()
        XCTAssertEqual(opened.value, 1)
        XCTAssertEqual(makeSettings(defaults, FeaturesHotKeyRegistrar()).shortcut, optionT)
    }

    func testKeyWithoutCommandOptionOrControlAsksForOne() throws {
        let settings = makeSettings(try makeDefaults(), FeaturesHotKeyRegistrar())
        settings.beginRecording()
        XCTAssertTrue(settings.handle(key(kVK_ANSI_T, [.shift], "t")))
        XCTAssertEqual(settings.issue, .needsModifier)
        XCTAssertTrue(settings.isRecording)
        XCTAssertNil(settings.shortcut)
    }

    func testKeysAreIgnoredWhenNotRecording() throws {
        let settings = makeSettings(try makeDefaults(), FeaturesHotKeyRegistrar())
        XCTAssertFalse(settings.handle(key(kVK_ANSI_T, [.option, .command], "t")))
        XCTAssertNil(settings.shortcut)
    }

    func testRecordingReleasesTheShortcutAndEscapeRestoresIt() throws {
        let registrar = FeaturesHotKeyRegistrar()
        let settings = makeSettings(try makeDefaults(), registrar)
        settings.activate {}
        settings.record(optionT)
        settings.beginRecording()
        XCTAssertTrue(registrar.active.isEmpty)
        XCTAssertTrue(settings.handle(key(kVK_Escape, [], "\u{1B}")))
        XCTAssertFalse(settings.isRecording)
        XCTAssertEqual(Array(registrar.active.values), [optionT])
        XCTAssertEqual(settings.shortcut, optionT)
    }

    func testShortcutUsedByTesseraOrMacOSIsRejected() throws {
        let commandN = GlobalShortcut(keyCode: UInt32(kVK_ANSI_N), modifiers: UInt32(cmdKey), key: "n")
        let conflicts = FeaturesConflicts(issues: [commandN.display: .usedByMenu(commandN.display, item: "Add Account")])
        let registrar = FeaturesHotKeyRegistrar()
        let settings = makeSettings(try makeDefaults(), registrar, conflicts: conflicts)
        settings.activate {}
        settings.record(commandN)
        XCTAssertEqual(settings.issue, .usedByMenu("⌘N", item: "Add Account"))
        XCTAssertNil(settings.shortcut)
        XCTAssertTrue(registrar.active.isEmpty)
    }

    func testTakenShortcutKeepsThePreviousOne() throws {
        let defaults = try makeDefaults()
        let registrar = FeaturesHotKeyRegistrar()
        registrar.taken = [controlK.display]
        let settings = makeSettings(defaults, registrar)
        settings.activate {}
        settings.record(optionT)
        settings.record(controlK)
        XCTAssertEqual(settings.issue, .taken("⌃⌘K"))
        XCTAssertEqual(settings.shortcut, optionT)
        XCTAssertEqual(Array(registrar.active.values), [optionT])
        XCTAssertEqual(makeSettings(defaults, FeaturesHotKeyRegistrar()).shortcut, optionT)
    }

    func testOtherRegistrationFailureIsReported() throws {
        let registrar = FeaturesHotKeyRegistrar()
        registrar.failure = OSStatus(paramErr)
        let settings = makeSettings(try makeDefaults(), registrar)
        settings.activate {}
        settings.record(optionT)
        XCTAssertEqual(settings.issue, .failed("⌥⌘T"))
        XCTAssertNil(settings.shortcut)
    }

    func testSavedShortcutTakenAtLaunchIsReported() throws {
        let defaults = try makeDefaults()
        makeSettings(defaults, FeaturesHotKeyRegistrar()).record(optionT)
        let registrar = FeaturesHotKeyRegistrar()
        registrar.taken = [optionT.display]
        let settings = makeSettings(defaults, registrar)
        settings.activate {}
        XCTAssertEqual(settings.issue, .taken("⌥⌘T"))
        XCTAssertFalse(settings.isRegistered)
        XCTAssertEqual(settings.shortcut, optionT)
    }

    func testClearingRemovesTheShortcut() throws {
        let defaults = try makeDefaults()
        let registrar = FeaturesHotKeyRegistrar()
        let settings = makeSettings(defaults, registrar)
        settings.activate {}
        settings.record(optionT)
        settings.clearShortcut()
        XCTAssertNil(settings.shortcut)
        XCTAssertTrue(registrar.active.isEmpty)
        XCTAssertNil(defaults.data(forKey: QuickAccessSettings.shortcutKey))
        XCTAssertNil(makeSettings(defaults, FeaturesHotKeyRegistrar()).shortcut)
    }

    func testDeactivateReleasesTheShortcut() throws {
        let registrar = FeaturesHotKeyRegistrar()
        let settings = makeSettings(try makeDefaults(), registrar)
        settings.activate {}
        settings.record(optionT)
        settings.deactivate()
        XCTAssertFalse(settings.isRegistered)
        XCTAssertTrue(registrar.active.isEmpty)
    }

    func testUnusableSavedShortcutIsIgnored() throws {
        let defaults = try makeDefaults()
        let shiftOnly = GlobalShortcut(keyCode: UInt32(kVK_ANSI_T), modifiers: UInt32(shiftKey), key: "t")
        defaults.set(try JSONEncoder().encode(shiftOnly), forKey: QuickAccessSettings.shortcutKey)
        XCTAssertNil(makeSettings(defaults, FeaturesHotKeyRegistrar()).shortcut)
        defaults.set(Data("not json".utf8), forKey: QuickAccessSettings.shortcutKey)
        XCTAssertNil(makeSettings(defaults, FeaturesHotKeyRegistrar()).shortcut)
    }
}
