import Foundation
import AppKit
import UniformTypeIdentifiers
import TesseraCore

/// One item that did not import, for the per-item result surface. `display` is a
/// redacted preview and never a full secret.
struct ImportItemFailure: Identifiable, Sendable {
    enum Kind: Sendable { case noQR, item }
    let id = UUID()
    var source: String    // "Line 3", a file name, or an issuer/account label
    var display: String    // redacted preview, may be empty
    var reason: String
    var kind: Kind = .item
}

/// Combined result of a bulk import across text, images, and files.
struct ImportSummary: Sendable {
    var added: Int
    var duplicates: Int
    var failures: [ImportItemFailure]

    var noQRCount: Int { failures.lazy.filter { $0.kind == .noQR }.count }
    var itemFailCount: Int { failures.lazy.filter { $0.kind == .item }.count }
    var hasDetail: Bool { !failures.isEmpty }
    var isEmpty: Bool { added == 0 && duplicates == 0 && failures.isEmpty }
    /// Something failed or nothing came in: the per-item report should open
    /// rather than a one-line notice.
    var needsReview: Bool { !failures.isEmpty || (added == 0 && duplicates == 0) }

    var line: String {
        var parts = ["Added \(added)"]
        if duplicates > 0 { parts.append("\(duplicates) duplicate\(duplicates == 1 ? "" : "s") skipped") }
        if noQRCount > 0 { parts.append("\(noQRCount) image\(noQRCount == 1 ? "" : "s") had no QR code") }
        if itemFailCount > 0 { parts.append("\(itemFailCount) not imported") }
        return parts.joined(separator: " · ")
    }
}

/// What a file import left for the screen that started it.
struct ImportResult: Equatable {
    var added = 0
    /// A Tessera encrypted backup among the files. It restores with its password
    /// instead of importing.
    var backup: URL?
    var failed = false
}

extension AppModel {
    static let importFileTypes: [UTType] = [.png, .jpeg, .tiff, .gif, .bmp, .heic, .webP, .image,
                                            .plainText, .text, .json]

    /// Import free text (pasted input, or newline-joined QR payloads). Classifies
    /// each line via `InputDetect`, imports every item that parses, and records a
    /// per-line failure for anything that doesn't. Never aborts on a bad line.
    @discardableResult
    func importReporting(_ text: String) -> Bool {
        let (accounts, errors) = InputDetect.parseText(text)
        let failures = errors.map {
            ImportItemFailure(source: "Line \($0.line)", display: $0.display, reason: $0.reason)
        }
        return finishImport(accounts: accounts, failures: failures) > 0
    }

    /// Ask for images or export files to import. Empty when the user cancels.
    func chooseImportFiles() -> [URL] {
        promptOpen(types: Self.importFileTypes,
                   message: "Choose images with 2FA QR codes, an app export (Aegis, 2FAS, Raivo, Google Authenticator) or a Tessera backup.",
                   multiple: true, allowsOther: true)
    }

    /// Import dropped or picked files. Reading and QR scanning run off the main
    /// thread with `importProgress` updated after each file; accounts merge once
    /// at the end, and one bad file never aborts the batch. The first Tessera
    /// backup comes back in the result for a restore; any further ones are
    /// reported as not imported.
    func importFiles(_ urls: [URL]) async -> ImportResult {
        guard !urls.isEmpty, importProgress == nil else { return ImportResult() }
        errorMessage = nil
        let total = urls.count
        importProgress = ImportProgress(done: 0, total: total)
        defer { importProgress = nil }

        let scan = await Task.detached(priority: .userInitiated) { [self] in
            ImportScanner.scan(urls) { done in
                Task { @MainActor in
                    guard let p = self.importProgress, p.total == total, done > p.done else { return }
                    self.importProgress = ImportProgress(done: done, total: total)
                }
            }
        }.value

        var failures = scan.failures
        failures.append(contentsOf: scan.backups.dropFirst().map {
            ImportItemFailure(source: $0.lastPathComponent, display: "",
                              reason: "Tessera backup. Restore one backup at a time from Settings > Backup.")
        })
        let backup = scan.backups.first
        if backup != nil && scan.accounts.isEmpty && failures.isEmpty {
            importReport = nil
            return ImportResult(backup: backup)
        }
        let added = finishImport(accounts: scan.accounts, failures: failures)
        return ImportResult(added: added, backup: backup, failed: errorMessage != nil)
    }

    /// Merge parsed accounts once, then publish the combined per-item report.
    /// Returns the number of accounts added.
    @discardableResult
    private func finishImport(accounts: [Account], failures inputFailures: [ImportItemFailure]) -> Int {
        errorMessage = nil
        let (added, duplicates, mergeFailures) = mergeReporting(accounts)
        if errorMessage != nil { return 0 }   // a hard persist failure, already surfaced
        let summary = ImportSummary(added: added, duplicates: duplicates,
                                    failures: inputFailures + mergeFailures)
        importReport = summary
        if added > 0 && summary.failures.isEmpty { notice = .info(summary.line) }
        return added
    }

    /// Merge that tolerates a bad account: duplicates are counted, per-account
    /// validation failures are collected (never a full secret), and the batch
    /// persists once. A persist failure surfaces via `errorMessage`.
    private func mergeReporting(_ parsed: [Account]) -> (added: Int, duplicates: Int, failures: [ImportItemFailure]) {
        var seen = Set(accounts.map(dedupeKey))
        var candidates: [Account] = []
        var duplicates = 0
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
            candidates.append(a)
        }
        if !candidates.isEmpty {
            do { try stage(VaultChange.appendingNew(candidates)) }
            catch { errorMessage = UserMessage.text(for: error, context: .add); return (0, 0, failures) }
        }
        return (candidates.count, duplicates, failures)
    }

    /// A user-facing reason from a thrown error, never exposing a secret.
    private func reasonText(_ error: Error) -> String {
        UserMessage.text(for: error, context: .add)
    }
}
