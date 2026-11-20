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

    func addAccount(_ account: Account) { mutate { $0.append(stamped(account)) } }

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
            let key = dedupeKey(a)
            if accounts.contains(where: { dedupeKey($0) == key }) { throw AccountEntryError.duplicate }
            try persist(accounts + [a])
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
        var seen = Set<String>(), next: [Account] = []
        var removed = 0
        for a in accounts {
            let k = dedupeKey(a)
            if seen.contains(k) { removed += 1; continue }
            seen.insert(k); next.append(a)
        }
        if removed > 0 { try persist(next) }
        return removed
    }

    /// Persisted manual reordering of the account list (the payload array order).
    func move(inSection ids: [String], fromOffsets: IndexSet, toOffset: Int) {
        let next = VaultList.reorder(accounts, section: ids, fromOffsets: fromOffsets, toOffset: toOffset)
        guard next.map(\.id) != accounts.map(\.id) else { return }
        mutate { $0 = next }
    }

    /// The cleartext otpauth:// URI for an account (for QR export / move to phone).
    func otpauthURI(for account: Account) -> String { OTPAuth.format(account) }

    func dedupeKey(_ a: Account) -> String {
        [String(describing: a.type), a.issuer.lowercased(), a.account.lowercased(),
         Base32.encodeNoPad(a.secret)].joined(separator: "|")
    }

    func remove(_ account: Account) { mutate { $0.removeAll { $0.id == account.id } } }

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

    private func bulkEdit(_ change: (inout [Account], Int64) -> Int, notice text: (Int) -> String) {
        var next = accounts
        let changed = change(&next, Int64(Date().timeIntervalSince1970))
        guard changed > 0, mutate({ $0 = next }) else { return }
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
        var next = accounts
        guard let i = next.firstIndex(where: { $0.id == account.id }) else { return nil }
        next[i].handle = h
        next[i].updatedAt = Int64(Date().timeIntervalSince1970)
        do { try persist(next) } catch { return UserMessage.textUnlessCancelled(for: error, context: .save) }
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
        var next = accounts
        guard let i = next.firstIndex(where: { $0.id == account.id }) else { return nil }
        next[i].issuer = iss
        next[i].account = acct
        next[i].handle = h
        next[i].folder = fold
        next[i].updatedAt = Int64(Date().timeIntervalSince1970)
        do { try persist(next) } catch { return UserMessage.textUnlessCancelled(for: error, context: .save) }
        return nil
    }

    func advanceHOTP(_ account: Account) {
        guard account.type == .hotp else { return }
        let saved = mutate { accts in
            if let i = accts.firstIndex(where: { $0.id == account.id }) {
                accts[i].counter = try Account.nextCounter(after: accts[i].counter)
                accts[i].updatedAt = Int64(Date().timeIntervalSince1970)
            }
        }
        guard saved else { return }
        // Copy the freshly advanced code, with the same feedback as a TOTP copy.
        if let updated = accounts.first(where: { $0.id == account.id }),
           let code = try? OTP.code(for: updated, at: Date()) {
            copyToPasteboard(code, kind: .code)
            announce("Copied \(account.displayName)")
        }
    }

    @discardableResult
    func copyCode(_ account: Account, at time: Date, to pasteboard: NSPasteboard = .general) -> String? {
        let next = CodeTiming.shouldCopyNext(account, at: time)
        let value: String
        do {
            value = try OTP.code(for: account, at: next ? CodeTiming.windowEnd(account, at: time) : time)
        } catch {
            notice = .failure(UserMessage.text(for: error, context: .save))
            return nil
        }
        copyToPasteboard(value, kind: .code, to: pasteboard)
        announce(next ? "Copied the next code for \(account.displayName)" : "Copied \(account.displayName)")
        return value
    }

    private func announce(_ text: String) {
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
