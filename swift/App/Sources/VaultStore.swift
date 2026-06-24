import AppKit
import Foundation
import TesseraCore

/// Loads and saves the encrypted envelope at the CLI-shared vault path. Under the
/// App Store sandbox, folder access is granted once via a security-scoped
/// bookmark (the vault is NOT relocated into the app container).
final class VaultStore {
    private let bookmarkKey = "tessera.vaultFolderBookmark"
    private let fm = FileManager.default

    /// Canonical shared path: $TESSERA_VAULT or ~/.local/share/tessera/vault.json.
    var vaultURL: URL {
        if let env = ProcessInfo.processInfo.environment["TESSERA_VAULT"], !env.isEmpty {
            return URL(fileURLWithPath: env)
        }
        let home = fm.homeDirectoryForCurrentUser
        return home.appendingPathComponent(".local/share/tessera/vault.json")
    }

    var exists: Bool { fm.fileExists(atPath: vaultURL.path) }

    func load() throws -> Envelope {
        try withFolderAccess { url in
            let data = try Data(contentsOf: url)
            return try Envelope.decode(data)
        }
    }

    func save(_ env: Envelope) throws {
        try withFolderAccessVoid { url in
            try fm.createDirectory(at: url.deletingLastPathComponent(),
                                   withIntermediateDirectories: true,
                                   attributes: [.posixPermissions: 0o700])
            let data = try env.encoded()
            try data.write(to: url, options: [.atomic, .completeFileProtection])
            try fm.setAttributes([.posixPermissions: 0o600], ofItemAtPath: url.path)
        }
    }

    // MARK: Security-scoped folder access

    /// Prompts the user to grant access to the vault's parent folder and persists
    /// a security-scoped bookmark. Call once during onboarding under the sandbox.
    func requestFolderAccess() {
        let panel = NSOpenPanel()
        panel.canChooseDirectories = true
        panel.canChooseFiles = false
        panel.directoryURL = vaultURL.deletingLastPathComponent()
        panel.message = "Grant Tessera access to its vault folder (shared with the tess CLI)."
        if panel.runModal() == .OK, let url = panel.url {
            if let data = try? url.bookmarkData(options: .withSecurityScope) {
                UserDefaults.standard.set(data, forKey: bookmarkKey)
            }
        }
    }

    private func resolveBookmark() -> URL? {
        guard let data = UserDefaults.standard.data(forKey: bookmarkKey) else { return nil }
        var stale = false
        let url = try? URL(resolvingBookmarkData: data, options: .withSecurityScope,
                           relativeTo: nil, bookmarkDataIsStale: &stale)
        return url
    }

    private func withFolderAccess<T>(_ body: (URL) throws -> T) throws -> T {
        if let folder = resolveBookmark() {
            let ok = folder.startAccessingSecurityScopedResource()
            defer { if ok { folder.stopAccessingSecurityScopedResource() } }
            return try body(vaultURL)
        }
        return try body(vaultURL)
    }

    private func withFolderAccessVoid(_ body: (URL) throws -> Void) throws {
        _ = try withFolderAccess { url -> Bool in try body(url); return true }
    }
}
