import Foundation
import AppKit
import LocalAuthentication
import SwiftUI
import UniformTypeIdentifiers
import TesseraCore
import TesseraArgon2

/// One item that did not import, for the per-item result surface. `display` is a
/// redacted preview and never a full secret.
struct ImportItemFailure: Identifiable {
    enum Kind { case noQR, item }
    let id = UUID()
    var source: String    // "Line 3", a file name, or an issuer/account label
    var display: String    // redacted preview, may be empty
    var reason: String
    var kind: Kind = .item
}

/// Combined result of a bulk import across text, images, and files.
struct ImportSummary {
    var added: Int
    var duplicates: Int
    var failures: [ImportItemFailure]

    var noQRCount: Int { failures.lazy.filter { $0.kind == .noQR }.count }
    var itemFailCount: Int { failures.lazy.filter { $0.kind == .item }.count }
    var hasDetail: Bool { !failures.isEmpty }
    var isEmpty: Bool { added == 0 && duplicates == 0 && failures.isEmpty }

    var line: String {
        var parts = ["Added \(added)"]
        if duplicates > 0 { parts.append("\(duplicates) duplicate\(duplicates == 1 ? "" : "s") skipped") }
        if noQRCount > 0 { parts.append("\(noQRCount) image\(noQRCount == 1 ? "" : "s") had no QR") }
        if itemFailCount > 0 { parts.append("\(itemFailCount) not imported") }
        return parts.joined(separator: " · ")
    }
}

/// Central app state: vault lifecycle, account list, live codes. The vault is
/// created silently on first run and opened automatically from the Keychain; an
/// optional "Require Touch ID" setting gates that open with biometrics.
@MainActor
final class AppModel: ObservableObject {
    @Published var isLocked = true
    @Published var isOpening = true
    @Published var needsReset = false        // vault on disk but no way to decrypt it here
    @Published var needsPassphrase = false   // vault openable with its passphrase (e.g. CLI-created)
    @Published var vaultUnreachable = false  // external vault is configured but missing/unreadable
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
    @Published var importReport: ImportSummary?   // per-item result of the last bulk import
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
    private var lastVaultModified: Date?   // for detecting an external CLI rewrite

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
        // Pick up edits the tess CLI made to a shared external vault when the app
        // returns to the foreground.
        NotificationCenter.default.addObserver(
            forName: NSApplication.didBecomeActiveNotification, object: nil, queue: .main
        ) { [weak self] _ in Task { @MainActor in self?.reloadIfExternalChanged() } }
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
        guard isLocked, !needsReset, !needsPassphrase, !vaultUnreachable else { isOpening = false; return }

        // Pre-load routing (pure, unit-tested in VaultResolution): an external vault
        // that's missing/unresolvable never silently falls back to or creates the
        // container.
        switch VaultOpen.preLoad(hasBookmark: store.hasBookmark,
                                 bookmarkResolves: store.isExternal, fileExists: store.exists) {
        case .unreachable:
            vaultUnreachable = true; isOpening = false; return
        case .createContainer:
            do { try await createFreshVault(); errorMessage = nil; isLocked = false }
            catch { errorMessage = friendly(error) }
            lastVaultModified = store.modifiedAt
            isOpening = false; return
        case .loadExisting:
            break
        }

        // On an external shared vault an unreadable file means "relocate", never
        // "delete".
        let env: Envelope
        do { env = try store.load() }
        catch {
            if store.isExternal { vaultUnreachable = true; isOpening = false; return }
            errorMessage = friendly(error); isOpening = false; return
        }

        // No wrap this device can satisfy silently. Route via the pure decision:
        // a passphrase wrap (e.g. a tess CLI vault) is asked for; an external vault
        // with none is relocated (never deleted); a container one offers reset.
        if !SecureEnclaveWrap.hasWrap(env) && !AppKey.exists {
            switch VaultOpen.noSilentWrap(hasPassphraseWrap: hasPassphraseWrap(env), isExternal: store.isExternal) {
            case .passphrase: needsPassphrase = true
            case .unreachable: vaultUnreachable = true
            case .reset: needsReset = true
            }
            isOpening = false; return
        }

