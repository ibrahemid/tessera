import Combine
import Foundation

enum BackupFolder: Equatable {
    case none, unavailable, available(URL)
}

struct BookmarkResolution: Equatable {
    let url: URL
    let isStale: Bool
}

protocol BackupFolderResolving: Sendable {
    func bookmark(for url: URL) -> Data?
    func resolve(_ data: Data) -> BookmarkResolution?
}

struct SecurityScopedBookmarks: BackupFolderResolving {
    func bookmark(for url: URL) -> Data? {
        let scoped = url.startAccessingSecurityScopedResource()
        defer { if scoped { url.stopAccessingSecurityScopedResource() } }
        return try? url.bookmarkData(options: [.withSecurityScope], includingResourceValuesForKeys: nil,
                                     relativeTo: nil)
    }

    func resolve(_ data: Data) -> BookmarkResolution? {
        var stale = false
        guard let url = try? URL(resolvingBookmarkData: data, options: [.withSecurityScope], relativeTo: nil,
                                 bookmarkDataIsStale: &stale) else { return nil }
        return BookmarkResolution(url: url, isStale: stale)
    }
}

@MainActor
final class BackupSettings: ObservableObject {
    static let folderBookmarkKey = "tessera.backupFolderBookmark"
    static let lastResultKey = "tessera.lastAutoBackup"

    @Published private(set) var folder: BackupFolder = .none
    @Published private(set) var lastResult: BackupResult?

    private let defaults: UserDefaults
    private let resolver: any BackupFolderResolving

    init(defaults: UserDefaults = .standard, resolver: any BackupFolderResolving = SecurityScopedBookmarks()) {
        self.defaults = defaults
        self.resolver = resolver
        lastResult = defaults.data(forKey: Self.lastResultKey)
            .flatMap { try? JSONDecoder().decode(BackupResult.self, from: $0) }
        folder = resolveSaved()
    }

    var folderURL: URL? {
        if case .available(let url) = folder { return url }
        return nil
    }

    func chooseFolder(_ url: URL) -> Bool {
        guard let data = resolver.bookmark(for: url), let resolved = resolver.resolve(data) else { return false }
        defaults.set(data, forKey: Self.folderBookmarkKey)
        defaults.removeObject(forKey: Self.lastResultKey)
        lastResult = nil
        folder = .available(resolved.url)
        return true
    }

    func turnOff() {
        defaults.removeObject(forKey: Self.folderBookmarkKey)
        defaults.removeObject(forKey: Self.lastResultKey)
        lastResult = nil
        folder = .none
    }

    func refresh() {
        folder = resolveSaved()
    }

    func record(_ outcome: Result<BackupResult, AutoBackupError>, at: Date) {
        let result = Self.result(of: outcome, at: at)
        lastResult = result
        if let data = try? JSONEncoder().encode(result) {
            defaults.set(data, forKey: Self.lastResultKey)
        }
    }

    static func result(of outcome: Result<BackupResult, AutoBackupError>, at: Date) -> BackupResult {
        switch outcome {
        case .success(let result):
            return result
        case .failure(let error):
            if case .prune = error {
                return BackupResult(at: at, fileName: BackupRotation.fileName(for: at),
                                    failure: UserMessage.text(for: error))
            }
            return BackupResult(at: at, fileName: nil, failure: UserMessage.text(for: error))
        }
    }

    private func resolveSaved() -> BackupFolder {
        guard let data = defaults.data(forKey: Self.folderBookmarkKey) else { return .none }
        guard let resolution = resolver.resolve(data) else { return .unavailable }
        guard resolution.isStale else { return .available(resolution.url) }
        guard let fresh = resolver.bookmark(for: resolution.url) else { return .unavailable }
        defaults.set(fresh, forKey: Self.folderBookmarkKey)
        return .available(resolution.url)
    }
}
