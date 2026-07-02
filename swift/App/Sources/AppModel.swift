import Foundation
import AppKit
import LocalAuthentication
import SwiftUI
import UniformTypeIdentifiers
import TesseraCore
import TesseraArgon2

/// Central app state: vault lifecycle, account list, live codes. The vault is
/// created silently on first run and opened automatically from the Keychain; an
/// optional "Require Touch ID" setting gates that open with biometrics.
@MainActor
final class AppModel: ObservableObject {
    @Published var isLocked = true
    @Published var isOpening = true
    @Published var needsReset = false        // vault on disk but no way to decrypt it here
    @Published var needsPassphrase = false   // vault openable with its passphrase (e.g. CLI-created)
    @Published var accounts: [Account] = [] {
        didSet {
            let f = Array(Set(accounts.map(\.folder).filter { !$0.isEmpty })).sorted()
            if f != folders { folders = f }
        }
    }
    /// Distinct non-empty folder names; derived on account changes, not per render tick.
    @Published private(set) var folders: [String] = []
    @Published var now = Date()
    @Published var errorMessage: String?
    @Published var status: String?          // transient success/info feedback
    @Published var vaultExists: Bool
    @Published var lastImportSkipped = 0
    @Published var requireBiometrics: Bool

    static let biometricsPrefKey = "tessera.requireTouchID"
    static let compactPrefKey = "tessera.compact"

    private let store = VaultStore()
    private let argon2 = Argon2Reference()
    private let isDemo: Bool
    private var envelope: Envelope?
    private var dek: Data?            // held after unlock; enables edits with no prompt
    private var appKey: Data?         // held after unlock; needed to retoggle Touch ID
    private var timer: Timer?

    init() {
        isDemo = false
        vaultExists = store.exists
        let pref = UserDefaults.standard.bool(forKey: AppModel.biometricsPrefKey)
        requireBiometrics = pref && !CommandLine.arguments.contains("-uitest")
        startTicking()
        // Lock on sleep only when Touch ID is required; otherwise the macOS login
        // session is the gate and the vault reopens automatically.
        NSWorkspace.shared.notificationCenter.addObserver(
            forName: NSWorkspace.willSleepNotification, object: nil, queue: .main
        ) { [weak self] _ in Task { @MainActor in if self?.requireBiometrics == true { self?.lock() } } }
    }

    /// Touch ID can gate the vault only on a Mac with both Secure Enclave and a
    /// usable biometric sensor.
    var biometricsAvailable: Bool { SecureEnclaveWrap.isAvailable && AppKey.biometricsAvailable }

    /// In-memory model for screenshots/previews; never touches disk or Keychain.
    enum DemoState { case populated, empty, locked, fresh }
    init(demo: DemoState) {
        isDemo = true
        isOpening = false
        requireBiometrics = true
        switch demo {
        case .populated:
            vaultExists = true; isLocked = false
            accounts = AppModel.sampleAccounts
        case .empty:
            vaultExists = true; isLocked = false; accounts = []
        case .locked:
            vaultExists = true; isLocked = true
        case .fresh:
            vaultExists = false; isLocked = true
        }
    }

    nonisolated static var sampleAccounts: [Account] {
        func s(_ str: String) -> Data { Data(str.utf8) }
        return [
            Account(id: "1", type: .totp, issuer: "GitHub", account: "ibrahem", secret: s("12345678901234567890"), algorithm: "SHA1", digits: 6, period: 30, pinned: true),
            Account(id: "2", type: .totp, issuer: "Cloudflare", account: "ibra@ibrahemid.com", secret: s("abcdefghij1234567890"), algorithm: "SHA1", digits: 6, period: 30, pinned: true),
            Account(id: "3", type: .totp, issuer: "Google", account: "you@gmail.com", secret: s("zyxwvutsrqponmlkjihg"), algorithm: "SHA1", digits: 6, period: 30),
            Account(id: "4", type: .steam, issuer: "Steam", account: "ibrahem", secret: s("steamsecret12345"), algorithm: "SHA1", digits: 5, period: 30),
            Account(id: "5", type: .totp, issuer: "AWS", account: "root", secret: s("awssecretkey00000000"), algorithm: "SHA256", digits: 6, period: 30),
            Account(id: "6", type: .hotp, issuer: "Bank", account: "•••• 4291", secret: s("banksecret0000000000"), algorithm: "SHA1", digits: 6, counter: 12),
        ]
    }

