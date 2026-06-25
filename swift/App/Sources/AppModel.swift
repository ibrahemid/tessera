import Foundation
import AppKit
import SwiftUI
import TesseraCore
import TesseraArgon2

/// Central app state: vault lifecycle, account list, live codes, and the unlock
/// flow (passphrase + optional Touch ID).
@MainActor
final class AppModel: ObservableObject {
    @Published var isLocked = true
    @Published var accounts: [Account] = []
    @Published var now = Date()
    @Published var errorMessage: String?
    @Published var vaultExists: Bool

    private let store = VaultStore()
    private let argon2 = Argon2Reference()
    private var envelope: Envelope?
    private var passphrase: String?
    private var timer: Timer?

    init() {
        vaultExists = store.exists
        startTicking()
    }

    /// In-memory model for screenshots/previews; never touches disk.
    enum DemoState { case populated, empty, locked, fresh }
    init(demo: DemoState) {
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

    static var sampleAccounts: [Account] {
        func s(_ str: String) -> Data { Data(str.utf8) }
        return [
            Account(id: "1", type: .totp, issuer: "GitHub", account: "ibrahem", secret: s("12345678901234567890"), algorithm: "SHA1", digits: 6, period: 30, pinned: true),
            Account(id: "2", type: .totp, issuer: "Cloudflare", account: "ibra@ibrahemid.com", secret: s("abcdefghij1234567890"), algorithm: "SHA1", digits: 6, period: 30, pinned: true),
            Account(id: "3", type: .totp, issuer: "Google", account: "support@ibrahemid.com", secret: s("zyxwvutsrqponmlkjihg"), algorithm: "SHA1", digits: 6, period: 30),
            Account(id: "4", type: .steam, issuer: "Steam", account: "ibrahem", secret: s("steamsecret12345"), algorithm: "SHA1", digits: 5, period: 30),
            Account(id: "5", type: .totp, issuer: "AWS", account: "root", secret: s("awssecretkey00000000"), algorithm: "SHA256", digits: 6, period: 30),
            Account(id: "6", type: .hotp, issuer: "Bank", account: "•••• 4291", secret: s("banksecret0000000000"), algorithm: "SHA1", digits: 6, counter: 12),
        ]
    }

    var hasTouchIDWrap: Bool {
        guard let env = envelope else { return false }
        return SecureEnclaveWrap.hasWrap(env)
    }

    var touchIDAvailableForUnlock: Bool {
        store.exists && SecureEnclaveWrap.isAvailable && (peekEnvelopeHasSEWrap())
    }

    private func peekEnvelopeHasSEWrap() -> Bool {
        if let env = envelope { return SecureEnclaveWrap.hasWrap(env) }
        if let env = try? store.load() { return SecureEnclaveWrap.hasWrap(env) }
        return false
    }

    // MARK: Lifecycle

    func createVault(passphrase: String) {
        run {
            let env = try seal(accounts: [], passphrase: passphrase, existing: nil)
            try store.save(env)
            self.envelope = env
            self.passphrase = passphrase
            self.accounts = []
            self.isLocked = false
            self.vaultExists = true
        }
    }

    func unlock(passphrase: String) {
        run {
            let env = try store.load()
            let accts = try env.open(passphrase: passphrase, argon2: argon2)
            self.envelope = env
            self.passphrase = passphrase
            self.accounts = accts
            self.isLocked = false
        }
    }

    func unlockWithTouchID() {
        run {
            let env = try store.load()
            let dek = try SecureEnclaveWrap.open(env, reason: "Unlock your Tessera vault")
            self.accounts = try env.open(dek: dek)
            self.envelope = env
            // passphrase stays nil; mutations require passphrase, so prompt then.
            self.isLocked = false
        }
    }

    func lock() {
        accounts = []
        passphrase = nil
        envelope = nil
        isLocked = true
    }

    // MARK: Account operations

    func addAccount(_ account: Account) { mutate { $0.append(stamped(account)) } }

    func importMigration(_ uri: String) {
        run {
            let imported = try Migration.parse(uri).map { stamped($0) }
            try requireMutable()
            try persist(accounts + imported)
        }
    }

    func importOTPAuth(_ uri: String) {
        run {
            let a = stamped(try OTPAuth.parse(uri))
            try requireMutable()
            try persist(accounts + [a])
        }
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

    func advanceHOTP(_ account: Account) {
        guard account.type == .hotp else { return }
        mutate { accts in
            if let i = accts.firstIndex(where: { $0.id == account.id }) {
                accts[i].counter += 1
                accts[i].updatedAt = Int64(Date().timeIntervalSince1970)
            }
        }
        // Copy the freshly advanced code.
        if let updated = accounts.first(where: { $0.id == account.id }),
           let code = try? OTP.code(for: updated, at: now) {
            NSPasteboard.general.clearContents()
            NSPasteboard.general.setString(code, forType: .string)
        }
    }

    var vaultPathDisplay: String {
        store.vaultURL.path.replacingOccurrences(of: NSHomeDirectory(), with: "~")
    }

    func code(for account: Account) -> String {
        (try? OTP.code(for: account, at: now)) ?? "------"
    }

    func remaining(for account: Account) -> Int { OTP.remainingSeconds(time: now, period: account.period) }

    // MARK: Touch ID enrollment

    func enableTouchID() {
        run {
            guard var env = envelope, let pass = passphrase else { throw VaultError.wrongPassphrase }
            let dek = try unwrapDEK(env: env, passphrase: pass)
            try SecureEnclaveWrap.enable(on: &env, dek: dek)
            try store.save(env)
            self.envelope = env
        }
    }

    func disableTouchID() {
        run {
            guard var env = envelope else { return }
            SecureEnclaveWrap.disable(on: &env)
            try store.save(env)
            self.envelope = env
        }
    }

    // MARK: Helpers

    private func mutate(_ change: (inout [Account]) -> Void) {
        run {
            try requireMutable()
            var next = accounts
            change(&next)
            try persist(next)
        }
    }

    private func requireMutable() throws {
        if passphrase == nil { throw VaultError.noPassphraseWrap } // unlocked via Touch ID only
    }

    private func persist(_ next: [Account]) throws {
        guard let env = envelope, let pass = passphrase else { throw VaultError.wrongPassphrase }
        let updated = try env.reseal(accounts: next, passphrase: pass, argon2: argon2)
        try store.save(updated)
        self.envelope = updated
        self.accounts = next
    }

    private func unwrapDEK(env: Envelope, passphrase: String) throws -> Data {
        try env.recoverDEK(passphrase: passphrase, argon2: argon2)
    }

    private func seal(accounts: [Account], passphrase: String, existing: Envelope?) throws -> Envelope {
        try Envelope.create(accounts: accounts, passphrase: passphrase, argon2: argon2)
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
        catch { errorMessage = "\(error)" }
    }

    private func startTicking() {
        timer = Timer.scheduledTimer(withTimeInterval: 1, repeats: true) { [weak self] _ in
            Task { @MainActor in self?.now = Date() }
        }
    }
}

