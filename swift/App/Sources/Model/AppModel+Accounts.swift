import Foundation
import AppKit
import SwiftUI
import TesseraCore

extension AppModel {
    static let hideCodesPrefKey = "tessera.hideCodes"

    /// The account's raw secret as a base32 setup key (what you type into another
    /// app's manual-entry screen).
    func secretBase32(for account: Account) -> String { Base32.encodeNoPad(account.secret) }

    // MARK: Account operations

    func addAccount(_ account: Account) {
        let a = stamped(account)
        mutate { $0.append(a) }
    }

    /// Add one account from manually typed fields (the "setup key" path). The
    /// secret is base32 as shown by the service. Validates and rejects duplicates.
    @discardableResult
    func addManual(type: OTPType, issuer: String, account: String, secretBase32: String,
                   algorithm: String, digits: Int, period: Int, counter: Int64) -> Bool {
        do {
            // Strict decode (spec base32 rules); accepts case, spaces, dashes.
            let secret: Data
            do { secret = try Base32.decode(secretBase32) }
            catch { throw AccountEntryError.invalidSetupKey }
            var a = Account(id: "", type: type,
                            issuer: issuer.trimmingCharacters(in: .whitespaces),
                            account: account.trimmingCharacters(in: .whitespaces),
                            secret: secret, algorithm: algorithm, digits: digits, period: period)
            if type == .hotp { a.counter = counter }
            a = stamped(a)
            try a.validate()
            let key = VaultChange.dedupeKey(a), added = a
            try stage { accounts in
                if accounts.contains(where: { VaultChange.dedupeKey($0) == key }) { throw AccountEntryError.duplicate }
                accounts.append(added)
            }
            errorMessage = nil
            notice = .info("Added \(a.displayName)")   // same feedback as link / scan import
            return true
        } catch {
            errorMessage = UserMessage.textUnlessCancelled(for: error, context: .add)
            return false
        }
    }

    /// Remove accounts that duplicate another (same type, issuer, account, secret),
    /// keeping the first. Returns the number removed.
    func removeDuplicates() throws -> Int {
        let removed = accounts.count - VaultChange.droppingDuplicates(accounts).count
        guard removed > 0 else { return 0 }
        try stage { $0 = VaultChange.droppingDuplicates($0) }
        return removed
    }

    /// Persisted manual reordering of the account list (the payload array order).
    func move(inSection ids: [String], fromOffsets: IndexSet, toOffset: Int) {
        let next = VaultList.reorder(accounts, section: ids, fromOffsets: fromOffsets, toOffset: toOffset)
        guard next.map(\.id) != accounts.map(\.id) else { return }
        mutate { $0 = VaultList.reorder($0, section: ids, fromOffsets: fromOffsets, toOffset: toOffset) }
    }

    /// The cleartext otpauth:// URI for an account (for QR export / move to phone).
    func otpauthURI(for account: Account) -> String { OTPAuth.format(account) }

    func dedupeKey(_ a: Account) -> Merge.ContentKey { VaultChange.dedupeKey(a) }

    func remove(_ account: Account) {
        let id = account.id
        mutate { $0.removeAll { $0.id == id } }
    }

    func togglePin(_ account: Account) {
        setPinned(!account.pinned, ids: [account.id])
    }

    /// Assign an account to a folder. Passing "" removes it from its folder.
    /// Folders exist exactly as long as an account references them.
    func setFolder(_ account: Account, to folder: String) {
        setFolder(ids: [account.id], to: folder)
    }

    func setFolder(ids: Set<String>, to folder: String) {
        let name = folder.trimmingCharacters(in: .whitespaces)
        let from = ids.count == 1 ? accounts.first { ids.contains($0.id) }?.folder ?? "" : ""
        bulkEdit { VaultEdit.setFolder(name, ids: ids, in: &$0, at: $1) } notice: { n in
            let subject = self.subject(count: n, ids: ids)
            if !name.isEmpty { return "Moved \(subject) to \(name)" }
            return from.isEmpty ? "Removed \(subject) from their folders" : "Removed \(subject) from \(from)"
        }
    }

    func setPinned(_ pinned: Bool, ids: Set<String>) {
        bulkEdit { VaultEdit.setPinned(pinned, ids: ids, in: &$0, at: $1) } notice: { n in
            pinned ? "Pinned \(self.subject(count: n, ids: ids))" : "Unpinned \(self.subject(count: n, ids: ids))"
        }
    }

    func addTag(_ raw: String, ids: Set<String>) {
        guard let tag = VaultEdit.normalizedTag(raw) else { return }
        bulkEdit { VaultEdit.addTag(tag, ids: ids, in: &$0, at: $1) } notice: { n in
            "Added #\(tag) to \(self.subject(count: n, ids: ids))"
        }
    }

    func removeTag(_ tag: String, ids: Set<String>) {
        bulkEdit { VaultEdit.removeTag(tag, ids: ids, in: &$0, at: $1) } notice: { n in
            "Removed #\(tag) from \(self.subject(count: n, ids: ids))"
        }
    }

    func renameFolder(_ old: String, to new: String) {
        let name = new.trimmingCharacters(in: .whitespaces)
        bulkEdit { VaultEdit.renameFolder(old, to: name, in: &$0, at: $1) } notice: { _ in
            "Renamed \(old) to \(name)"
        }
    }

