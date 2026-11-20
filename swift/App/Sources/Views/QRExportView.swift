import SwiftUI
import AppKit
import UniformTypeIdentifiers
import TesseraCore

struct QRExportView: View {
    @EnvironmentObject var model: AppModel
    @Environment(\.dismiss) var dismiss
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let account: Account
    private enum Field { case secret, link }
    @State private var copied: Field?
    @State private var hasCopied = false
    // The account is fixed for the sheet's lifetime, but body re-evaluates every
    // second with the ticking model — render the QR once, not per tick.
    @State private var qrImage: NSImage?
    @State private var saveError: String?

    init(account: Account) {
        self.account = account
    }

    #if DEBUG
    init(account: Account, copiedSecret: Bool) {
        self.account = account
        _copied = State(initialValue: copiedSecret ? .secret : nil)
        _hasCopied = State(initialValue: copiedSecret)
    }
    #endif

    private var name: String { account.displayName }

    var body: some View {
        VStack(spacing: 12) {
            VStack(spacing: 2) {
                Text(name).font(Typo.display(16)).foregroundStyle(Palette.textPrimary)
                Text("Move this account to another app").font(Typo.label(11)).foregroundStyle(Palette.textSecondary)
            }

            // Export copies the SECRET / setup link (for migration), not the
            // rotating code — copying the code here would be useless.
            copyButton("Copy setup key", field: .secret, value: model.secretBase32(for: account))
            copyButton("Copy setup link", field: .link, value: model.otpauthURI(for: account))
            if hasCopied {
                Text("The clipboard clears \(Int(AppModel.pasteboardExpiry)) seconds after each copy.")
                    .multilineTextAlignment(.center).font(Typo.label(11)).foregroundStyle(Palette.textSecondary)
                    .frame(maxWidth: 240)
                    .fixedSize(horizontal: false, vertical: true)
            }

            if let img = qrImage {
                Image(nsImage: img).interpolation(.none).resizable()
                    .frame(width: 190, height: 190)
                    .background(.white, in: RoundedRectangle(cornerRadius: 8))
                Text("Or scan this QR with another authenticator. It contains the secret.")
                    .multilineTextAlignment(.center).font(Typo.label(11)).foregroundStyle(Palette.textSecondary)
                    .frame(maxWidth: 240)
                    .fixedSize(horizontal: false, vertical: true)
                if let e = saveError { ErrorLine(e) }
                HStack {
                    Button("Save image…") { save(img) }.buttonStyle(.plain).foregroundStyle(Palette.accentInk)
                    Spacer()
                    Button("Done") { dismiss() }.buttonStyle(.plain).foregroundStyle(Palette.textSecondary)
                        .keyboardShortcut(.defaultAction)
                }.font(Typo.label(12, .medium)).frame(maxWidth: 240)
            } else {
                Button("Done") { dismiss() }.buttonStyle(.plain).foregroundStyle(Palette.textSecondary)
                    .keyboardShortcut(.defaultAction)
            }
        }
        .padding(24).frame(width: 300).background(Palette.background)
        .background(CaptureShield())
        .onAppear { qrImage = QRImage.generate(from: model.otpauthURI(for: account)) }
    }

    private func copyButton(_ title: String, field: Field, value: String) -> some View {
        Button { copy(value, field) } label: {
            HStack(spacing: 8) {
                Image(systemName: copied == field ? "checkmark" : "doc.on.doc")
                    .font(.system(size: 12, weight: .semibold)).foregroundStyle(Palette.accentInk)
                Text(copied == field ? "Copied" : title)
                    .font(Typo.label(12, .medium)).foregroundStyle(Palette.textPrimary)
                Spacer()
            }
            .padding(.horizontal, 12).padding(.vertical, 9).frame(maxWidth: 240)
            .background(Palette.surfaceHi, in: RoundedRectangle(cornerRadius: 9))
            .overlay(RoundedRectangle(cornerRadius: 9).stroke(Palette.border, lineWidth: 1))
        }
        .buttonStyle(.plain)
    }

    private func copy(_ value: String, _ field: Field) {
        model.copyToPasteboard(value, kind: .secret)
        withAnimation(reduceMotion ? nil : .default) { copied = field; hasCopied = true }
        Task { @MainActor in
            try? await Task.sleep(for: .seconds(1.3))
            withAnimation(reduceMotion ? nil : .default) { if copied == field { copied = nil } }
        }
    }

    private func save(_ img: NSImage) {
        let panel = NSSavePanel()
        panel.nameFieldStringValue = "\(name).png"
        panel.allowedContentTypes = [.png]
        guard panel.runModal() == .OK, let url = panel.url else { return }
        do {
            try QRImage.writePNG(img, to: url)
            saveError = nil
        } catch {
            saveError = UserMessage.text(for: error, context: .export, url: url)
        }
    }
}
