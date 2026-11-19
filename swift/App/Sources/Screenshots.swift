import SwiftUI
import AppKit
import TesseraCore
import TesseraArgon2
#if DEBUG
import ScreenCaptureKit
#endif

#if DEBUG
/// `Tessera --shoot <outdir>` writes every shot, light and dark, to `<screen>-<variant>-<light|dark>.png`.
@MainActor
enum Screenshots {
    static var isRequested: Bool { CommandLine.arguments.contains("--shoot") }

    static let frozenNow = Date(timeIntervalSince1970: 1_800_000_012)

    enum Chrome {
        case window
        case panel
    }

    enum ShotError: Error, CustomStringConvertible {
        case noWindow(String), noPNG(String), unsupportedOS, noContrastOverride
        var description: String {
            switch self {
            case .noContrastOverride: return "could not force Increase Contrast for the contrast shots"
            case .noWindow(let name): return "window not capturable for \(name)"
            case .noPNG(let name): return "PNG encoding failed for \(name)"
            case .unsupportedOS: return "--shoot needs macOS 14.4 or later"
            }
        }
    }

    struct Shot {
        let screen: String
        let variant: String
        var size: CGSize?
        var chrome: Chrome = .panel
        var compact = false
        var scrollToBottom = false
        var state: AppModel.DemoState = .populated
        var highContrast = false
        var setUp: (AppModel) -> Void = { _ in }
        var afterAppear: (AppModel) -> Void = { _ in }
        let view: () -> AnyView
    }

    static let mainSize = CGSize(width: 920, height: 620)
    static let compactSize = CGSize(width: 480, height: 620)

    static var shots: [Shot] { baseShots + vaultShots + sheetShots + featureShots }

    static var baseShots: [Shot] {
        let root = { AnyView(RootView()) }
        let uri = "otpauth://totp/GitHub:octocat?secret=JBSWY3DPEHPK3PXP&issuer=GitHub"
        return [
            Shot(screen: "locked", variant: "touchid", size: mainSize, chrome: .window, state: .locked, view: root),
            Shot(screen: "locked", variant: "plain", size: mainSize, chrome: .window, state: .locked,
                 setUp: { $0.requireBiometrics = false }, view: root),
            Shot(screen: "passphrase", variant: "cli", size: mainSize, chrome: .window, state: .locked,
                 setUp: { $0.needsPassphrase = true }, view: root),
            Shot(screen: "passphrase", variant: "error", size: mainSize, chrome: .window, state: .locked,
                 setUp: { m in
                     m.needsPassphrase = true; m.passphraseIsRecovery = true
                     m.errorMessage = UserMessage.text(for: VaultError.wrongPassphrase, context: .unlock)
                 }, view: root),
            Shot(screen: "reset", variant: "prompt", size: mainSize, chrome: .window, state: .locked,
                 setUp: { $0.needsReset = true }, view: root),
            Shot(screen: "recovery", variant: "touchid", size: mainSize, chrome: .window, state: .locked,
                 setUp: { $0.needsRecovery = true }, view: root),
            Shot(screen: "unreachable", variant: "external", size: mainSize, chrome: .window, state: .locked,
                 setUp: { $0.vaultUnreachable = true }, view: root),
            Shot(screen: "vault", variant: "empty", size: mainSize, chrome: .window, state: .empty, view: root),
            Shot(screen: "vault", variant: "window", size: mainSize, chrome: .window, view: root),
            Shot(screen: "vault", variant: "compact", size: compactSize, chrome: .window, compact: true, view: root),
            Shot(screen: "vault", variant: "contrast", size: mainSize, chrome: .window, highContrast: true, view: root),
            Shot(screen: "notice", variant: "info", size: mainSize, chrome: .window,
                 setUp: { $0.notice = .info("Copied GitHub") }, view: root),
            Shot(screen: "notice", variant: "failure", size: mainSize, chrome: .window,
                 setUp: { $0.notice = .failure(saveFailure) }, view: root),
            Shot(screen: "add", variant: "empty") { AnyView(AddAccountView()) },
            Shot(screen: "add", variant: "uri") { AnyView(AddAccountView(input: uri)) },
            Shot(screen: "add", variant: "manual") { AnyView(AddAccountView(input: "JBSWY3DPEHPK3PXP", manual: true)) },
            Shot(screen: "add", variant: "report", afterAppear: { $0.importReport = sampleReport }) {
                AnyView(AddAccountView())
            },
            Shot(screen: "add", variant: "contrast", highContrast: true, afterAppear: { $0.importReport = sampleReport }) {
                AnyView(AddAccountView())
            },
            Shot(screen: "add", variant: "scandenied",
                 afterAppear: { $0.errorMessage = UserMessage.screenRecordingDenied }) { AnyView(AddAccountView()) },
            Shot(screen: "add", variant: "error", afterAppear: { m in
                m.errorMessage = UserMessage.text(for: AccountEntryError.invalidSetupKey, context: .add)
            }) { AnyView(AddAccountView(input: "JBSWY3DPEHPK3PX1", manual: true)) },
            Shot(screen: "edit", variant: "account") { AnyView(EditAccountView(account: accounts[0])) },
            Shot(screen: "qr", variant: "export") { AnyView(QRExportView(account: accounts[0])) },
            Shot(screen: "settings", variant: "general") { AnyView(SettingsView(tab: .general)) },
            Shot(screen: "settings", variant: "security") { AnyView(SettingsView(tab: .security)) },
            Shot(screen: "settings", variant: "vault") { AnyView(SettingsView(tab: .vault)) },
            Shot(screen: "settings", variant: "backup") { AnyView(SettingsView(tab: .backup)) },
            Shot(screen: "settings", variant: "feedback") {
                AnyView(SettingsView(tab: .vault, feedback: [.vaultFile: .failure("That file isn’t a Tessera vault.")]))
            },
            Shot(screen: "recoverypassphrase", variant: "sheet") { AnyView(RecoveryPassphraseView()) },
            Shot(screen: "backup", variant: "export") { AnyView(ExportBackupView()) },
            Shot(screen: "backup", variant: "restore") { AnyView(RestoreBackupView()) },
        ]
    }

