import SwiftUI
import AppKit
import TesseraCore

/// `Tessera --marketing <dir>` renders App Store screenshots at the required
/// 2560×1600 (1280×800 logical @2x), light + dark. Design-iteration only.
@MainActor
enum MarketingShot {
    static func runIfRequested() -> Bool {
        let args = CommandLine.arguments
        guard let i = args.firstIndex(of: "--marketing") else { return false }
        let dir = (i + 1 < args.count) ? args[i + 1] : NSTemporaryDirectory() + "tessera_marketing"
        try? FileManager.default.createDirectory(atPath: dir, withIntermediateDirectories: true)

        let screens: [(String, AnyView)] = [
            ("01-vault", AnyView(Frame(
                title: "Every code,\nat a glance.",
                subtitle: "TOTP, HOTP, and Steam Guard — with a live countdown and one-click copy from your menu bar.",
                content: WindowMock()))),
            ("02-touchid", AnyView(Frame(
                title: "Locked to you.",
                subtitle: "Your vault is encrypted on-device with argon2id and XChaCha20-Poly1305, unlocked with Touch ID.",
                content: UnlockMock()))),
            ("03-cli", AnyView(Frame(
                title: "A real\ncommand line.",
                subtitle: "The only Mac authenticator with a first-class CLI. Live terminal view, scripting, JSON — tess watch.",
                content: TerminalMock()))),
            ("04-private", AnyView(Frame(
                title: "Private by\ndesign.",
                subtitle: "No account. No servers. No tracking. Open source, so anyone can verify exactly what it does.",
                content: TrustMock()))),
        ]
        for scheme in [ColorScheme.light, .dark] {
            for (name, view) in screens {
                render(view.environment(\.colorScheme, scheme).tint(Palette.accent),
                       to: "\(dir)/\(name)-\(scheme == .light ? "light" : "dark").png")
            }
        }
        return true
    }

    private static func render(_ view: some View, to path: String) {
        let r = ImageRenderer(content: view.frame(width: 1280, height: 800))
        r.scale = 2 // → 2560×1600
        guard let img = r.nsImage, let tiff = img.tiffRepresentation,
              let rep = NSBitmapImageRep(data: tiff),
              let png = rep.representation(using: .png, properties: [:]) else { return }
        try? png.write(to: URL(fileURLWithPath: path))
    }
}

// MARK: Canvas frame

private struct Frame<Content: View>: View {
    let title: String
    let subtitle: String
    let content: Content
    init(title: String, subtitle: String, @ViewBuilder content: () -> Content) {
        self.title = title; self.subtitle = subtitle; self.content = content()
    }
    init(title: String, subtitle: String, content: Content) {
        self.title = title; self.subtitle = subtitle; self.content = content
    }

    var body: some View {
        ZStack {
            LinearGradient(colors: [Palette.surfaceHi, Palette.background],
                           startPoint: .topLeading, endPoint: .bottomTrailing)
            HStack(spacing: 56) {
                VStack(alignment: .leading, spacing: 20) {
                    MosaicMark(side: 44)
                    Text(title).font(.system(size: 52, weight: .bold, design: .rounded))
                        .foregroundStyle(Palette.textPrimary).fixedSize(horizontal: false, vertical: true)
                    Text(subtitle).font(.system(size: 19))
                        .foregroundStyle(Palette.textSecondary).lineSpacing(3)
                        .fixedSize(horizontal: false, vertical: true)
                }
                .frame(width: 470, alignment: .leading)
                content
                    .clipShape(RoundedRectangle(cornerRadius: 22))
                    .overlay(RoundedRectangle(cornerRadius: 22).stroke(Palette.border, lineWidth: 1))
                    .shadow(color: .black.opacity(0.18), radius: 40, y: 18)
            }
            .padding(.horizontal, 80)
        }
    }
}

// MARK: Window mocks (non-scrolling so ImageRenderer captures them)

private func sample() -> [Account] { AppModel.sampleAccounts }

private struct WindowMock: View {
    var body: some View {
        VStack(spacing: 0) {
            HStack {
                HStack(spacing: 7) { MosaicMark(side: 18); Text("Tessera").font(Typo.display(16)).foregroundStyle(Palette.textPrimary) }
                Spacer()
                Image(systemName: "plus").font(.system(size: 12, weight: .bold)).foregroundStyle(Palette.accent)
                    .frame(width: 26, height: 26).background(Palette.accentSoft, in: RoundedRectangle(cornerRadius: 8))
            }.padding(14)
            Divider().overlay(Palette.border)
            VStack(spacing: 8) {
                ForEach(Array(sample().prefix(5).enumerated()), id: \.element.id) { i, a in
                    AccountRowView(account: a, remaining: [27, 19, 11, 4, 23][i],
                                   code: ["318 204", "907 551", "642 119", "VHHQY", "775 380"][i],
                                   copied: i == 0, reduceMotion: true,
                                   onCopy: {}, onPin: {}, onDelete: {}, onAdvance: {})
                }
            }.padding(14)
            Spacer(minLength: 0)
        }
        .frame(width: 380, height: 500)
        .background(Palette.background)
    }
}