        do {
            try await openLoaded(env, reason: reason)
            errorMessage = nil
            isLocked = false
            lastVaultModified = store.modifiedAt
        } catch {
            // The Keychain app key didn't open this vault: it belongs to another
            // passphrase (a CLI vault). Ask for it instead of dead-ending.
            if case VaultError.wrongPassphrase = error, !SecureEnclaveWrap.hasWrap(env), hasPassphraseWrap(env) {
                needsPassphrase = true
            } else {
                errorMessage = friendly(error)
            }
        }
        isOpening = false
    }

    /// Point Tessera at an existing vault file (typically one created by the tess
    /// CLI), store a security-scoped bookmark, and open it through the normal
    /// unlock flow. Never copies the file — it opens in place so the CLI keeps
    /// working on the same path.
    func openExistingVault() {
        let panel = NSOpenPanel()
        panel.allowedContentTypes = [.json]
        panel.allowsOtherFileTypes = true
        panel.canChooseDirectories = false
        panel.message = "Choose a vault created by the tess CLI (usually vault.json)."
        let hint = FileManager.default.homeDirectoryForCurrentUser.appendingPathComponent(".local/share/tessera")
        if FileManager.default.fileExists(atPath: hint.path) { panel.directoryURL = hint }
        guard panel.runModal() == .OK, let url = panel.url else { return }
        guard store.setExternal(url) else {
            errorMessage = "Couldn't open that vault"; return
        }
        reopenFromStore()
    }

    /// Return to the built-in vault. Leaves any external file untouched.
    func useBuiltInVault() {
        store.clearExternal()
        reopenFromStore()
    }

    /// Clear in-memory state and re-run the open flow after the vault source
    /// changed (external ↔ built-in).
    private func reopenFromStore() {
        accounts = []; dek = nil; appKey = nil; envelope = nil
        isLocked = true; errorMessage = nil; status = nil
        vaultUnreachable = false; needsReset = false; needsPassphrase = false
        vaultExists = store.exists
        isOpening = true
        Task { await open(reason: "Unlock your Tessera vault") }
    }

    /// Reload accounts if an external vault (shared with the CLI) changed on disk
    /// while unlocked. The DEK is unchanged by CLI account edits, so decrypt with
    /// the held key; if that fails, keep the current list rather than dropping it.
    func reloadIfExternalChanged() {
        guard !isLocked, store.isExternal, let dek else { return }
        let modified = store.modifiedAt
        guard modified != lastVaultModified else { return }
        guard let env = try? store.load(), let accts = try? env.open(dek: dek) else { return }
        self.envelope = env
        self.accounts = accts
        self.lastVaultModified = modified
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
            self.lastVaultModified = store.modifiedAt
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

    /// Start over. For the built-in vault this deletes the Keychain key and the
    /// vault file. For an external vault shared with the CLI, it only detaches
    /// (drops the bookmark) — the app never deletes a file the CLI owns.
    func resetTessera() {
        if store.isExternal {
            store.clearExternal()
        } else {
            AppKey.delete()
            try? FileManager.default.removeItem(at: store.vaultURL)
        }
        envelope = nil; dek = nil; appKey = nil; accounts = []
        needsReset = false; needsPassphrase = false; vaultUnreachable = false; isLocked = true
        vaultExists = store.exists; errorMessage = nil; isOpening = true
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

    /// Import free text (pasted input, or newline-joined QR payloads). Classifies
    /// each line via `InputDetect`, imports every item that parses, and records a
    /// per-line failure for anything that doesn't. Never aborts on a bad line.
    @discardableResult
    func importReporting(_ text: String) -> Bool {
        let (accounts, errors) = InputDetect.parseText(text)
        let failures = errors.map {
            ImportItemFailure(source: "Line \($0.line)", display: $0.display, reason: $0.reason)
        }
        return finishImport(accounts: accounts, failures: failures)
    }

    /// Open a combined picker (images or app exports / link files) and import.
    @discardableResult
    func importPickedFiles() -> Bool {
        let types: [UTType] = [.png, .jpeg, .tiff, .gif, .bmp, .heic, .webP, .image,
                               .plainText, .text, .json]
        let urls = promptOpen(types: types,
                              message: "Choose images with 2FA QR codes, or an app export (Aegis, 2FAS, Raivo, Google Authenticator).",
                              multiple: true, allowsOther: true)
        guard !urls.isEmpty else { return false }
        return importDroppedFiles(urls)
    }

    /// Import one or many dropped or picked files. Each URL is handled on its own:
    /// an image is scanned for every QR it holds; anything else is read as bytes
    /// and parsed as an app export, falling back to `InputDetect.parseText`
    /// (otpauth lists, a migration URI in a file, bare setup keys). Accounts and
    /// per-item failures accumulate across all files and merge once; one bad file
    /// never aborts the batch.
    @discardableResult
    func importDroppedFiles(_ urls: [URL]) -> Bool {
        var accounts: [Account] = []
        var failures: [ImportItemFailure] = []
        for url in urls {
            let name = url.lastPathComponent
            let scoped = url.startAccessingSecurityScopedResource()
            defer { if scoped { url.stopAccessingSecurityScopedResource() } }

            if isImageURL(url) {
                guard let img = NSImage(contentsOf: url),
                      let cg = img.cgImage(forProposedRect: nil, context: nil, hints: nil) else {
                    failures.append(ImportItemFailure(source: name, display: "", reason: "Couldn't read image"))
                    continue
                }
                let payloads: [String]
                do { payloads = try QRCapture.detectAll(in: cg) }
                catch {
                    failures.append(ImportItemFailure(source: name, display: "", reason: reasonText(error)))
                    continue
                }
                if payloads.isEmpty {
                    failures.append(ImportItemFailure(source: name, display: "", reason: "No QR code found", kind: .noQR))
                    continue
                }
                for payload in payloads {
                    let (accs, errs) = InputDetect.parseText(payload)
                    accounts.append(contentsOf: accs)
                    failures.append(contentsOf: errs.map {
                        ImportItemFailure(source: name, display: $0.display, reason: $0.reason)
                    })
                }
            } else {
                guard let data = try? Data(contentsOf: url) else {
                    failures.append(ImportItemFailure(source: name, display: "", reason: "Couldn't read file"))
                    continue
                }
                // A recognized app export wins; its error (e.g. "export is
                // encrypted") beats a generic line-parse failure.
                do {
                    if let found = try Importers.parse(data) {
                        accounts.append(contentsOf: found.accounts)
                        continue
                    }
                } catch {
                    failures.append(ImportItemFailure(source: name, display: "", reason: reasonText(error)))
                    continue
                }
                guard let text = String(data: data, encoding: .utf8) else {
                    failures.append(ImportItemFailure(source: name, display: "", reason: "Couldn't read file"))
                    continue
                }
                let (accs, errs) = InputDetect.parseText(text)
                accounts.append(contentsOf: accs)
                failures.append(contentsOf: errs.map {
                    ImportItemFailure(source: "\(name) line \($0.line)", display: $0.display, reason: $0.reason)
                })
            }
        }
        return finishImport(accounts: accounts, failures: failures)
    }

    /// Whether a URL is an image (drives QR-scan vs read-as-text). Uses the
    /// system content type, with an extension fallback for no-metadata files.
    private func isImageURL(_ url: URL) -> Bool {
        if let type = try? url.resourceValues(forKeys: [.contentTypeKey]).contentType {
            return type.conforms(to: .image)
        }
        let ext = url.pathExtension.lowercased()
        return ["png", "jpg", "jpeg", "heic", "webp", "tiff", "tif", "gif", "bmp"].contains(ext)
    }

    /// Merge parsed accounts once, then publish the combined per-item report.
    /// Returns true if at least one account was added.
    @discardableResult
    private func finishImport(accounts: [Account], failures inputFailures: [ImportItemFailure]) -> Bool {
        errorMessage = nil
        let (added, duplicates, mergeFailures) = mergeReporting(accounts)
        if errorMessage != nil { return false }   // a hard persist failure, already surfaced
        let summary = ImportSummary(added: added, duplicates: duplicates,
                                    failures: inputFailures + mergeFailures)
        importReport = summary
        if added > 0 && summary.failures.isEmpty { status = summary.line }
        return added > 0
    }

    /// Merge that tolerates a bad account: duplicates are counted, per-account
    /// validation failures are collected (never a full secret), and the batch
    /// persists once. A persist failure surfaces via `errorMessage`.
    private func mergeReporting(_ parsed: [Account]) -> (added: Int, duplicates: Int, failures: [ImportItemFailure]) {
        var seen = Set(accounts.map(dedupeKey))
        var next = accounts
        var added = 0, duplicates = 0
        var failures: [ImportItemFailure] = []
        for a0 in parsed {
            let key = dedupeKey(a0)
            if seen.contains(key) { duplicates += 1; continue }
            let a = stamped(a0)
            do { try a.validate() }
            catch {
                let label = a0.issuer.isEmpty ? (a0.account.isEmpty ? "setup key" : a0.account) : a0.issuer
                failures.append(ImportItemFailure(source: label, display: "", reason: reasonText(error)))
                continue
            }
            seen.insert(key)
            next.append(a)
            added += 1
        }
        if added > 0 {
            do { try persist(next) }
            catch { errorMessage = friendly(error); return (0, 0, failures) }
        }
        return (added, duplicates, failures)
    }

    /// A user-facing reason from a thrown error, never exposing a secret.
    private func reasonText(_ error: Error) -> String {
        if let e = error as? Importers.ImporterError { return e.description }
        if let e = error as? AccountError { return e.description }
        return "\(error)"
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

    /// Running against a user-selected external vault (shared with the tess CLI).
    var isExternalVault: Bool { store.isExternal }

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
        self.lastVaultModified = store.modifiedAt   // our own write; don't reload it back
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
