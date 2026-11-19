import Foundation
import ImageIO
import UniformTypeIdentifiers
import TesseraCore

/// Files read so far in a running import, for the progress line.
struct ImportProgress: Equatable, Sendable {
    var done: Int
    var total: Int
}

/// Everything a batch of files yielded before merging: accounts to add, items
/// that failed, and Tessera encrypted backups, which restore with a password
/// instead of importing.
struct ImportScan: Sendable {
    var accounts: [Account] = []
    var failures: [ImportItemFailure] = []
    var backups: [URL] = []
}

/// Reads and classifies dropped or picked files. Pure file work with no app
/// state, so it runs off the main thread.
enum ImportScanner {
    /// Scan every file in order, calling `progress` with the count done after
    /// each one. One unreadable file never stops the batch.
    static func scan(_ urls: [URL], progress: @Sendable (Int) -> Void = { _ in }) -> ImportScan {
        var result = ImportScan()
        for (i, url) in urls.enumerated() {
            scanFile(url, into: &result)
            progress(i + 1)
        }
        return result
    }

    static func isTesseraBackup(_ data: Data) -> Bool {
        (try? Envelope.decode(data)) != nil
    }

    /// Content type first, extension as the fallback for files without metadata.
    static func isImage(_ url: URL) -> Bool {
        if let type = try? url.resourceValues(forKeys: [.contentTypeKey]).contentType {
            return type.conforms(to: .image)
        }
        let ext = url.pathExtension.lowercased()
        return ["png", "jpg", "jpeg", "heic", "webp", "tiff", "tif", "gif", "bmp"].contains(ext)
    }

    private static func scanFile(_ url: URL, into result: inout ImportScan) {
        let name = url.lastPathComponent
        let scoped = url.startAccessingSecurityScopedResource()
        defer { if scoped { url.stopAccessingSecurityScopedResource() } }

        if isImage(url) {
            scanImage(url, name: name, into: &result)
            return
        }
        guard let data = try? Data(contentsOf: url) else {
            result.failures.append(ImportItemFailure(source: name, display: "", reason: "Couldn’t read this file."))
            return
        }
        if isTesseraBackup(data) {
            result.backups.append(url)
            return
        }
        // A recognized app export wins; its error (an encrypted export, say)
        // says more than a generic line-parse failure.
        do {
            if let found = try Importers.parse(data) {
                result.accounts.append(contentsOf: found.accounts)
                return
            }
        } catch {
            result.failures.append(ImportItemFailure(source: name, display: "", reason: reason(error)))
            return
        }
        guard let text = String(data: data, encoding: .utf8) else {
            result.failures.append(ImportItemFailure(source: name, display: "", reason: "Not a QR image or a file Tessera can import."))
            return
        }
        let (accounts, errors) = InputDetect.parseText(text)
        if accounts.isEmpty && errors.isEmpty {
            result.failures.append(ImportItemFailure(source: name, display: "", reason: "Nothing to import in this file."))
            return
        }
        result.accounts.append(contentsOf: accounts)
        result.failures.append(contentsOf: errors.map {
            ImportItemFailure(source: "\(name) line \($0.line)", display: $0.display, reason: $0.reason)
        })
    }

    private static func scanImage(_ url: URL, name: String, into result: inout ImportScan) {
        guard let source = CGImageSourceCreateWithURL(url as CFURL, nil),
              let image = CGImageSourceCreateImageAtIndex(source, 0, nil) else {
            result.failures.append(ImportItemFailure(source: name, display: "", reason: "Couldn’t read this image."))
            return
        }
        let payloads: [String]
        do { payloads = try QRCapture.detectAll(in: image) }
        catch {
            result.failures.append(ImportItemFailure(source: name, display: "", reason: reason(error)))
            return
        }
        if payloads.isEmpty {
            result.failures.append(ImportItemFailure(source: name, display: "", reason: "No QR code found.", kind: .noQR))
            return
        }
        for payload in payloads {
            let (accounts, errors) = InputDetect.parseText(payload)
            result.accounts.append(contentsOf: accounts)
            result.failures.append(contentsOf: errors.map {
                ImportItemFailure(source: name, display: $0.display, reason: $0.reason)
            })
        }
    }

    private static func reason(_ error: Error) -> String {
        UserMessage.text(for: error, context: .add)
    }
}