    // MARK: Open / unlock

    /// Called once when the window appears: create the vault on first run, or open
    /// the existing one. Silent unless "Require Touch ID" is on.
    func openOnLaunch() async {
        guard !isDemo else { return }
        await open(reason: "Unlock your Tessera vault")
    }

    /// Re-open after a manual lock.
    func unlock() {
        Task { await open(reason: "Unlock your Tessera vault") }
    }

    private func open(reason: String) async {
        guard isLocked, !needsReset, !needsPassphrase else { isOpening = false; return }
        do {
            if !store.exists {
                try await createFreshVault()
            } else {
                let env = try store.load()
                // No wrap this device can satisfy silently. A passphrase wrap
                // (e.g. a vault created by the tess CLI) can still be opened by
                // asking for it; only a vault with neither is unrecoverable.
                if !SecureEnclaveWrap.hasWrap(env) && !AppKey.exists {
                    if hasPassphraseWrap(env) { needsPassphrase = true } else { needsReset = true }
                    isOpening = false; return
                }
                try await openLoaded(env, reason: reason)
            }
            errorMessage = nil
            isLocked = false
        } catch {
            // The Keychain app key didn't open this vault: it belongs to another
            // passphrase (a CLI vault pointed at via TESSERA_VAULT). Ask for it
            // instead of dead-ending.
            if case VaultError.wrongPassphrase = error,
               let env = try? store.load(), !SecureEnclaveWrap.hasWrap(env), hasPassphraseWrap(env) {
                needsPassphrase = true
            } else {
                errorMessage = friendly(error)
            }
        }
        isOpening = false
    }

    private func hasPassphraseWrap(_ env: Envelope) -> Bool {
        env.wraps.contains { $0.type == "passphrase" }
    }

    /// Open a vault this Mac has no key for (e.g. created by the tess CLI) with
    /// its passphrase. On success a Secure Enclave wrap is added alongside the
    /// passphrase wrap, so future opens are silent and the CLI keeps working.
    func unlockWithPassphrase(_ passphrase: String) async {
        guard !passphrase.isEmpty else { return }
        errorMessage = nil
        do {
            let env = try store.load()
            let a = argon2
            let req = requireBiometrics
            let (updated, seAdded, dek, accts) = try await Task.detached(priority: .userInitiated) { () -> (Envelope, Bool, Data, [Account]) in
                let dek = try env.recoverDEK(passphrase: passphrase, argon2: a)
                let accts = try env.open(dek: dek)
                var updated = env
                var seAdded = false
                if SecureEnclaveWrap.isAvailable {
                    do { try SecureEnclaveWrap.enable(on: &updated, dek: dek, requireBiometrics: req); seAdded = true }
                    catch {}   // still unlocked this session; next launch asks again
                }
                return (updated, seAdded, dek, accts)
            }.value
            if seAdded { try? store.save(updated) }
            self.envelope = seAdded ? updated : env
            self.dek = dek; self.appKey = nil; self.accounts = accts
            needsPassphrase = false
            isLocked = false
        } catch {
            errorMessage = friendly(error)
        }
    }

