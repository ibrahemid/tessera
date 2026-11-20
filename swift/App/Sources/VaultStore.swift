import Foundation
import TesseraCore

final class VaultStore {
    static let bookmarkKey = "vaultBookmark"
    static let folderBookmarkKey = "vaultFolderBookmark"
    static let fileNameKey = "vaultFileName"
    #if DEBUG
    static let environmentKey = "TESSERA_VAULT"
    #endif

    let containerVaultURL: URL
    private(set) var location: VaultLocation
    private(set) var hasBookmark = false
    private(set) var isExternal = false

    private let defaults: UserDefaults
    #if DEBUG
    private let environment: [String: String]
    #endif
    private var scopedURL: URL?

    init(defaults: UserDefaults = .standard,
         environment: [String: String] = ProcessInfo.processInfo.environment,
         containerVaultURL: URL? = nil) {
        self.defaults = defaults
        #if DEBUG
        self.environment = environment
        #endif
        let container = containerVaultURL ?? Self.defaultContainerVaultURL()
        self.containerVaultURL = container
        location = VaultLocation(fileURL: container, access: .container)
        resolve()
    }

    deinit { stopAccess() }

    var vaultURL: URL { location.fileURL }

    var exists: Bool { FileManager.default.fileExists(atPath: location.fileURL.path) }

    var modifiedAt: Date? { DurableFile.modificationDate(of: DurableFile.resolvedTarget(location.fileURL)) }

    @discardableResult
    func setExternal(_ url: URL) -> Bool {
        guard let data = Self.bookmark(for: url) else { return false }
        defaults.set(data, forKey: Self.bookmarkKey)
        defaults.removeObject(forKey: Self.folderBookmarkKey)
        defaults.removeObject(forKey: Self.fileNameKey)
        resolve()
        return true
    }

    @discardableResult
    func setFolder(_ folder: URL, fileName: String) -> Bool {
        guard Self.isValidFileName(fileName), let data = Self.bookmark(for: folder) else { return false }
        defaults.set(data, forKey: Self.folderBookmarkKey)
        defaults.set(fileName, forKey: Self.fileNameKey)
        defaults.removeObject(forKey: Self.bookmarkKey)
        resolve()
        return true
    }

    func clearExternal() {
        defaults.removeObject(forKey: Self.bookmarkKey)
        defaults.removeObject(forKey: Self.folderBookmarkKey)
        defaults.removeObject(forKey: Self.fileNameKey)
        resolve()
    }

    func load() throws -> Envelope {
        try Envelope.decode(try DurableFile.read(location.fileURL))
    }

    func save(_ env: Envelope) throws {
        let bytes = try env.encoded()
        let target = DurableFile.resolvedTarget(location.fileURL)
        guard location.hasDirectoryAccess else {
            try DurableFile.replace(bytes, at: target, hasDirectoryAccess: false)
            return
        }
        let dir = target.deletingLastPathComponent()
        do {
            try DurableFile.makePrivateDirectory(dir)
        } catch {
            throw VaultWriteError.io(operation: "create folder", url: dir, errno: error.code.rawValue)
        }
        try DurableFile.replace(bytes, at: target, hasDirectoryAccess: true,
                                fileProtection: location.access == .container)
    }

    static func isParentFolder(_ folder: URL, of file: URL) -> Bool {
        canonicalPath(folder) == canonicalPath(file.deletingLastPathComponent())
    }

    static func isValidFileName(_ name: String) -> Bool {
        !name.isEmpty && name != "." && name != ".." && !name.contains("/") && !name.contains("\0")
    }

    private func resolve() {
        stopAccess()
        hasBookmark = false
        isExternal = false
        location = VaultLocation(fileURL: containerVaultURL, access: .container)
        #if DEBUG
        if let path = environment[Self.environmentKey], !path.isEmpty {
            location = VaultLocation(fileURL: URL(fileURLWithPath: (path as NSString).expandingTildeInPath),
                                     access: .folder)
            return
        }
        #endif
        if let data = defaults.data(forKey: Self.folderBookmarkKey) {
            hasBookmark = true
            guard let name = defaults.string(forKey: Self.fileNameKey), Self.isValidFileName(name),
                  let folder = resolveBookmark(data, key: Self.folderBookmarkKey) else { return }
            location = VaultLocation(fileURL: folder.appendingPathComponent(name, isDirectory: false), access: .folder)
            isExternal = true
            return
        }
        if let data = defaults.data(forKey: Self.bookmarkKey) {
            hasBookmark = true
            guard let file = resolveBookmark(data, key: Self.bookmarkKey) else { return }
            location = VaultLocation(fileURL: file, access: .fileOnly)
            isExternal = true
        }
    }

    private func resolveBookmark(_ data: Data, key: String) -> URL? {
        var stale = false
        guard let url = try? URL(resolvingBookmarkData: data, options: [.withSecurityScope],
                                 relativeTo: nil, bookmarkDataIsStale: &stale) else { return nil }
        if url.startAccessingSecurityScopedResource() { scopedURL = url }
        if stale, let fresh = try? url.bookmarkData(options: [.withSecurityScope],
                                                    includingResourceValuesForKeys: nil, relativeTo: nil) {
            defaults.set(fresh, forKey: key)
        }
        return url
    }

    private func stopAccess() {
        scopedURL?.stopAccessingSecurityScopedResource()
        scopedURL = nil
    }

    private static func bookmark(for url: URL) -> Data? {
        let scoped = url.startAccessingSecurityScopedResource()
        defer { if scoped { url.stopAccessingSecurityScopedResource() } }
        return try? url.bookmarkData(options: [.withSecurityScope], includingResourceValuesForKeys: nil, relativeTo: nil)
    }

    private static func canonicalPath(_ url: URL) -> String {
        let resolved = url.standardizedFileURL.resolvingSymlinksInPath().path
        return resolved.count > 1 && resolved.hasSuffix("/") ? String(resolved.dropLast()) : resolved
    }

    private static func defaultContainerVaultURL() -> URL {
        let fm = FileManager.default
        let support = (try? fm.url(for: .applicationSupportDirectory, in: .userDomainMask,
                                   appropriateFor: nil, create: true))
            ?? fm.homeDirectoryForCurrentUser.appendingPathComponent("Library/Application Support")
        return support.appendingPathComponent("Tessera/vault.json")
    }
}
