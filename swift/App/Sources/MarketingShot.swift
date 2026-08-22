import SwiftUI
import AppKit
import TesseraCore

#if DEBUG
/// `Tessera --marketing <dir>` renders App Store screenshots at the required
/// 2560×1600 (1280×800 logical @2x), light + dark. Design iteration only —
/// compiled out of release builds.
///
/// The terminal frames are not drawings: they render recorded output of the real
/// `tess` binary, captured with `tmux capture-pane -e` against a throwaway vault
/// and committed under `docs/appstore-assets/captures/`. Point the renderer at
/// that directory with `TESSERA_SHOT_CAPTURES`, or pass `--captures <dir>`.
@MainActor
enum MarketingShot {
    static func runIfRequested() -> Bool {
        let args = CommandLine.arguments
        guard let i = args.firstIndex(of: "--marketing") else { return false }
        let dir = (i + 1 < args.count) ? args[i + 1] : NSTemporaryDirectory() + "tessera_marketing"
        try? FileManager.default.createDirectory(atPath: dir, withIntermediateDirectories: true)

        let captures: [String: [[AnsiRun]]]
        do {
            captures = try TerminalCapture.loadAll(in: resolveCaptureDir(args))
        } catch {
            FileHandle.standardError.write(Data("tessera --marketing: \(error)\n".utf8))
            exit(2)
        }

        let screens: [(String, AnyView)] = [
            ("01-watch", AnyView(Frame(
                title: "Codes in\nyour terminal.",
                subtitle: "tess watch lists every account with a live countdown bar. Arrow keys move, c copies, / searches.",
                content: TerminalBox(command: "tess watch", lines: captures["tess-watch"] ?? [])))),
            ("02-code", AnyView(Frame(
                title: "One command,\none code.",
                subtitle: "tess code github -c puts the current code on the clipboard. --json makes it scriptable.",
                content: TerminalBox(command: "tess code", lines: captures["tess-code"] ?? [])))),
            ("03-vault", AnyView(Frame(
                title: "Every account,\none window.",
                subtitle: "TOTP, HOTP, and Steam Guard, each with a countdown ring. Click a row to copy its code.",
                content: WindowMock()))),
            ("04-touchid", AnyView(Frame(
                title: "Unlock with\nTouch ID.",
                subtitle: "The vault is encrypted on your Mac with XChaCha20-Poly1305, and its key is wrapped by the Secure Enclave.",
                content: LockedScreen()))),
            ("05-import", AnyView(Frame(
                title: "Bring your\naccounts over.",
                subtitle: "Import a Google Authenticator transfer, or an Aegis, 2FAS, or Raivo export. The app also scans a QR code on screen.",
                content: TerminalBox(command: "tess import", lines: captures["tess-import"] ?? [])))),
            ("06-folders", AnyView(Frame(
                title: "Folders and\ntags.",
                subtitle: "Group accounts into folders in the app or the CLI. Tag and filter them with tess move, tess tag, and tess list.",
                content: TerminalBox(command: "tess list", lines: captures["tess-folders"] ?? [])))),
            ("07-format", AnyView(Frame(
                title: "One vault file,\ntwo cores.",
                subtitle: "The vault format is a published spec. The Go and Swift cores cross-decrypt shared vectors on every commit.",
                content: VaultFormatCard()))),
            ("08-private", AnyView(Frame(
                title: "Nothing leaves\nyour Mac.",
                subtitle: "No account and no servers. The app ships without a network entitlement, and the source is Apache-2.0.",
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

    /// `--captures <dir>`, else `$TESSERA_SHOT_CAPTURES`, else the in-repo path
    /// relative to the working directory.
    private static func resolveCaptureDir(_ args: [String]) -> String {
        if let i = args.firstIndex(of: "--captures"), i + 1 < args.count { return args[i + 1] }
        if let env = ProcessInfo.processInfo.environment["TESSERA_SHOT_CAPTURES"], !env.isEmpty { return env }
        return FileManager.default.currentDirectoryPath + "/docs/appstore-assets/captures"
    }

    private static func render(_ view: some View, to path: String) {
        let r = ImageRenderer(content: view.frame(width: 1280, height: 800))
        r.scale = 2 // → 2560×1600
        guard let img = r.nsImage else { return }
        try? QRImage.writePNG(img, to: URL(fileURLWithPath: path))
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
                    .fixedSize()
                    .clipShape(RoundedRectangle(cornerRadius: 22))
                    .overlay(RoundedRectangle(cornerRadius: 22).stroke(Palette.border, lineWidth: 1))
                    .shadow(color: .black.opacity(0.18), radius: 40, y: 18)
            }
            .padding(.horizontal, 80)
        }
    }
}

// MARK: Recorded terminal output

/// One styled span of a recorded terminal line.
struct AnsiRun {
    var text: String
    var fg: Color?
    var bg: Color?
    var bold: Bool
}

enum TerminalCaptureError: Error, CustomStringConvertible {
    case directoryMissing(String)
    case captureMissing(name: String, dir: String)
    case unreadable(path: String, underlying: Error)

    var description: String {
        switch self {
        case .directoryMissing(let dir):
            return "capture directory not found: \(dir). Record the frames first (docs/APP_STORE.md, Screenshots), or pass --captures <dir>."
        case .captureMissing(let name, let dir):
            return "missing capture \(name).ansi in \(dir). Record it with the command listed in docs/APP_STORE.md, Screenshots."
        case .unreadable(let path, let underlying):
            return "cannot read \(path): \(underlying)"
        }
    }
}

/// Loads `tmux capture-pane -e` recordings and turns their SGR escapes into
/// styled runs. Only the sequences tess and the shell actually emit are
/// honored; every other escape is skipped rather than printed.
enum TerminalCapture {
    static let names = ["tess-watch", "tess-code", "tess-import", "tess-folders"]

    static func loadAll(in dir: String) throws -> [String: [[AnsiRun]]] {
        var isDir: ObjCBool = false
        guard FileManager.default.fileExists(atPath: dir, isDirectory: &isDir), isDir.boolValue else {
            throw TerminalCaptureError.directoryMissing(dir)
        }
        var out: [String: [[AnsiRun]]] = [:]
        for name in names {
            let path = "\(dir)/\(name).ansi"
            guard FileManager.default.fileExists(atPath: path) else {
                throw TerminalCaptureError.captureMissing(name: name, dir: dir)
            }
            let raw: String
            do {
                raw = try String(contentsOfFile: path, encoding: .utf8)
            } catch {
                throw TerminalCaptureError.unreadable(path: path, underlying: error)
            }
            out[name] = parse(raw)
        }
        return out
    }

    static func parse(_ raw: String) -> [[AnsiRun]] {
        let lines = raw.split(separator: "\n", omittingEmptySubsequences: false).map { parseLine(String($0)) }
        var trimmed = lines
        while let last = trimmed.last, last.allSatisfy({ $0.text.trimmingCharacters(in: .whitespaces).isEmpty }) {
            trimmed.removeLast()
        }
        return trimmed
    }

    private static func parseLine(_ line: String) -> [AnsiRun] {
        var runs: [AnsiRun] = []
        var current = AnsiRun(text: "", fg: nil, bg: nil, bold: false)
        let chars = Array(line)
        var i = 0

        func flush() {
            if !current.text.isEmpty { runs.append(current) }
            current.text = ""
        }

        while i < chars.count {
            let c = chars[i]
            guard c == "\u{1B}" else {
                current.text.append(c)
                i += 1
                continue
            }
            // Escape sequence: ESC [ params final. Anything else is dropped.
            i += 1
            guard i < chars.count, chars[i] == "[" else { continue }
            i += 1
            var params = ""
            while i < chars.count, !("@"..."~").contains(chars[i]) {
                params.append(chars[i])
                i += 1
            }
            let final: Character = i < chars.count ? chars[i] : "m"
            i += 1
            guard final == "m" else { continue }
            flush()
            apply(params, to: &current)
        }
        flush()
        return runs
    }

    private static func apply(_ params: String, to run: inout AnsiRun) {
        if params.isEmpty {
            run.fg = nil; run.bg = nil; run.bold = false
            return
        }
        let codes = params.split(separator: ";", omittingEmptySubsequences: false).map { Int($0) ?? 0 }
        var j = 0
        while j < codes.count {
            switch codes[j] {
            case 0: run.fg = nil; run.bg = nil; run.bold = false
            case 1: run.bold = true
            case 22: run.bold = false
            case 39: run.fg = nil
            case 49: run.bg = nil
            case 30...37: run.fg = basic(codes[j] - 30, bright: false)
            case 90...97: run.fg = basic(codes[j] - 90, bright: true)
            case 40...47: run.bg = basic(codes[j] - 40, bright: false)
            case 100...107: run.bg = basic(codes[j] - 100, bright: true)
            case 38, 48:
                let isFg = codes[j] == 38
                guard j + 1 < codes.count else { return }
                if codes[j + 1] == 2, j + 4 < codes.count {
                    let c = Color(.sRGB,
                                  red: Double(codes[j + 2]) / 255,
                                  green: Double(codes[j + 3]) / 255,
                                  blue: Double(codes[j + 4]) / 255)
                    if isFg { run.fg = c } else { run.bg = c }
                    j += 4
                } else if codes[j + 1] == 5, j + 2 < codes.count {
                    let c = xterm256(codes[j + 2])
                    if isFg { run.fg = c } else { run.bg = c }
                    j += 2
                }
            default: break
            }
            j += 1
        }
    }

    private static func basic(_ n: Int, bright: Bool) -> Color {
        let normal: [UInt32] = [0x2A2E39, 0xE05252, 0x4CAF6A, 0xE3B23C,
                                0x5B8DEF, 0xB07CD6, 0x4EC9B0, 0xD6D6DE]
        let light: [UInt32] = [0x6E727D, 0xFF6B6B, 0x7BD88F, 0xF2CC60,
                               0x82AEFF, 0xCE9BE8, 0x6FE3CC, 0xF2F2F5]
        let table = bright ? light : normal
        return Color(hex: table[min(max(n, 0), 7)])
    }

    private static func xterm256(_ n: Int) -> Color {
        if n < 16 { return basic(n % 8, bright: n >= 8) }
        if n < 232 {
            let i = n - 16
            let steps: [Double] = [0, 95, 135, 175, 215, 255]
            return Color(.sRGB, red: steps[i / 36] / 255,
                         green: steps[(i / 6) % 6] / 255,
                         blue: steps[i % 6] / 255)
        }
        let v = Double(8 + (n - 232) * 10) / 255
        return Color(.sRGB, red: v, green: v, blue: v)
    }
}

/// Terminal chrome around a recorded session. 84 columns at 10.5pt fits the
/// frame's content column without wrapping.
private struct TerminalBox: View {
    let command: String
    let lines: [[AnsiRun]]
    private let size: CGFloat = 10.5
    private let background = Color(hex: 0x14161C)
    private let defaultText = Color(hex: 0xD6D6DE)

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack(spacing: 6) {
                Circle().fill(Color(hex: 0xFF5F57)).frame(width: 11, height: 11)
                Circle().fill(Color(hex: 0xFEBC2E)).frame(width: 11, height: 11)
                Circle().fill(Color(hex: 0x28C840)).frame(width: 11, height: 11)
                Spacer()
                Text(command).font(.system(size: 11, design: .monospaced))
                    .foregroundStyle(.white.opacity(0.4))
            }
            .padding(.bottom, 14)
            VStack(alignment: .leading, spacing: 2) {
                ForEach(Array(lines.enumerated()), id: \.offset) { _, runs in
                    Text(attributed(runs))
                }
            }
        }
        .padding(20)
        .frame(width: 594, alignment: .topLeading)
        .background(background)
    }

    private func attributed(_ runs: [AnsiRun]) -> AttributedString {
        guard !runs.isEmpty else {
            var blank = AttributedString(" ")
            blank.font = .system(size: size, design: .monospaced)
            return blank
        }
        var out = AttributedString("")
        for run in runs {
            var piece = AttributedString(run.text)
            piece.font = .system(size: size, weight: run.bold ? .bold : .regular, design: .monospaced)
            piece.foregroundColor = run.fg ?? defaultText
            if let bg = run.bg { piece.backgroundColor = bg }
            out.append(piece)
        }
        return out
    }
}

// MARK: App screens

private func sample() -> [Account] { AppModel.sampleAccounts }

/// The app's real locked screen, rendered from RootView. ImageRenderer cannot
/// draw the populated vault (its list never lays out headless), so the vault
/// frame below composes the app's real row view instead.
private struct LockedScreen: View {
    var body: some View {
        RootView()
            .environmentObject(AppModel(demo: .locked))
            .frame(width: 380, height: 500)
            .background(Palette.background)
    }
}

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
                                   onCopy: {}, onAdvance: {})
                }
            }.padding(14)
            Spacer(minLength: 0)
        }
        .frame(width: 380, height: 500)
        .background(Palette.background)
    }
}