    static var accounts: [Account] {
        var list = AppModel.sampleAccounts
        for i in list.indices {
            if list[i].issuer == "AWS" { list[i].period = 15 }
            if list[i].pinned { list[i].folder = "Work" }
        }
        list.insert(Account(id: "7", type: .totp, issuer: "Northwind Federal Credit Union",
                            account: "member-services-primary@northwind.example",
                            secret: Data("northwindsecret00000".utf8)), at: 3)
        Handles.assign(&list)
        return list
    }

    static var saveFailure: String {
        UserMessage.text(for: CocoaError(.fileWriteNoPermission), context: .save,
                         url: URL(fileURLWithPath: "/Users/demo/Documents/vault.json"))
    }

    static var sampleReport: ImportSummary {
        ImportSummary(added: 2, duplicates: 1, failures: [
            ImportItemFailure(source: "Line 3", display: "otpauth://totp/Acme:dev?secret=…",
                              reason: "base32: invalid character 1"),
            ImportItemFailure(source: "Line 5", display: "", reason: "missing secret"),
            ImportItemFailure(source: "scan.png", display: "", reason: "No QR code found", kind: .noQR),
        ])
    }

    static func run() async {
        let args = CommandLine.arguments
        guard let i = args.firstIndex(of: "--shoot") else { return }
        let outDir = URL(fileURLWithPath: (i + 1 < args.count) ? args[i + 1] : NSTemporaryDirectory() + "tessera_shots")
        do {
            try FileManager.default.createDirectory(at: outDir, withIntermediateDirectories: true)
        } catch {
            FileHandle.standardError.write(Data("tessera --shoot: \(error.localizedDescription)\n".utf8))
            return
        }
        for w in NSApp.windows { w.orderOut(nil) }
        for shot in shots {
            for dark in [false, true] {
                let url = outDir.appendingPathComponent("\(shot.screen)-\(shot.variant)-\(dark ? "dark" : "light").png")
                do {
                    try await capture(shot, dark: dark, to: url)
                    print(url.path)
                } catch {
                    FileHandle.standardError.write(Data("tessera --shoot: \(url.lastPathComponent): \(error)\n".utf8))
                }
            }
        }
    }

    private static func model(for shot: Shot) -> AppModel {
        let m = AppModel(demo: shot.state)
        if shot.state == .populated { m.accounts = accounts }
        m.ticker.freeze(at: frozenNow)
        shot.setUp(m)
        return m
    }

