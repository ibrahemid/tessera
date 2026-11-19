import Foundation
import AppKit
import LocalAuthentication
import SwiftUI
import TesseraCore
import TesseraArgon2

/// Central app state: vault lifecycle, account list, live codes. The vault is
/// created silently on first run and opened automatically from the Keychain; an
/// optional "Require Touch ID" setting gates that open with biometrics.
@MainActor
final class AppModel: ObservableObject {
    @Published var isLocked = true { didSet { ticker.setEnabled(!isLocked) } }
    @Published var isOpening = true
    @Published var needsReset = false        // vault on disk but no way to decrypt it here
    @Published var needsPassphrase = false   // vault openable with its passphrase (e.g. CLI-created)
    @Published var needsRecovery = false     // Secure Enclave key is gone and there is no passphrase wrap
    /// The passphrase prompt was reached because Touch ID stopped working, not
    /// because the vault came from the CLI. Only the wording differs.
    @Published var passphraseIsRecovery = false
    @Published var vaultUnreachable = false  // external vault is configured but missing/unreadable
    @Published var accounts: [Account] = [] {
        didSet {
            let f = Array(Set(accounts.map(\.folder).filter { !$0.isEmpty })).sorted()
            if f != folders { folders = f }
            codeCache.removeAll(keepingCapacity: true)
        }
    }
    /// Distinct non-empty folder names; derived on account changes, not per render tick.
    @Published private(set) var folders: [String] = []
    @Published var errorMessage: String?
    @Published var status: String? { didSet { statusToken = UUID() } }   // transient success/info feedback
    /// Bumped on every assignment to `status` so a repeated identical message
    /// restarts its auto-dismiss timer instead of being coalesced away.
    @Published private(set) var statusToken = UUID()
    @Published var vaultExists: Bool
    @Published var importReport: ImportSummary?   // per-item result of the last bulk import
    @Published var requireBiometrics: Bool
    /// Whether the vault carries an argon2id passphrase wrap: the recovery path
    /// when Touch ID can't open it, and what lets the tess CLI open it too.
    @Published private(set) var hasRecoveryPassphrase = false

    static let biometricsPrefKey = "tessera.requireTouchID"
    static let compactPrefKey = "tessera.compact"
    /// How long a copied code or secret stays on the pasteboard.
    static let pasteboardExpiry: TimeInterval = 30

    /// The 1 Hz clock. Views that render countdowns observe this, not the model.
    let ticker = Ticker()

    let store = VaultStore()
    let argon2 = Argon2Reference()
    let isDemo: Bool
    var envelope: Envelope? {
        didSet { hasRecoveryPassphrase = envelope?.hasPassphraseWrap ?? false }
    }
    var dek: Data?            // held after unlock; enables edits with no prompt
    var appKey: Data?         // held after unlock; needed to retoggle Touch ID
    var lastVaultModified: Date?   // for detecting an external CLI rewrite
    var pendingSwitchMessage: String?   // "Now using X", shown once the switched vault unlocks
    private let observers = NotificationObservers()
    var pasteboardClear: Task<Void, Never>?
    /// Codes memoized per account until its period slot (or the account itself)
    /// changes, so a tick re-renders rows without recomputing every HMAC.
    var codeCache: [String: (slot: Int64, updatedAt: Int64, code: String)] = [:]

    init() {
        isDemo = false
        vaultExists = store.exists
        let pref = UserDefaults.standard.bool(forKey: AppModel.biometricsPrefKey)
        #if DEBUG
        requireBiometrics = pref && !CommandLine.arguments.contains("-uitest")
        #else
        requireBiometrics = pref
        #endif
        // Lock on sleep only when Touch ID is required; otherwise the macOS login
        // session is the gate and the vault reopens automatically.
        let workspace = NSWorkspace.shared.notificationCenter
        observers.add(workspace, workspace.addObserver(
            forName: NSWorkspace.willSleepNotification, object: nil, queue: .main
        ) { [weak self] _ in Task { @MainActor in if self?.requireBiometrics == true { self?.lock() } } })
        // Pick up edits the tess CLI made to a shared external vault when the app
        // returns to the foreground.
        let center = NotificationCenter.default
        observers.add(center, center.addObserver(
            forName: NSApplication.didBecomeActiveNotification, object: nil, queue: .main
        ) { [weak self] _ in Task { @MainActor in self?.reloadIfExternalChanged() } })
    }

    #if DEBUG
    /// In-memory model for screenshots/previews; never touches disk or Keychain.
    /// Debug-only: the shipping binary carries no sample data and no way to
    /// reach a fake vault.
    enum DemoState { case populated, empty, locked, fresh }
    init(demo: DemoState) {
        isDemo = true
        isOpening = false
        requireBiometrics = true
        switch demo {
        case .populated:
            vaultExists = true; isLocked = false
            var demo = AppModel.sampleAccounts
            Handles.assign(&demo)
            accounts = demo
        case .empty:
            vaultExists = true; isLocked = false; accounts = []
        case .locked:
            vaultExists = true; isLocked = true
        case .fresh:
            vaultExists = false; isLocked = true
        }
        // Property observers don't fire during init, so start the clock by hand.
        ticker.setEnabled(!isLocked)
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
    #endif

    // MARK: Helpers

    func mutate(_ change: (inout [Account]) -> Void) {
        run {
            var next = accounts
            change(&next)
            try persist(next)
        }
    }

    func persist(_ next: [Account]) throws {
        guard let env = envelope, let dek = dek else { throw VaultError.wrongPassphrase }
        // Any account added this write (import, manual entry) gets a handle here,
        // matching the CLI's save path so both cores stay in step.
        var next = next
        Handles.assign(&next)
        try Handles.checkUniqueness(next)
        let updated = try env.reseal(accounts: next, dek: dek)
        try store.save(updated)
        self.envelope = updated
        self.accounts = next
        self.lastVaultModified = store.modifiedAt   // our own write; don't reload it back
    }

    func stamped(_ a: Account) -> Account {
        var x = a
        if x.id.isEmpty { x.id = UUID().uuidString }
        let ts = Int64(Date().timeIntervalSince1970)
        if x.createdAt == 0 { x.createdAt = ts }
        x.updatedAt = ts
        return x
    }

    func run(_ body: () throws -> Void) {
        do { errorMessage = nil; try body() }
        catch { errorMessage = friendly(error) }
    }

    /// User-cancelled biometrics is not an error worth surfacing.
    func friendly(_ error: Error) -> String? {
        if case AppKey.AppKeyError.keychain(let s) = error, s == errSecUserCanceled { return nil }
        if let la = error as? LAError, [.userCancel, .systemCancel, .appCancel].contains(la.code) { return nil }
        let ns = error as NSError
        if ns.domain == LAErrorDomain, [-2, -4, -9].contains(ns.code) { return nil }
        if ns.code == Int(errSecUserCanceled) { return nil }
        return "\(error)"
    }
}