    /// First run: random DEK wrapped by the Secure Enclave (instant, hardware-
    /// bound, no argon2). On the rare SE-less Mac, fall back to a Keychain key
    /// behind a passphrase wrap.
    private func createFreshVault() async throws {
        if SecureEnclaveWrap.isAvailable {
            let req = requireBiometrics
            let (env, dek) = try await Task.detached(priority: .userInitiated) { () -> (Envelope, Data) in
                let made = try Envelope.createUnwrapped(accounts: [])
                var env = made.0
                try SecureEnclaveWrap.enable(on: &env, dek: made.1, requireBiometrics: req)
                return (env, made.1)
            }.value
            try store.save(env)
            self.envelope = env; self.dek = dek; self.appKey = nil
        } else {
            let a = argon2
            let (env, dek, key) = try await Task.detached(priority: .userInitiated) { () -> (Envelope, Data, Data) in
                let key = try AppKey.create(requireBiometrics: false)
                let pass = key.base64EncodedString()
                let env = try Envelope.create(accounts: [], passphrase: pass, argon2: a)
                let dek = try env.recoverDEK(passphrase: pass, argon2: a)
                return (env, dek, key)
            }.value
            try store.save(env)
            self.envelope = env; self.dek = dek; self.appKey = key
        }
        self.accounts = []; self.vaultExists = true
    }

    private func openLoaded(_ env: Envelope, reason: String) async throws {
        if SecureEnclaveWrap.hasWrap(env) {
            let (dek, accts) = try await Task.detached(priority: .userInitiated) { () -> (Data, [Account]) in
                let dek = try SecureEnclaveWrap.open(env, reason: reason)  // prompts only if biometric
                return (dek, try env.open(dek: dek))
            }.value
            self.envelope = env; self.dek = dek; self.appKey = nil; self.accounts = accts
        } else {
            let a = argon2
            let (key, dek, accts) = try await Task.detached(priority: .userInitiated) { () -> (Data, Data, [Account]) in
                let key = try AppKey.read(reason: reason)
                let dek = try env.recoverDEK(passphrase: key.base64EncodedString(), argon2: a)
                return (key, dek, try env.open(dek: dek))
            }.value
            self.envelope = env; self.dek = dek; self.appKey = key; self.accounts = accts
        }
    }

    func lock() {
        guard !isLocked else { return }
        accounts = []
        dek = nil
        appKey = nil
        envelope = nil
        isLocked = true
        errorMessage = nil
        status = nil
    }

    // MARK: Touch ID setting

    /// Toggle whether opening the vault requires Touch ID. Rewraps the DEK under a
    /// Secure Enclave key with or without the biometric flag (turning it on
    /// prompts once to confirm). Falls back to the Keychain item on SE-less Macs.
    func setRequireBiometrics(_ on: Bool) {
        Task { await applyRequireBiometrics(on) }
    }

    private func applyRequireBiometrics(_ on: Bool) async {
        guard let env = envelope, let dek = dek else {
            errorMessage = "\(VaultError.wrongPassphrase)"; return
        }
        do {
            if SecureEnclaveWrap.isAvailable {
                // Rewrap off the main thread: turning Touch ID on prompts here.
                let newEnv = try await Task.detached(priority: .userInitiated) { () -> Envelope in
                    var e = env
                    SecureEnclaveWrap.disable(on: &e)
                    try SecureEnclaveWrap.enable(on: &e, dek: dek, requireBiometrics: on)
                    return e
                }.value
                try store.save(newEnv)
                self.envelope = newEnv
            } else if let key = appKey {
                do { try AppKey.store(key, requireBiometrics: on) }
                catch {
                    // Put the key back under the previous gating; the toggle
                    // must never cost the only unlock path.
                    try? AppKey.store(key, requireBiometrics: requireBiometrics)
                    throw error
                }
            } else {
                throw VaultError.wrongPassphrase
            }
            requireBiometrics = on
            UserDefaults.standard.set(on, forKey: AppModel.biometricsPrefKey)
            errorMessage = nil
        } catch {
            errorMessage = friendly(error)
        }
    }

    // MARK: Reset & backup

