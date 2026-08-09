import SwiftUI
import AppKit
import TesseraCore
import TesseraArgon2

#if DEBUG
/// Hidden screenshot mode: `Tessera --shoot <outdir>` renders the key screens to
/// PNGs (light + dark) and exits. Design iteration only — compiled out of
/// release builds, so the shipping binary carries no hidden mode.
@MainActor
enum Screenshots {
    static func runIfRequested() -> Bool {
        let args = CommandLine.arguments
        guard let i = args.firstIndex(of: "--shoot") else { return false }
        let outDir = (i + 1 < args.count) ? args[i + 1] : NSTemporaryDirectory() + "tessera_shots"
        try? FileManager.default.createDirectory(atPath: outDir, withIntermediateDirectories: true)

        let shots: [(String, AnyView)] = [
            ("rows", AnyView(RowsPreview().frame(width: Metrics.windowWidth).background(Palette.background))),
            ("vault", AnyView(RootView().environmentObject(AppModel(demo: .populated)))),
            ("empty", AnyView(RootView().environmentObject(AppModel(demo: .empty)))),
            ("locked", AnyView(RootView().environmentObject(AppModel(demo: .locked)))),
            ("add", AnyView(AddAccountView().environmentObject(AppModel(demo: .populated)).background(Palette.background))),
            ("settings", AnyView(SettingsView().environmentObject(AppModel(demo: .populated)))),
        ]
        for scheme in [ColorScheme.light, .dark] {
            for (name, view) in shots {
                render(view.environment(\.colorScheme, scheme).tint(Palette.accent),
                       to: "\(outDir)/\(name)-\(scheme == .light ? "light" : "dark").png")
            }
        }
        return true
    }

    private static func render(_ view: some View, to path: String) {
        let renderer = ImageRenderer(content: view)
        renderer.scale = 2
        guard let img = renderer.nsImage else { return }
        try? QRImage.writePNG(img, to: URL(fileURLWithPath: path))
    }
}

/// Non-scrolling stack of sample rows so ImageRenderer captures the row design.
private struct RowsPreview: View {
    var body: some View {
        let accts = AppModel.sampleAccounts
        let remaining = [26, 18, 9, 3, 22, 30]
        VStack(spacing: 8) {
            SectionLabel("Pinned")
            ForEach(Array(accts.prefix(2).enumerated()), id: \.element.id) { i, a in
                AccountRowView(account: a, remaining: remaining[i],
                               code: sampleCode(a), copied: i == 0,
                               reduceMotion: false, onCopy: {}, onAdvance: {})
            }
            SectionLabel("All").padding(.top, 4)
            ForEach(Array(accts.dropFirst(2).enumerated()), id: \.element.id) { i, a in
                AccountRowView(account: a, remaining: remaining[i + 2],
                               code: sampleCode(a), copied: false,
                               reduceMotion: false, onCopy: {}, onAdvance: {})
            }
        }
        .padding(Metrics.pad)
    }
    private func sampleCode(_ a: Account) -> String {
        switch a.type { case .steam: return "VHHQY"; case .hotp: return "418 920"; default: break }
        return ["318 204", "907 551", "642 119", "", "775 380"][min(Int(a.id) ?? 1, 4)]
    }
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
        if Screenshots.runIfRequested() {
            // Give the renderer a beat, then exit before showing UI.
            DispatchQueue.main.asyncAfter(deadline: .now() + 0.3) { NSApp.terminate(nil) }
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
