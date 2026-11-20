import SwiftUI

struct VaultLocationSection: View {
    let location: VaultLocation
    let displayPath: (URL) -> String
    let grantFolderAccess: @MainActor () -> String?
    @State private var message: String?

    static func accessLine(for location: VaultLocation) -> String {
        switch location.access {
        case .container:
            return "Built-in vault."
        case .folder:
            return "Shared with the tess CLI. Saves use its lock file and keep \(location.fileURL.lastPathComponent).bak."
        case .fileOnly:
            return "Shared with the tess CLI. Without folder access, saves skip the CLI’s lock file and backup."
        }
    }

    static func grantFolder(for location: VaultLocation) -> URL? {
        guard location.access == .fileOnly else { return nil }
        return DurableFile.resolvedTarget(location.fileURL).deletingLastPathComponent()
    }

    var body: some View {
        Section {
            LabeledContent("Location") {
                Text(displayPath(location.fileURL))
                    .font(Typo.code(11))
                    .textSelection(.enabled)
                    .lineLimit(1).truncationMode(.middle)
            }
            Text(Self.accessLine(for: location))
                .font(Typo.label(11)).foregroundStyle(Palette.textSecondary)
                .fixedSize(horizontal: false, vertical: true)
            if let folder = Self.grantFolder(for: location) {
                VaultActionButton(
                    title: "Allow folder access…",
                    subtitle: "Choose \(displayPath(folder))."
                ) { message = grantFolderAccess() }
            }
            if let message { ErrorLine(message) }
        }
        .onChange(of: location) { message = nil }
    }
}