    /// Delete the Keychain key and vault file, then start a fresh empty vault.
    func resetTessera() {
        AppKey.delete()
        try? FileManager.default.removeItem(at: store.vaultURL)
        envelope = nil; dek = nil; appKey = nil; accounts = []
        vaultExists = false; needsReset = false; needsPassphrase = false; isLocked = true
        errorMessage = nil; isOpening = true
        Task { await open(reason: "Unlock your Tessera vault") }
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
            errorMessage = friendly(error)
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
            errorMessage = "Couldn't read that file"; return false
        }
        errorMessage = nil
        do {
            let a = argon2
            let imported = try await Task.detached(priority: .userInitiated) { () -> [Account] in
                let env = try Envelope.decode(data)
                return try env.open(passphrase: passphrase, argon2: a)
            }.value
            let (added, _) = try merge(imported, resetIDs: true)
            status = added == 0 ? "Those accounts are already here" : "Restored \(added) account\(added == 1 ? "" : "s")"
            return added > 0
        } catch {
            errorMessage = friendly(error)
            return false
        }
    }

    /// Export every account as plaintext otpauth:// links — the universal format
    /// any authenticator can import. The secrets are readable, so this warns.
    @discardableResult
    func exportPlaintextLinks() -> Bool {
        guard !accounts.isEmpty else { return false }
        let body = accounts.map { OTPAuth.format($0) }.joined(separator: "\n") + "\n"
        guard let url = promptSave(name: "tessera-accounts.txt", types: [.plainText],
                                   message: "Plain text — the secret keys are readable. Anyone with this file can generate your codes.") else { return false }
        var ok = false
        run {
            try Data(body.utf8).write(to: url, options: [.atomic])
            status = "Exported \(accounts.count) link\(accounts.count == 1 ? "" : "s")"
            ok = true
        }
        return ok
    }

    private func promptSave(name: String, types: [UTType], message: String) -> URL? {
        let panel = NSSavePanel()
        panel.nameFieldStringValue = name
        panel.allowedContentTypes = types
        panel.message = message
        return panel.runModal() == .OK ? panel.url : nil
    }

    private func promptOpen(types: [UTType], message: String, multiple: Bool = false, allowsOther: Bool = false) -> [URL] {
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

    /// The account's raw secret as a base32 setup key (what you type into another
    /// app's manual-entry screen).
    func secretBase32(for account: Account) -> String { Base32.encodeNoPad(account.secret) }

    // MARK: Account operations

    func addAccount(_ account: Account) { mutate { $0.append(stamped(account)) } }

    /// Add one account from manually typed fields (the "setup key" path). The
    /// secret is base32 as shown by the service. Validates and rejects duplicates.
    func addManual(type: OTPType, issuer: String, account: String, secretBase32: String,
                   algorithm: String, digits: Int, period: Int, counter: Int64) {
        run {
            let secret = try Base32.decode(secretBase32)   // lenient: case, spaces, dashes
            var a = Account(id: "", type: type,
                            issuer: issuer.trimmingCharacters(in: .whitespaces),
                            account: account.trimmingCharacters(in: .whitespaces),
                            secret: secret, algorithm: algorithm, digits: digits, period: period)
            if type == .hotp { a.counter = counter }
            a = stamped(a)
            try a.validate()
            let key = dedupeKey(a)
            if accounts.contains(where: { dedupeKey($0) == key }) {
                throw AccountError.invalid("this account is already in your vault")
            }
            try persist(accounts + [a])
            status = "Added \(a.displayName)"   // same feedback as link / scan import
        }
    }

    /// Import every QR code from one or more image files (a screenshot may hold
    /// several codes, e.g. a Google Authenticator export).
    @discardableResult
    func importQRImage() -> Bool {
        let urls = promptOpen(types: [.png, .jpeg, .tiff, .image],
                              message: "Choose one or more images that contain 2FA QR codes.", multiple: true)
        guard !urls.isEmpty else { return false }
        var payloads: [String] = []
        var lastError: Error?
        for url in urls {
            guard let img = NSImage(contentsOf: url),
                  let cg = img.cgImage(forProposedRect: nil, context: nil, hints: nil) else { continue }
            do { payloads.append(contentsOf: try QRCapture.detectAll(in: cg)) }
            catch { lastError = error }
        }
        if payloads.isEmpty {
            // Don't mask a detection failure as "no QR found".
            errorMessage = lastError.flatMap(friendly) ?? "No 2FA QR codes found in those images"
            return false
        }
        return importReporting(payloads.joined(separator: "\n"))
    }

    /// Import otpauth/migration text (multi-line), set a status summary, and
    /// return true if at least one account was added. The single entry point that
    /// guarantees the user always gets feedback (added, all-duplicates, or error).
    @discardableResult
    func importReporting(_ text: String) -> Bool {
        status = nil
        let added = importText(text)
        if errorMessage != nil { return false }
        return reportImport(added: added, skipped: lastImportSkipped)
    }

    /// The one summary rule for every bulk-import path.
    private func reportImport(added: Int, skipped: Int) -> Bool {
        if added == 0 && skipped == 0 { errorMessage = "Nothing to import"; return false }
        var parts: [String] = []
        if added > 0 { parts.append("Added \(added)") }
        if skipped > 0 { parts.append("skipped \(skipped) duplicate\(skipped == 1 ? "" : "s")") }
        status = parts.joined(separator: ", ")
        return added > 0
    }

    /// Remove accounts that duplicate another (same type, issuer, account, secret),
    /// keeping the first. Returns the number removed.
    @discardableResult
    func removeDuplicates() -> Int {
        var removed = 0
        run {
            var seen = Set<String>(), next: [Account] = []
            for a in accounts {
                let k = dedupeKey(a)
                if seen.contains(k) { removed += 1; continue }
                seen.insert(k); next.append(a)
            }
            if removed > 0 { try persist(next) }
        }
        return removed
    }

    /// Persisted manual reordering of the account list (the payload array order).
    func move(fromOffsets: IndexSet, toOffset: Int) {
        mutate { $0.move(fromOffsets: fromOffsets, toOffset: toOffset) }
    }

    /// The cleartext otpauth:// URI for an account (for QR export / move to phone).
    func otpauthURI(for account: Account) -> String { OTPAuth.format(account) }

    /// Bulk import: accepts one or many lines, each an otpauth:// or
    /// otpauth-migration:// URI. Skips duplicates. Reports the count via status.
    @discardableResult
    func importText(_ text: String) -> Int {
        var added = 0
        run {
            var parsed: [Account] = []
            for raw in text.split(whereSeparator: \.isNewline) {
                let line = raw.trimmingCharacters(in: .whitespaces)
                if line.isEmpty || line.hasPrefix("#") { continue }
                if line.hasPrefix("otpauth-migration://") {
                    parsed.append(contentsOf: try Migration.parse(line))
                } else if line.hasPrefix("otpauth://") {
                    parsed.append(try OTPAuth.parse(line))
                } else {
                    throw AccountError.invalid("not an otpauth or otpauth-migration link")
                }
            }
            if parsed.isEmpty { throw AccountError.invalid("nothing to import") }
            let (a, skipped) = try merge(parsed)
            added = a
            lastImportSkipped = skipped
        }
        return added
    }

    /// Open a file and bulk-import it: an Aegis / 2FAS / Raivo export (same
    /// importers as the CLI), or a text file of otpauth links, one per line.
    @discardableResult
    func importFromFile() -> Bool {
        let urls = promptOpen(types: [.plainText, .text, .json],
                              message: "Choose an app export (Aegis, 2FAS, Raivo) or a file of otpauth:// links.",
                              allowsOther: true)
        guard let url = urls.first else { return false }
        guard let data = try? Data(contentsOf: url) else {
            errorMessage = "Couldn't read that file"; return false
        }
        // Recognized app exports first; their errors (e.g. "export is encrypted")
        // beat a generic line-parse failure.
        var imported: [Account]?
        do {
            if let found = try Importers.parse(data) { imported = found.accounts }
        } catch {
            errorMessage = friendly(error)
            return false
        }
        if let imported {
            status = nil
            var added = 0, skipped = 0
            run { (added, skipped) = try merge(imported) }
            guard errorMessage == nil else { return false }
            return reportImport(added: added, skipped: skipped)
        }
        guard let text = String(data: data, encoding: .utf8) else {
            errorMessage = "Couldn't read that file"; return false
        }
        return importReporting(text)
    }

    private func dedupeKey(_ a: Account) -> String {
        [String(describing: a.type), a.issuer.lowercased(), a.account.lowercased(),
         Base32.encodeNoPad(a.secret)].joined(separator: "|")
    }

    func remove(_ account: Account) { mutate { $0.removeAll { $0.id == account.id } } }

    func togglePin(_ account: Account) {
        mutate { accts in
            if let i = accts.firstIndex(where: { $0.id == account.id }) {
                accts[i].pinned.toggle()
                accts[i].updatedAt = Int64(Date().timeIntervalSince1970)
            }
        }
    }

    /// Assign an account to a list (folder). Passing "" removes it from any list.
    /// Lists exist exactly as long as an account references them.
    func setFolder(_ account: Account, to folder: String) {
        let name = folder.trimmingCharacters(in: .whitespaces)
        mutate { accts in
            if let i = accts.firstIndex(where: { $0.id == account.id }) {
                accts[i].folder = name
                accts[i].updatedAt = Int64(Date().timeIntervalSince1970)
            }
        }
        status = name.isEmpty ? "Removed from list" : "Added to \(name)"
    }

    func advanceHOTP(_ account: Account) {
        guard account.type == .hotp else { return }
        mutate { accts in
            if let i = accts.firstIndex(where: { $0.id == account.id }) {
                accts[i].counter += 1
                accts[i].updatedAt = Int64(Date().timeIntervalSince1970)
            }
        }
        guard errorMessage == nil else { return }   // the advance didn't persist; no success feedback
        // Copy the freshly advanced code, with the same feedback as a TOTP copy.
        if let updated = accounts.first(where: { $0.id == account.id }),
           let code = try? OTP.code(for: updated, at: now) {
            NSPasteboard.general.clearContents()
            NSPasteboard.general.setString(code, forType: .string)
            status = "Copied \(account.displayName)"
        }
    }

    var vaultPathDisplay: String {
        store.vaultURL.path.replacingOccurrences(of: NSHomeDirectory(), with: "~")
    }

    func code(for account: Account) -> String {
        (try? OTP.code(for: account, at: now)) ?? "------"
    }

    func remaining(for account: Account) -> Int { OTP.remainingSeconds(time: now, period: account.period) }

    // MARK: Helpers

    private func mutate(_ change: (inout [Account]) -> Void) {
        run {
            var next = accounts
            change(&next)
            try persist(next)
        }
    }

    private func persist(_ next: [Account]) throws {
        guard let env = envelope, let dek = dek else { throw VaultError.wrongPassphrase }
        let updated = try env.reseal(accounts: next, dek: dek)
        try store.save(updated)
        self.envelope = updated
        self.accounts = next
    }

    private func stamped(_ a: Account) -> Account {
        var x = a
        if x.id.isEmpty { x.id = UUID().uuidString }
        let ts = Int64(Date().timeIntervalSince1970)
        if x.createdAt == 0 { x.createdAt = ts }
        x.updatedAt = ts
        return x
    }

    private func run(_ body: () throws -> Void) {
        do { errorMessage = nil; try body() }
        catch { errorMessage = friendly(error) }
    }

    /// User-cancelled biometrics is not an error worth surfacing.
    private func friendly(_ error: Error) -> String? {
        if case AppKey.AppKeyError.keychain(let s) = error, s == errSecUserCanceled { return nil }
        if let la = error as? LAError, [.userCancel, .systemCancel, .appCancel].contains(la.code) { return nil }
        let ns = error as NSError
        if ns.domain == LAErrorDomain, [-2, -4, -9].contains(ns.code) { return nil }
        if ns.code == Int(errSecUserCanceled) { return nil }
        return "\(error)"
    }

    private func startTicking() {
        timer = Timer.scheduledTimer(withTimeInterval: 1, repeats: true) { [weak self] _ in
            Task { @MainActor in self?.now = Date() }
        }
    }
}