/// The vault envelope, quoted from spec/vault-format.md.
private struct VaultFormatCard: View {
    private let mono = Font.system(size: 12, design: .monospaced)
    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            Text("vault.json").font(Typo.label(13, .semibold)).foregroundStyle(Palette.textSecondary)
            VStack(alignment: .leading, spacing: 3) {
                line("{", Palette.textSecondary)
                line("  \"version\": 1,", Palette.textPrimary)
                line("  \"aead\": \"xchacha20poly1305\",", Palette.textPrimary)
                line("  \"wraps\": [ argon2id, secure-enclave ],", Palette.accent)
                line("  \"payload\": { \"nonce\": …, \"ct\": … }", Palette.textPrimary)
                line("}", Palette.textSecondary)
            }
            Divider().overlay(Palette.border)
            VStack(alignment: .leading, spacing: 10) {
                fact("Written and read by the Go core and the Swift core")
                fact("Both cross-decrypt shared vectors in CI, every commit")
                fact("The spec ships in the repo: spec/vault-format.md")
            }
        }
        .padding(28)
        .frame(width: 430, alignment: .topLeading)
        .background(Palette.surface)
    }
    private func line(_ s: String, _ c: Color) -> some View {
        Text(s).font(mono).foregroundStyle(c)
    }
    private func fact(_ s: String) -> some View {
        HStack(alignment: .top, spacing: 10) {
            Image(systemName: "checkmark").font(.system(size: 11, weight: .bold))
                .foregroundStyle(Palette.accent).frame(width: 16)
            Text(s).font(Typo.label(13, .medium)).foregroundStyle(Palette.textPrimary)
                .fixedSize(horizontal: false, vertical: true)
        }
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
#endif
