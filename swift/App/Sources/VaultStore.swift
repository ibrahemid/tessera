import AppKit
import Foundation
import TesseraCore

/// Loads and saves the encrypted envelope. By default the app keeps its vault in
/// its own Application Support container, which is always writable under the App
/// Store sandbox. Power users can point both the app and the `tess` CLI at a
/// shared path via the TESSERA_VAULT environment variable.
final class VaultStore {
    private let fm = FileManager.default

    var vaultURL: URL {
        if let env = ProcessInfo.processInfo.environment["TESSERA_VAULT"], !env.isEmpty {
            return URL(fileURLWithPath: (env as NSString).expandingTildeInPath)
        }
        let support = (try? fm.url(for: .applicationSupportDirectory, in: .userDomainMask,
                                   appropriateFor: nil, create: true))
            ?? fm.homeDirectoryForCurrentUser.appendingPathComponent("Library/Application Support")
        return support.appendingPathComponent("Tessera/vault.json")
    }

    var exists: Bool { fm.fileExists(atPath: vaultURL.path) }

    func load() throws -> Envelope {
        try Envelope.decode(try Data(contentsOf: vaultURL))
    }

    func save(_ env: Envelope) throws {
        let url = vaultURL
        try fm.createDirectory(at: url.deletingLastPathComponent(),
                               withIntermediateDirectories: true,
                               attributes: [.posixPermissions: 0o700])
        try env.encoded().write(to: url, options: [.atomic, .completeFileProtection])
        try? fm.setAttributes([.posixPermissions: 0o600], ofItemAtPath: url.path)
    }
}
