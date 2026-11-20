import Darwin
import Foundation
import TesseraCore

enum AutoBackupError: Error, Equatable, LocalizedError {
    case folderUnavailable, needsPassphrase, write(errno: Int32), prune(failed: Int)

    var errorDescription: String? { UserMessage.text(for: self) }
}

struct BackupResult: Codable, Equatable {
    let at: Date
    let fileName: String?
    let failure: String?
}

actor AutoBackupWriter {
    typealias Remove = @Sendable (_ path: String) -> Int32

    static let systemRemove: Remove = { path in unlink(path) == 0 ? 0 : errno }

    private let remove: Remove

    init(remove: @escaping Remove = AutoBackupWriter.systemRemove) {
        self.remove = remove
    }

    func write(_ envelope: Envelope, rotated: Bool, at: Date, folder: URL,
               vault: URL?) -> Result<BackupResult, AutoBackupError> {
        let scoped = folder.startAccessingSecurityScopedResource()
        defer { if scoped { folder.stopAccessingSecurityScopedResource() } }
        guard Self.isFolder(folder) else { return .failure(.folderUnavailable) }
        let data: Data
        do {
            data = try envelope.passphraseWrapsOnly().encoded()
        } catch EnvelopeError.backupNeedsPassphrase {
            return .failure(.needsPassphrase)
        } catch {
            return .failure(.write(errno: EINVAL))
        }
        let name = BackupRotation.fileName(for: at)
        DurableFile.sweepStaleTemps(in: folder)
        do throws(VaultWriteError) {
            try DurableFile.replace(data, at: folder.appendingPathComponent(name, isDirectory: false),
                                    hasDirectoryAccess: true)
        } catch {
            if case .io(_, _, let code) = error { return .failure(.write(errno: code)) }
            return .failure(.write(errno: EIO))
        }
        guard let failed = prune(folder, keep: name, rotated: rotated, today: at, vault: vault) else {
            return .failure(.prune(failed: 0))
        }
        if failed > 0 { return .failure(.prune(failed: failed)) }
        return .success(BackupResult(at: at, fileName: name, failure: nil))
    }

    private func prune(_ folder: URL, keep: String, rotated: Bool, today: Date, vault: URL?) -> Int? {
        guard let names = try? FileManager.default.contentsOfDirectory(atPath: folder.path) else { return nil }
        let vaultID = vault.flatMap { Self.fileID(followingLinksAt: $0.path) }
        let candidates = names.filter { name in
            guard BackupRotation.isBackupName(name) else { return false }
            let path = folder.appendingPathComponent(name, isDirectory: false).path
            var info = stat()
            if lstat(path, &info) == 0, info.st_mode & S_IFMT == S_IFDIR { return false }
            if let vaultID, Self.fileID(followingLinksAt: path) == vaultID { return false }
            return true
        }
        var failed = 0
        for name in BackupRotation.namesToDelete(candidates, keep: keep, rotated: rotated, today: today) {
            let code = remove(folder.appendingPathComponent(name, isDirectory: false).path)
            if code != 0 && code != ENOENT { failed += 1 }
        }
        return failed
    }

    private struct FileID: Equatable {
        let device: dev_t
        let inode: ino_t
    }

    private static func fileID(followingLinksAt path: String) -> FileID? {
        var info = stat()
        guard stat(path, &info) == 0 else { return nil }
        return FileID(device: info.st_dev, inode: info.st_ino)
    }

    private static func isFolder(_ url: URL) -> Bool {
        var info = stat()
        return stat(url.path, &info) == 0 && info.st_mode & S_IFMT == S_IFDIR
    }
}
