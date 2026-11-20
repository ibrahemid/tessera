import Foundation
import AppKit
import UniformTypeIdentifiers
import TesseraCore

enum BackupExportOutcome: Equatable {
    case saved(URL)
    case failed(String)
}

enum BackupRestoreOutcome: Equatable {
    case restored(Int)
    /// Every account in the backup is already in the vault.
    case nothingNew(Int)
    case failed(String)

    /// Result line for the screen that started the restore.
    var line: String? {
        switch self {
        case .restored(let n): return "Restored \(n) account\(n == 1 ? "" : "s")."
        case .nothingNew(let n):
            return n == 1 ? "The account in this backup is already in your vault."
                          : "All \(n) accounts in this backup are already in your vault."
        case .failed: return nil
        }
    }
}

/// Checks a file is a Tessera encrypted backup before asking for its password.
enum BackupFile {
    /// Why `url` can't be restored with a password, or nil when it can.
    static func problem(at url: URL) -> String? {
        let scoped = url.startAccessingSecurityScopedResource()
        defer { if scoped { url.stopAccessingSecurityScopedResource() } }
        guard let data = try? Data(contentsOf: url) else {
            return "Couldn’t read \(url.lastPathComponent). Choose another file."
        }
        return problem(in: data)
    }

    static func problem(in data: Data) -> String? {
        guard let env = try? Envelope.decode(data) else {
            return "That file isn’t a Tessera backup. Choose a file made with Export encrypted backup."
        }
        guard env.hasPassphraseWrap else {
            return UserMessage.text(for: VaultError.noPassphraseWrap, context: .restore)
        }
        return nil
    }
}

enum BackupExport {
    static func write(_ accounts: [Account], password: String, argon2: Argon2idProvider, to url: URL) throws {
        let data = try Envelope.create(accounts: accounts, passphrase: password, argon2: argon2).encoded()
        try DurableFile.writePrivate(data, to: url)
    }
}

enum LinkExport {
    static func body(for accounts: [Account]) -> String {
        accounts.map { OTPAuth.format($0) }.joined(separator: "\n") + "\n"
    }

    static func write(_ accounts: [Account], to url: URL) throws(VaultWriteError) {
        try DurableFile.writePrivate(Data(body(for: accounts).utf8), to: url)
    }
}

extension AppModel {
    // MARK: Encrypted backup

    /// Ask where to save a backup. Comes before the password work so a cancel
    /// never pays the argon2 cost.
    func chooseBackupDestination() -> URL? {
        promptSave(name: "tessera-backup.json", types: [.json],
                   message: "Encrypted with the backup password. Keep the file and the password.")
    }

    /// Write an encrypted, password-wrapped copy of the current accounts to
    /// `url`. Restorable with that password here and by the tess CLI. The
    /// argon2 work runs off the main thread.
    func exportBackup(to url: URL, password: String) async -> BackupExportOutcome {
        guard password.count >= PasswordRule.minLength else {
            return .failed(PasswordRule.text(for: .tooShort, noun: "password"))
        }
        let accts = accounts
        let a = argon2
        do {
            try await Task.detached(priority: .userInitiated) {
                try BackupExport.write(accts, password: password, argon2: a, to: url)
            }.value
            backupLedger.recordExport(forVaultAt: store.vaultURL.path)
            return .saved(url)
        } catch {
            return .failed(UserMessage.text(for: error, context: .export, url: url))
        }
    }

    /// Ask for a backup file to restore.
    func chooseBackupFile() -> URL? {
        promptOpen(types: [.json], message: "Choose a Tessera backup.", allowsOther: true).first
    }

    /// Restore accounts from the Tessera encrypted backup at `url`. Decrypts with
    /// the backup password (argon2 off the main thread) and merges into the
    /// current vault, skipping exact duplicates.
    func restoreBackup(from url: URL, password: String) async -> BackupRestoreOutcome {
        let scoped = url.startAccessingSecurityScopedResource()
        let data = try? Data(contentsOf: url)
        if scoped { url.stopAccessingSecurityScopedResource() }
        guard let data else {
            return .failed("Couldn’t read \(url.lastPathComponent). Choose another file.")
        }
        if let problem = BackupFile.problem(in: data) { return .failed(problem) }
        let imported: [Account]
        do {
            let a = argon2
            imported = try await Task.detached(priority: .userInitiated) { () -> [Account] in
                try Envelope.decode(data).open(passphrase: password, argon2: a)
            }.value
        } catch {
            return .failed(UserMessage.text(for: error, context: .restore))
        }
        let candidates: [Account]
        let outcome: CommitOutcome
        do {
            candidates = try restoreCandidates(imported)
            guard !candidates.isEmpty else { return .nothingNew(imported.count) }
            outcome = try await stage(VaultChange.appendingNew(candidates), onFailure: .reload).value
        } catch {
            return .failed(UserMessage.text(for: error, context: .save))
        }
        switch outcome {
        case .committed(let committed):
            let ids = Set(candidates.map(\.id))
            let added = committed.accounts.filter { ids.contains($0.id) }.count
            return added == 0 ? .nothingNew(imported.count) : .restored(added)
        case .failed(let error):
            return .failed(UserMessage.text(for: error, context: .save))
        }
    }

    // MARK: Plain text export

    /// Export every account as plaintext otpauth:// links, the format any
    /// authenticator can import. The secrets are readable, so the panel says so.
    func exportPlaintextLinks() -> Notice? {
        guard !accounts.isEmpty else { return nil }
        let exported = accounts
        guard let url = promptSave(name: "tessera-accounts.txt", types: [.plainText],
                                   message: "Plain text. Anyone with this file can read the setup keys and generate your codes.") else { return nil }
        do {
            try LinkExport.write(exported, to: url)
            return .info("Exported \(exported.count) link\(exported.count == 1 ? "" : "s") to \(url.lastPathComponent)")
        } catch {
            return .failure(UserMessage.text(for: error, context: .export, url: url))
        }
    }

    private func promptSave(name: String, types: [UTType], message: String) -> URL? {
        let panel = NSSavePanel()
        panel.nameFieldStringValue = name
        panel.allowedContentTypes = types
        panel.message = message
        return panel.runModal() == .OK ? panel.url : nil
    }

    func promptOpen(types: [UTType], message: String, multiple: Bool = false, allowsOther: Bool = false) -> [URL] {
        let panel = NSOpenPanel()
        panel.allowedContentTypes = types
        panel.allowsMultipleSelection = multiple
        panel.allowsOtherFileTypes = allowsOther
        panel.message = message
        return panel.runModal() == .OK ? panel.urls : []
    }

    /// Backup accounts not already in the vault, with fresh ids so a restore
    /// never collides, stamped and validated.
    private func restoreCandidates(_ parsed: [Account]) throws -> [Account] {
        var seen = Set(accounts.map(dedupeKey))
        var candidates: [Account] = []
        for var a in parsed where seen.insert(dedupeKey(a)).inserted {
            a.id = ""
            a = stamped(a)
            try a.validate()
            candidates.append(a)
        }
        return candidates
    }
}