    private static func capture(_ shot: Shot, dark: Bool, to url: URL) async throws {
        UserDefaults.standard.register(defaults: [AppModel.compactPrefKey: shot.compact])
        let model = model(for: shot)
        let restoreContrast = shot.highContrast ? try forceIncreasedContrast() : nil
        defer { restoreContrast?() }
        let host = NSHostingView(rootView: shot.view().environmentObject(model))
        host.sceneBridgingOptions = [.toolbars]
        let style: NSWindow.StyleMask = shot.chrome == .window
            ? [.titled, .closable, .miniaturizable, .resizable, .fullSizeContentView]
            : [.borderless]
        // The window server only captures a window partly on a screen: one point stays on it, below every other window.
        let screen = NSScreen.screens.first?.frame ?? .zero
        let origin = NSPoint(x: screen.maxX - 1, y: screen.minY)
        let window = OffscreenWindow(contentRect: NSRect(origin: origin, size: shot.size ?? CGSize(width: 400, height: 300)),
                                     styleMask: style, backing: .buffered, defer: false)
        window.isReleasedWhenClosed = false
        window.hasShadow = false
        window.level = NSWindow.Level(rawValue: Int(CGWindowLevelForKey(.desktopWindow)))
        let appearance: NSAppearance.Name = shot.highContrast
            ? (dark ? .accessibilityHighContrastDarkAqua : .accessibilityHighContrastAqua)
            : (dark ? .darkAqua : .aqua)
        window.appearance = NSAppearance(named: appearance)
        // WindowConfigurator styles the titlebar asynchronously, which can land after the capture.
        window.titlebarAppearsTransparent = true
        window.contentView = host
        if shot.size == nil {
            host.layoutSubtreeIfNeeded()
            window.setContentSize(host.fittingSize)
        }
        window.setFrameOrigin(origin)
        window.orderFront(nil)
        defer { window.orderOut(nil); window.close() }

        try await Task.sleep(for: .milliseconds(500))
        shot.afterAppear(model)
        try await Task.sleep(for: .milliseconds(250))
        if shot.size == nil { window.setContentSize(host.fittingSize) }
        if shot.scrollToBottom { scrollToBottom(in: host) }
        try await Task.sleep(for: .milliseconds(250))
        host.layoutSubtreeIfNeeded()
        window.displayIfNeeded()

        let image = try await grab(window, name: url.lastPathComponent)
        guard let png = NSBitmapImageRep(cgImage: image).representation(using: .png, properties: [:]) else {
            throw ShotError.noPNG(url.lastPathComponent)
        }
        try png.write(to: url, options: [.atomic])
    }