    func removeFolder(_ name: String) {
        bulkEdit { VaultEdit.removeFolder(name, in: &$0, at: $1) } notice: { _ in
            "Removed the \(name) folder"
        }
    }

    var allTags: [String] { VaultList.tags(in: accounts) }

    private func bulkEdit(_ change: @escaping @Sendable (inout [Account], Int64) -> Int,
                          notice text: (Int) -> String) {
        let stamp = Int64(Date().timeIntervalSince1970)
        var probe = accounts
        let changed = change(&probe, stamp)
        guard changed > 0, mutate({ _ = change(&$0, stamp) }) else { return }
        notice = .info(text(changed))
    }

    private func subject(count: Int, ids: Set<String>) -> String {
        if ids.count == 1, let a = accounts.first(where: { ids.contains($0.id) }) { return a.displayName }
        return count == 1 ? "1 account" : "\(count) accounts"
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
        guard accounts.contains(where: { $0.id == account.id }) else { return nil }
        let id = account.id, stamp = Int64(Date().timeIntervalSince1970)
        do {
            try stage { accounts in
                guard let i = accounts.firstIndex(where: { $0.id == id }) else { return }
                accounts[i].handle = h
                accounts[i].updatedAt = stamp
            }
        } catch { return UserMessage.textUnlessCancelled(for: error, context: .save) }
        return nil
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
        guard accounts.contains(where: { $0.id == account.id }) else { return nil }
        let id = account.id, stamp = Int64(Date().timeIntervalSince1970)
        do {
            try stage { accounts in
                guard let i = accounts.firstIndex(where: { $0.id == id }) else { return }
                accounts[i].issuer = iss
                accounts[i].account = acct
                accounts[i].handle = h
                accounts[i].folder = fold
                accounts[i].updatedAt = stamp
            }
        } catch { return UserMessage.textUnlessCancelled(for: error, context: .save) }
        return nil
    }

    func announce(_ text: String) {
        notice = .info(text)
        AccessibilityNotification.Announcement(text).post()
    }

    /// The account's current code. Memoized per period slot: a 1 Hz tick
    /// re-renders the row but recomputes the HMAC only when the slot (or the
    /// account) actually changed.
    func code(for account: Account, at time: Date) -> String {
        memoizedCode(account, key: account.id, at: time)
    }

    func nextCode(for account: Account, at time: Date) -> String {
        guard CodeTiming.isTimeBased(account) else { return "" }
        return memoizedCode(account, key: account.id + "\u{0}next", at: CodeTiming.windowEnd(account, at: time))
    }

    func remaining(for account: Account, at time: Date) -> Int {
        CodeTiming.remaining(account, at: time)
    }

    private func memoizedCode(_ account: Account, key: String, at time: Date) -> String {
        let slot = periodSlot(account, at: time)
        if let hit = codeCache[key], hit.slot == slot, hit.updatedAt == account.updatedAt {
            return hit.code
        }
        let code = (try? OTP.code(for: account, at: time)) ?? "------"
        codeCache[key] = (slot, account.updatedAt, code)
        return code
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

extension VaultPersisting {
    @discardableResult
    func advanceHOTP(_ account: Account,
                     to pasteboard: any SecretPasteboard = NSPasteboard.general) -> Task<Bool, Never>? {
        guard account.type == .hotp else { return nil }
        let id = account.id, name = account.displayName, stamp = Int64(Date().timeIntervalSince1970)
        let commit: Task<CommitOutcome, Never>
        do {
            commit = try stage({ accounts in
                guard let i = accounts.firstIndex(where: { $0.id == id }) else { return }
                accounts[i].counter = try Account.nextCounter(after: accounts[i].counter)
                accounts[i].updatedAt = stamp
            }, showsBeforeCommit: false)
        } catch {
            reportFailure(error)
            return nil
        }
        let generation = lockGeneration
        return Task { @MainActor in
            guard case .committed(let committed) = await commit.value, generation == self.lockGeneration,
                  let fresh = committed.accounts.first(where: { $0.id == id }) else { return false }
            if let i = self.accounts.firstIndex(where: { $0.id == id }), self.accounts[i].counter < fresh.counter {
                self.accounts[i].counter = fresh.counter
                self.accounts[i].updatedAt = fresh.updatedAt
            }
            guard let code = try? OTP.code(for: fresh, at: Date()) else { return false }
            guard self.copyToPasteboard(code, kind: .code, to: pasteboard) else {
                self.notice = .failure(UserMessage.copyFailed)
                return false
            }
            self.announce("Copied \(name)")
            return true
        }
    }

    @discardableResult
    func copyCode(_ account: Account, at time: Date,
                  to pasteboard: any SecretPasteboard = NSPasteboard.general) -> String? {
        let next = CodeTiming.shouldCopyNext(account, at: time)
        let value: String
        do {
            value = try OTP.code(for: account, at: next ? CodeTiming.windowEnd(account, at: time) : time)
        } catch {
            notice = .failure(UserMessage.text(for: error, context: .save))
            return nil
        }
        guard copyToPasteboard(value, kind: .code, to: pasteboard) else {
            notice = .failure(UserMessage.copyFailed)
            return nil
        }
        announce(next ? "Copied the next code for \(account.displayName)" : "Copied \(account.displayName)")
        return value
    }
}
