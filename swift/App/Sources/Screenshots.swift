import SwiftUI
import AppKit
import TesseraCore
import TesseraArgon2

/// Hidden screenshot mode: `Tessera --shoot <outdir>` renders the key screens to
/// PNGs (light + dark) and exits. Used for design iteration; no effect on the
/// shipping app.
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

/// Exercises the real vault read/write path under whatever sandbox the running
/// binary is signed with. `Tessera --selftest` prints PASS/FAIL and exits.
@MainActor
enum SelfTest {
    static func runIfRequested() -> Bool {
        guard CommandLine.arguments.contains("--selftest") else { return false }
        let store = VaultStore()
        let argon2 = Argon2Reference()
        let pass = "selftest-passphrase"
        do {
            let acct = Account(id: "t1", type: .totp, issuer: "SelfTest", account: "x",
                               secret: Data("12345678901234567890".utf8), algorithm: "SHA1",
                               digits: 6, period: 30)
            let env = try Envelope.create(accounts: [acct], passphrase: pass, argon2: argon2)
            try store.save(env)                          // write into the sandbox container
            let reopened = try store.load()              // read back
            let got = try reopened.open(passphrase: pass, argon2: argon2)
            try? FileManager.default.removeItem(at: store.vaultURL)
            if got.count == 1 && got[0].issuer == "SelfTest" {
                print("SELFTEST PASS path=\(store.vaultURL.path)")
                exit(0)
            }
            print("SELFTEST FAIL: unexpected accounts \(got)")
            exit(1)
        } catch {
            print("SELFTEST FAIL: \(error)")
            exit(1)
        }
    }
}

/// Verifies the default daily-unlock path: a non-biometric Secure Enclave wrap
/// must open with NO Touch ID prompt. `Tessera --selftest-se` prints PASS/FAIL
/// (or SKIP on SE-less Macs). A PASS with no biometric dialog confirms silent
/// open. Run from the signed app (SE needs entitlements).
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
    func applicationDidFinishLaunching(_ notification: Notification) {
        if SelfTest.runIfRequested() { return }
        if SelfTestSE.runIfRequested() { return }
        if MarketingShot.runIfRequested() {
            DispatchQueue.main.asyncAfter(deadline: .now() + 0.3) { NSApp.terminate(nil) }
            return
        }
        if Screenshots.runIfRequested() {
            // Give the renderer a beat, then exit before showing UI.
            DispatchQueue.main.asyncAfter(deadline: .now() + 0.3) { NSApp.terminate(nil) }
        }
    }

    func applicationShouldHandleReopen(_ sender: NSApplication, hasVisibleWindows flag: Bool) -> Bool {
        if !flag {
            sender.windows.first(where: { $0.canBecomeKey })?.makeKeyAndOrderFront(nil)
        }
        return true
    }
}