    // SwiftUI resolves dynamic colors with the high-contrast appearance only while the system Increase Contrast flag is on.
    private static func forceIncreasedContrast() throws -> () -> Void {
        guard let method = class_getInstanceMethod(NSWorkspace.self,
                                                   #selector(getter: NSWorkspace.accessibilityDisplayShouldIncreaseContrast)),
              let forced = class_getInstanceMethod(IncreasedContrast.self, #selector(getter: IncreasedContrast.isOn)) else {
            throw ShotError.noContrastOverride
        }
        let original = method_setImplementation(method, method_getImplementation(forced))
        postDisplayOptionsChange()
        return {
            method_setImplementation(method, original)
            postDisplayOptionsChange()
        }
    }

    private static func postDisplayOptionsChange() {
        NSWorkspace.shared.notificationCenter.post(name: NSWorkspace.accessibilityDisplayOptionsDidChangeNotification,
                                                   object: NSWorkspace.shared)
    }

    // Window-server capture, since cacheDisplay drops vibrancy and sidebar materials.
    private static func grab(_ window: NSWindow, name: String) async throws -> CGImage {
        guard #available(macOS 14.4, *) else { throw ShotError.unsupportedOS }
        for _ in 0..<20 {
            // The views shield their windows from capture; these windows never hold a real vault.
            window.sharingType = .readOnly
            let content = try await SCShareableContent.currentProcess
            if let target = content.windows.first(where: { $0.windowID == CGWindowID(window.windowNumber) }),
               target.isOnScreen {
                let filter = SCContentFilter(desktopIndependentWindow: target)
                if CGFloat(filter.pointPixelScale) == window.backingScaleFactor {
                    let config = SCStreamConfiguration()
                    config.width = Int(filter.contentRect.width * CGFloat(filter.pointPixelScale))
                    config.height = Int(filter.contentRect.height * CGFloat(filter.pointPixelScale))
                    config.captureResolution = .best
                    config.ignoreShadowsSingleWindow = true
                    config.showsCursor = false
                    return try await SCScreenshotManager.captureImage(contentFilter: filter, configuration: config)
                }
            }
            try await Task.sleep(for: .milliseconds(100))
        }
        throw ShotError.noWindow(name)
    }

    private static func scrollToBottom(in view: NSView) {
        guard let scroll = firstScrollView(in: view), let doc = scroll.documentView else { return }
        let clip = scroll.contentView
        let y = doc.isFlipped ? max(0, doc.frame.height - clip.bounds.height) : 0
        clip.scroll(to: NSPoint(x: 0, y: y))
        scroll.reflectScrolledClipView(clip)
    }

    private static func firstScrollView(in view: NSView) -> NSScrollView? {
        if let s = view as? NSScrollView { return s }
        for sub in view.subviews { if let s = firstScrollView(in: sub) { return s } }
        return nil
    }
}

/// A window AppKit never pulls back onto a screen, so shots render out of sight.
private final class OffscreenWindow: NSWindow {
    override func constrainFrameRect(_ frameRect: NSRect, to screen: NSScreen?) -> NSRect { frameRect }
}

private final class IncreasedContrast: NSObject {
    @objc var isOn: Bool { true }
}
#endif

/// Exercises the seal/write/read/open path under whatever sandbox the running
/// binary is signed with. `Tessera --selftest` prints PASS/FAIL and exits.
///
/// It writes to a throwaway file in the sandbox temp directory and never goes
/// through `VaultStore`, which would resolve the user's real vault (an external
/// bookmark, `TESSERA_VAULT`, or the container file) and delete it on the way
/// out. The self-test must never touch a vault that holds accounts.
@MainActor
enum SelfTest {
    static func runIfRequested() -> Bool {
        guard CommandLine.arguments.contains("--selftest") else { return false }
        let argon2 = Argon2Reference()
        let pass = "selftest-passphrase"
        let url = FileManager.default.temporaryDirectory
            .appendingPathComponent("tessera-selftest-\(UUID().uuidString).json")
        // exit() does not unwind, so every path cleans up before it.
        do {
            let acct = Account(id: "t1", type: .totp, issuer: "SelfTest", account: "x",
                               secret: Data("12345678901234567890".utf8), algorithm: "SHA1",
                               digits: 6, period: 30)
            let env = try Envelope.create(accounts: [acct], passphrase: pass, argon2: argon2)
            try env.encoded().write(to: url, options: [.atomic, .completeFileProtection])
            let reopened = try Envelope.decode(try Data(contentsOf: url))
            let got = try reopened.open(passphrase: pass, argon2: argon2)
            if got.count == 1 && got[0].issuer == "SelfTest" {
                print("SELFTEST PASS path=\(url.path)")
                try? FileManager.default.removeItem(at: url)
                exit(0)
            }
            print("SELFTEST FAIL: unexpected accounts \(got)")
            try? FileManager.default.removeItem(at: url)
            exit(1)
        } catch {
            print("SELFTEST FAIL: \(error)")
            try? FileManager.default.removeItem(at: url)
            exit(1)
        }
    }
}

/// Verifies the default daily-unlock path: a non-biometric Secure Enclave wrap
/// must open with NO Touch ID prompt. `Tessera --selftest-se` prints PASS/FAIL
/// (or SKIP on SE-less Macs). A PASS with no biometric dialog confirms silent
/// open. Kept in release builds because it validates the signed build's Secure
/// Enclave entitlement; it works entirely in memory and never reads, writes, or
/// deletes a vault file.
@MainActor
enum SelfTestSE {
    static func runIfRequested() -> Bool {
        guard CommandLine.arguments.contains("--selftest-se") else { return false }
        guard SecureEnclaveWrap.isAvailable else { print("SELFTEST-SE SKIP: no Secure Enclave"); exit(0) }
        do {
            let acct = Account(id: "t1", type: .totp, issuer: "SelfTest", account: "x",
                               secret: Data("12345678901234567890".utf8), algorithm: "SHA1",
                               digits: 6, period: 30)
            let made = try Envelope.createUnwrapped(accounts: [acct])
            var env = made.0
            try SecureEnclaveWrap.enable(on: &env, dek: made.1, requireBiometrics: false)
            let dek = try SecureEnclaveWrap.open(env, reason: "Tessera self-test")
            let got = try env.open(dek: dek)
            if got.count == 1 && got[0].issuer == "SelfTest" {
                print("SELFTEST-SE PASS: non-biometric SE wrap opened silently")
                exit(0)
            }
            print("SELFTEST-SE FAIL: unexpected accounts \(got)")
            exit(1)
        } catch {
            print("SELFTEST-SE FAIL: \(error)")
            exit(1)
        }
    }
}

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
