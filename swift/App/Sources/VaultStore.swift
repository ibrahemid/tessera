import AppKit
import Foundation
import TesseraCore

/// Loads and saves the encrypted envelope. The vault is resolved in this order:
///
/// 1. A persisted security-scoped bookmark (`vaultBookmark`) to a file the user
///    picked — typically a vault shared with the `tess` CLI. Load/save run inside
///    a security-scoped access span; a stale bookmark is refreshed in place.
/// 2. The `TESSERA_VAULT` environment variable (dev / CLI launches).
/// 3. The app's own Application Support container (always writable under the App
///    Store sandbox).
///
/// A bookmark that is configured but cannot be resolved is expressed by
/// `hasBookmark` + `isExternal`; the pure `VaultOpen.preLoad` decision maps those
/// to a "relocate" state so the app never silently falls back to the container.
final class VaultStore {
    private let fm = FileManager.default
    static let bookmarkKey = "vaultBookmark"

    /// The resolved external vault URL, if a bookmark is configured and resolves.
    private(set) var externalURL: URL?

    init() { resolveBookmark() }

    /// A bookmark is configured (whether or not it currently resolves).
    var hasBookmark: Bool { UserDefaults.standard.data(forKey: Self.bookmarkKey) != nil }
    /// Running against a resolved external vault.
    var isExternal: Bool { externalURL != nil }

    private func resolveBookmark() {
        externalURL = nil
        guard let data = UserDefaults.standard.data(forKey: Self.bookmarkKey) else { return }
        var stale = false
        guard let url = try? URL(resolvingBookmarkData: data, options: [.withSecurityScope],
                                 relativeTo: nil, bookmarkDataIsStale: &stale) else { return }
        externalURL = url
        if stale { refreshBookmark(url) }
    }

    private func refreshBookmark(_ url: URL) {
        let scoped = url.startAccessingSecurityScopedResource()
        defer { if scoped { url.stopAccessingSecurityScopedResource() } }
        if let data = try? url.bookmarkData(options: [.withSecurityScope],
                                            includingResourceValuesForKeys: nil, relativeTo: nil) {
            UserDefaults.standard.set(data, forKey: Self.bookmarkKey)
        }
    }

    /// Persist a security-scoped bookmark to a user-picked vault file and switch to
    /// it. Returns false if the bookmark can't be created.
    @discardableResult
    func setExternal(_ url: URL) -> Bool {
        let scoped = url.startAccessingSecurityScopedResource()
        defer { if scoped { url.stopAccessingSecurityScopedResource() } }
        guard let data = try? url.bookmarkData(options: [.withSecurityScope],
                                               includingResourceValuesForKeys: nil, relativeTo: nil) else {
            return false
        }
        UserDefaults.standard.set(data, forKey: Self.bookmarkKey)
        externalURL = url
        return true
    }

    /// Drop the external bookmark and return to the built-in container vault. Never
    /// touches the external file — it belongs to the CLI.
    func clearExternal() {
        UserDefaults.standard.removeObject(forKey: Self.bookmarkKey)
        externalURL = nil
    }

    var vaultURL: URL {
        if let url = externalURL { return url }
        if let env = ProcessInfo.processInfo.environment["TESSERA_VAULT"], !env.isEmpty {
            return URL(fileURLWithPath: (env as NSString).expandingTildeInPath)
        }
        return containerVaultURL
    }

    /// The app's own vault under the sandbox container, regardless of any external
    /// bookmark. Used to name the built-in path when offering to switch back to it.
    var containerVaultURL: URL {
        let support = (try? fm.url(for: .applicationSupportDirectory, in: .userDomainMask,
                                   appropriateFor: nil, create: true))
            ?? fm.homeDirectoryForCurrentUser.appendingPathComponent("Library/Application Support")
        return support.appendingPathComponent("Tessera/vault.json")
    }

    var exists: Bool { withAccess { fm.fileExists(atPath: vaultURL.path) } }

    /// Last-modified time of the vault file, used to detect an external rewrite by
    /// the CLI.
    var modifiedAt: Date? {
        withAccess { (try? fm.attributesOfItem(atPath: vaultURL.path)[.modificationDate]) as? Date }
    }

    func load() throws -> Envelope {
        try withAccess { try Envelope.decode(try Data(contentsOf: vaultURL)) }
    }

    func save(_ env: Envelope) throws {
        try withAccess {
            let url = vaultURL
            if externalURL == nil {
                try fm.createDirectory(at: url.deletingLastPathComponent(),
                                       withIntermediateDirectories: true,
                                       attributes: [.posixPermissions: 0o700])
                try env.encoded().write(to: url, options: [.atomic, .completeFileProtection])
                try? fm.setAttributes([.posixPermissions: 0o600], ofItemAtPath: url.path)
            } else {
                // Shared file the tess CLI also writes: atomic write, no data
                // protection class (which can fail outside the container).
                try env.encoded().write(to: url, options: [.atomic])
            }
        }
    }

    /// Run `body` with security-scoped access held for an external vault. A
    /// non-throwing closure satisfies the `rethrows` signature, so `exists`,
    /// `modifiedAt`, `load`, and `save` all share this one wrapper.
    private func withAccess<T>(_ body: () throws -> T) rethrows -> T {
        guard let url = externalURL else { return try body() }
        let scoped = url.startAccessingSecurityScopedResource()
        defer { if scoped { url.stopAccessingSecurityScopedResource() } }
        return try body()
    }
}
