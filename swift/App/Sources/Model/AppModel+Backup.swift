import Foundation
import AppKit
import UniformTypeIdentifiers
import TesseraCore

extension AppModel {
    // MARK: Reset & backup

    /// Start over. For the built-in vault this deletes the Keychain key and the
    /// vault file. For an external vault shared with the CLI, it only detaches
    /// (drops the bookmark) — the app never deletes a file the CLI owns.
    func resetTessera() {
        pendingSwitchMessage = nil
        if store.isExternal {
            store.clearExternal()
        } else {
            AppKey.delete()
            try? FileManager.default.removeItem(at: store.vaultURL)
        }
        envelope = nil; dek = nil; appKey = nil; accounts = []
        needsReset = false; needsPassphrase = false; needsRecovery = false
        passphraseIsRecovery = false
        vaultUnreachable = false; isLocked = true
        vaultExists = store.exists; errorMessage = nil; isOpening = true
        Task { await open(reason: "Unlock your Tessera vault") }
    }

    /// What reset does here, which differs by vault source: an external file the
    /// CLI owns is only disconnected, never deleted.
    var resetActionTitle: String {
        isExternalVault ? "Disconnect vault" : "Delete vault and start over"
    }

    var resetWarning: String {
        isExternalVault
            ? "Disconnects the vault file; the file itself is not deleted."
            : "Deletes all accounts and the vault key on this Mac."
    }

    /// Write an encrypted, passphrase-wrapped copy of the current accounts to a
    /// file the user picks. Restorable with that passphrase by the `tess` CLI.
    /// The panel comes first so a cancel never pays the argon2 cost, which runs
    /// off the main thread.
    @discardableResult
    func exportBackup(passphrase: String) async -> Bool {
        guard let url = promptSave(name: "tessera-backup.json", types: [.json],
                                   message: "Encrypted with this password. Keep the file and password safe.") else { return false }
        errorMessage = nil
        do {
            let accts = accounts
            let a = argon2
            let data = try await Task.detached(priority: .userInitiated) {
                try Envelope.create(accounts: accts, passphrase: passphrase, argon2: a).encoded()
            }.value
            try data.write(to: url, options: [.atomic])
            return true
        } catch {
            errorMessage = UserMessage.textUnlessCancelled(for: error, context: .save, url: url)
            return false
        }
    }

    /// Restore accounts from a Tessera encrypted backup. Decrypts with the backup
    /// password (argon2 off the main thread) and merges into the current vault,
    /// skipping exact duplicates. Reuses the same Envelope format the CLI
    /// reads/writes — no format change.
    @discardableResult
    func restoreBackup(passphrase: String) async -> Bool {
        guard let url = promptOpen(types: [.json], message: "Choose a Tessera encrypted backup (.json).").first else { return false }
        guard let data = try? Data(contentsOf: url) else {
            errorMessage = "Couldn't read that file."; return false
        }
        errorMessage = nil
        let imported: [Account]
        do {
            let a = argon2
            imported = try await Task.detached(priority: .userInitiated) { () -> [Account] in
                let env = try Envelope.decode(data)
                return try env.open(passphrase: passphrase, argon2: a)
            }.value
        } catch {
            errorMessage = UserMessage.text(for: error, context: .restore)
            return false
        }
        do {
            let (added, _) = try merge(imported, resetIDs: true)
            notice = .info(added == 0 ? "Those accounts are already here" : "Restored \(added) account\(added == 1 ? "" : "s")")
            return added > 0
        } catch {
            errorMessage = UserMessage.textUnlessCancelled(for: error, context: .save)
            return false
        }
    }

    /// Export every account as plaintext otpauth:// links — the universal format
    /// any authenticator can import. The secrets are readable, so this warns.
    func exportPlaintextLinks() -> Notice? {
        guard !accounts.isEmpty else { return nil }
        let body = accounts.map { OTPAuth.format($0) }.joined(separator: "\n") + "\n"
        guard let url = promptSave(name: "tessera-accounts.txt", types: [.plainText],
                                   message: "Plain text — the secret keys are readable. Anyone with this file can generate your codes.") else { return nil }
        do {
            try Data(body.utf8).write(to: url, options: [.atomic])
            return .info("Exported \(accounts.count) link\(accounts.count == 1 ? "" : "s")")
        } catch {
            return .failure(UserMessage.text(for: error, context: .save, url: url))
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

    /// Merge parsed accounts into the vault, skipping exact duplicates. The one
    /// place the dedupe/stamp/validate/persist sequence lives.
    private func merge(_ parsed: [Account], resetIDs: Bool = false) throws -> (added: Int, skipped: Int) {
        var seen = Set(accounts.map(dedupeKey))
        var next = accounts
        var added = 0
        for var a in parsed {
            let key = dedupeKey(a)
            if seen.contains(key) { continue }
            seen.insert(key)
            if resetIDs { a.id = "" }        // fresh id so a restore never collides
            a = stamped(a)
            try a.validate()
            next.append(a)
            added += 1
        }
        if added > 0 { try persist(next) }
        return (added, parsed.count - added)
    }
}
