import Foundation
import AppKit
import UniformTypeIdentifiers
import TesseraCore

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

extension AppModel {
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
        if added > 0 && summary.failures.isEmpty { notice = .info(summary.line) }
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
            catch { errorMessage = UserMessage.text(for: error, context: .add); return (0, 0, failures) }
        }
        return (added, duplicates, failures)
    }

    /// A user-facing reason from a thrown error, never exposing a secret.
    private func reasonText(_ error: Error) -> String {
        UserMessage.text(for: error, context: .add)
    }
}
