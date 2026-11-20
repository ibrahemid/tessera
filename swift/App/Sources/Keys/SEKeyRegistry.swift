import Foundation
import os

/// The Secure Enclave key ids this Mac created for each vault file, keyed by
/// the file's real path. A sweep deletes only ids registered to the vault it
/// just opened, so a key that a copy at another path uses is never touched.
struct SEKeyRegistry: @unchecked Sendable {
    static let defaultsKey = "tessera.seKeyRegistry.v1"
    private static let lock = OSAllocatedUnfairLock()

    private let defaults: UserDefaults

    init(defaults: UserDefaults) { self.defaults = defaults }

    static func normalizedPath(_ vaultPath: String) -> String {
        URL(fileURLWithPath: vaultPath).resolvingSymlinksInPath().path
    }

    func register(_ id: SEKeyID, for vaultPath: String) {
        update(vaultPath) { ids in if !ids.contains(id.hex) { ids.append(id.hex) } }
    }

    func unregister(_ id: SEKeyID, for vaultPath: String) {
        update(vaultPath) { ids in ids.removeAll { $0 == id.hex } }
    }

    func ids(for vaultPath: String) -> [SEKeyID] {
        let path = Self.normalizedPath(vaultPath)
        return Self.lock.withLock { load()[path] ?? [] }.compactMap(SEKeyID.init(hex:))
    }

    private func update(_ vaultPath: String, _ change: (inout [String]) -> Void) {
        let path = Self.normalizedPath(vaultPath)
        Self.lock.withLockUnchecked {
            var all = load()
            var ids = all[path] ?? []
            change(&ids)
            all[path] = ids.isEmpty ? nil : ids
            if all.isEmpty {
                defaults.removeObject(forKey: Self.defaultsKey)
            } else {
                defaults.set(all, forKey: Self.defaultsKey)
            }
        }
    }

    private func load() -> [String: [String]] {
        guard let stored = defaults.dictionary(forKey: Self.defaultsKey) else { return [:] }
        var out: [String: [String]] = [:]
        for (path, value) in stored {
            guard let ids = value as? [String] else { continue }
            out[path] = ids.filter { SEKeyID(hex: $0) != nil }
        }
        return out
    }
}
