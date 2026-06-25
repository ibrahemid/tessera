import SwiftUI
import AppKit
import TesseraCore

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
            ("create", AnyView(RootView().environmentObject(AppModel(demo: .fresh)))),
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
        guard let img = renderer.nsImage,
              let tiff = img.tiffRepresentation,
              let rep = NSBitmapImageRep(data: tiff),
              let png = rep.representation(using: .png, properties: [:]) else { return }
        try? png.write(to: URL(fileURLWithPath: path))
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
                               reduceMotion: false, onCopy: {}, onPin: {}, onDelete: {}, onAdvance: {})
            }
            SectionLabel("All").padding(.top, 4)
            ForEach(Array(accts.dropFirst(2).enumerated()), id: \.element.id) { i, a in
                AccountRowView(account: a, remaining: remaining[i + 2],
                               code: sampleCode(a), copied: false,
                               reduceMotion: false, onCopy: {}, onPin: {}, onDelete: {}, onAdvance: {})
            }
        }
        .padding(Metrics.pad)
    }
    private func sampleCode(_ a: Account) -> String {
        switch a.type { case .steam: return "VHHQY"; case .hotp: return "418 920"; default: break }
        return ["318 204", "907 551", "642 119", "", "775 380"][min(Int(a.id) ?? 1, 4)]
    }
}

final class AppDelegate: NSObject, NSApplicationDelegate {
    func applicationDidFinishLaunching(_ notification: Notification) {
        if Screenshots.runIfRequested() {
            // Give the renderer a beat, then exit before showing UI.
            DispatchQueue.main.asyncAfter(deadline: .now() + 0.3) { NSApp.terminate(nil) }
        }
    }
}
