import SwiftUI

struct DamagedVaultView: View {
    struct Option: Equatable, Identifiable {
        let source: URL
        let title: String
        var id: URL { source }
    }

    let info: DamagedVaultInfo
    let displayPath: (URL) -> String
    let restore: @MainActor (URL) async -> String?
    let openAnother: @MainActor () -> String?
    @State private var isRestoring = false
    @State private var message: String?

    static func options(for info: DamagedVaultInfo) -> [Option] {
        var options: [Option] = []
        if let backup = info.backup {
            options.append(Option(source: backup, title: "Restore previous version"))
        }
        if let rollback = info.rollback {
            options.append(Option(source: rollback, title: "Restore copy from before the key change"))
        }
        return options
    }

    var body: some View {
        let options = Self.options(for: info)
        VStack(spacing: 16) {
            Spacer()
            MosaicMark(side: 50)
            Text("Can’t read your vault").font(Typo.display(20)).foregroundStyle(Palette.textPrimary)
            Text("\(displayPath(info.vault)) is damaged or was saved by a newer Tessera.")
                .multilineTextAlignment(.center).font(Typo.label(13)).foregroundStyle(Palette.textSecondary)
                .frame(maxWidth: 360)
            if let first = options.first {
                PrimaryButton(first.title, enabled: !isRestoring) { start(first.source) }.frame(maxWidth: 260)
                ForEach(options.dropFirst()) { option in
                    secondaryButton(option.title) { start(option.source) }
                }
                secondaryButton("Open another vault file…") { message = openAnother() }
            } else {
                PrimaryButton("Open another vault file…", enabled: !isRestoring) { message = openAnother() }
                    .frame(maxWidth: 260)
            }
            if let message { ErrorLine(message) }
            Spacer()
        }
        .padding(32)
    }

    private func secondaryButton(_ title: String, action: @escaping () -> Void) -> some View {
        Button(title, action: action)
            .buttonStyle(.plain).font(Typo.label(12)).foregroundStyle(Palette.textSecondary)
            .disabled(isRestoring)
    }

    private func start(_ source: URL) {
        isRestoring = true
        message = nil
        Task {
            message = await restore(source)
            isRestoring = false
        }
    }
}
