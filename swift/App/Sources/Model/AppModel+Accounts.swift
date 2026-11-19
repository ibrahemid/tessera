import Foundation
import SwiftUI
import TesseraCore

extension AppModel {
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
            // Strict decode (spec base32 rules); accepts case, spaces, dashes.
            let secret: Data
            do { secret = try Base32.decode(secretBase32) }
            catch { throw AccountError.invalid("not a valid setup key (base32)") }
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

    /// Remove accounts that duplicate another (same type, issuer, account, secret),
    /// keeping the first. Returns the number removed, or nil when the write
    /// failed — the error is on `errorMessage` and there is nothing to report.
    @discardableResult
    func removeDuplicates() -> Int? {
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
        return errorMessage == nil ? removed : nil
    }

    /// Persisted manual reordering of the account list (the payload array order).
    func move(fromOffsets: IndexSet, toOffset: Int) {
        mutate { $0.move(fromOffsets: fromOffsets, toOffset: toOffset) }
    }

    /// The cleartext otpauth:// URI for an account (for QR export / move to phone).
    func otpauthURI(for account: Account) -> String { OTPAuth.format(account) }

    func dedupeKey(_ a: Account) -> String {
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
        guard errorMessage == nil else { return }   // the change didn't persist; no success feedback
        status = name.isEmpty ? "Removed from list" : "Added to \(name)"
    }

    /// Set an account's handle (alias) from the edit view. Applies the same
    /// charset and vault-uniqueness rules as auto-assignment; returns nil on
    /// success or a factual reason the edit was rejected. The freed old value
    /// becomes available to future auto-assignment.
    func setHandle(_ account: Account, to raw: String) -> String? {
        let h = raw.trimmingCharacters(in: .whitespaces).lowercased()
        if h == account.handle { return nil }
        guard Handles.isValid(h) else {
            return "Aliases are 1-12 characters: a lowercase letter first, then letters or digits."
        }
        if let other = accounts.first(where: { $0.id != account.id && $0.handle == h }) {
            return "\(h) is already used by \(other.displayName)."
        }
        var failure: String?
        run {
            var next = accounts
            guard let i = next.firstIndex(where: { $0.id == account.id }) else { return }
            next[i].handle = h
            next[i].updatedAt = Int64(Date().timeIntervalSince1970)
            try persist(next)
        }
        if let msg = errorMessage { failure = msg; errorMessage = nil }
        return failure
    }

    /// Apply the edit sheet's fields (issuer, account label, handle, folder) in a
    /// single persist. Returns nil on success or a factual reason to show inline.
    /// The handle keeps the same charset + vault-uniqueness rules as auto-assign;
    /// crypto parameters (type, algorithm, digits, period) are never editable —
    /// changing them would silently break the codes.
    func applyEdit(_ account: Account, issuer: String, accountLabel: String,
                   handle rawHandle: String, folder rawFolder: String) -> String? {
        let iss = issuer.trimmingCharacters(in: .whitespaces)
        let acct = accountLabel.trimmingCharacters(in: .whitespaces)
        if iss.isEmpty && acct.isEmpty {
            return "Give the account an issuer or a name."
        }
        let h = rawHandle.trimmingCharacters(in: .whitespaces).lowercased()
        guard Handles.isValid(h) else {
            return "Aliases are 1-12 characters: a lowercase letter first, then letters or digits."
        }
        if let other = accounts.first(where: { $0.id != account.id && $0.handle == h }) {
            return "\(h) is already used by \(other.displayName)."
        }
        let fold = rawFolder.trimmingCharacters(in: .whitespaces)
        var failure: String?
        run {
            var next = accounts
            guard let i = next.firstIndex(where: { $0.id == account.id }) else { return }
            next[i].issuer = iss
            next[i].account = acct
            next[i].handle = h
            next[i].folder = fold
            next[i].updatedAt = Int64(Date().timeIntervalSince1970)
            try persist(next)
        }
        if let msg = errorMessage { failure = msg; errorMessage = nil }
        return failure
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
           let code = try? OTP.code(for: updated, at: Date()) {
            copyToPasteboard(code, transient: true)
            status = "Copied \(account.displayName)"
        }
    }

    /// The account's current code. Memoized per period slot: a 1 Hz tick
    /// re-renders the row but recomputes the HMAC only when the slot (or the
    /// account) actually changed.
    func code(for account: Account, at time: Date) -> String {
        let slot = periodSlot(account, at: time)
        if let hit = codeCache[account.id], hit.slot == slot, hit.updatedAt == account.updatedAt {
            return hit.code
        }
        let code = (try? OTP.code(for: account, at: time)) ?? "------"
        codeCache[account.id] = (slot, account.updatedAt, code)
        return code
    }

    func remaining(for account: Account, at time: Date) -> Int {
        OTP.remainingSeconds(time: time, period: account.period)
    }

    /// The value a code is a pure function of: the counter for HOTP, the time
    /// window otherwise (Steam is always a 30s window).
    private func periodSlot(_ a: Account, at time: Date) -> Int64 {
        switch a.type {
        case .hotp: return a.counter
        case .steam: return Int64(time.timeIntervalSince1970) / 30
        case .totp: return Int64(time.timeIntervalSince1970) / Int64(max(a.period, 1))
        }
    }
}