private struct UnlockMock: View {
    var body: some View {
        VStack(spacing: 16) {
            Spacer()
            MosaicMark(side: 48)
            Text("Vault locked").font(Typo.display(19)).foregroundStyle(Palette.textPrimary)
            RoundedRectangle(cornerRadius: 9).fill(Palette.surfaceHi).frame(width: 240, height: 38)
                .overlay(HStack { Text("••••••••••").foregroundStyle(Palette.textFaint); Spacer() }.padding(.horizontal, 12))
            RoundedRectangle(cornerRadius: 9).fill(Palette.accent).frame(width: 240, height: 38)
                .overlay(Text("Unlock").font(Typo.label(13, .semibold)).foregroundStyle(.white))
            Label("Unlock with Touch ID", systemImage: "touchid").font(Typo.label(12, .medium)).foregroundStyle(Palette.accent)
            Spacer()
        }
        .frame(width: 380, height: 500).background(Palette.background)
    }
}

private struct TerminalMock: View {
    let gold = Color(hex: 0xE3B23C)
    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 6) {
                Circle().fill(Color(hex: 0xFF5F57)).frame(width: 11, height: 11)
                Circle().fill(Color(hex: 0xFEBC2E)).frame(width: 11, height: 11)
                Circle().fill(Color(hex: 0x28C840)).frame(width: 11, height: 11)
                Spacer()
                Text("tess watch").font(.system(size: 11, design: .monospaced)).foregroundStyle(.white.opacity(0.4))
            }.padding(.bottom, 8)
            row("◧ Tessera", gold, bold: true); rowPlain("6 accounts")
            Spacer().frame(height: 8)
            term("G", 0xB0506E, "GitHub", "318 204", 0.85)
            term("C", 0x3B6FB0, "Cloudflare", "907 551", 0.62)
            term("G", 0x8E5BA6, "Google", "642 119", 0.4)
            term("S", 0x4F5BB0, "Steam", "VHHQY", 0.12, warn: true)
            term("A", 0xB08A2E, "AWS", "775 380", 0.74)
            Spacer().frame(height: 10)
            Text("↑/↓ move · enter/c copy · / search · q quit")
                .font(.system(size: 11, design: .monospaced)).foregroundStyle(.white.opacity(0.4))
        }
        .padding(20)
        .frame(width: 440, height: 360, alignment: .topLeading)
        .background(Color(hex: 0x14161C))
    }
    func row(_ s: String, _ c: Color, bold: Bool = false) -> some View {
        Text(s).font(.system(size: 14, weight: bold ? .bold : .regular, design: .monospaced)).foregroundStyle(c)
    }
    func rowPlain(_ s: String) -> some View {
        Text(s).font(.system(size: 12, design: .monospaced)).foregroundStyle(.white.opacity(0.5))
    }
    func term(_ mono: String, _ hue: UInt32, _ name: String, _ code: String, _ frac: Double, warn: Bool = false) -> some View {
        HStack(spacing: 10) {
            Text(mono).font(.system(size: 13, weight: .bold, design: .monospaced)).foregroundStyle(Color(hex: hue)).frame(width: 14)
            Text(name).font(.system(size: 13, design: .monospaced)).foregroundStyle(.white.opacity(0.9)).frame(width: 110, alignment: .leading)
            Text(code).font(.system(size: 13, weight: .medium, design: .monospaced)).foregroundStyle(.white).frame(width: 70, alignment: .leading)
            bar(frac, warn: warn)
        }
    }
    func bar(_ frac: Double, warn: Bool) -> some View {
        let width: CGFloat = 90, n = 10, on = Int(Double(n) * frac + 0.5)
        return Text(String(repeating: "█", count: on) + String(repeating: "░", count: n - on))
            .font(.system(size: 12, design: .monospaced))
            .foregroundStyle(warn ? Color(hex: 0xF97316) : gold)
            .frame(width: width, alignment: .leading)
    }
}

private struct TrustMock: View {
    let items = [("lock.fill", "Encrypted on-device"), ("wifi.slash", "Works fully offline"),
                 ("person.crop.circle.badge.xmark", "No account, ever"), ("chevron.left.forwardslash.chevron.right", "Open source")]
    var body: some View {
        VStack(alignment: .leading, spacing: 18) {
            MosaicMark(side: 40)
            ForEach(items, id: \.0) { item in
                HStack(spacing: 12) {
                    Image(systemName: item.0).font(.system(size: 16)).foregroundStyle(Palette.accent).frame(width: 24)
                    Text(item.1).font(Typo.label(15, .medium)).foregroundStyle(Palette.textPrimary)
                }
            }
        }
        .padding(34)
        .frame(width: 380, height: 360, alignment: .topLeading)
        .background(Palette.surface)
    }
}
