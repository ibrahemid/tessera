import Foundation
import CoreGraphics
import LocalAuthentication
import ScreenCaptureKit
import TesseraCore

enum AccountEntryError: Error {
    case invalidSetupKey
    case duplicate
}

enum UserMessage {
    enum Context { case unlock, restore, add, scan, settings, save, export }

    static let screenRecordingDenied = "Tessera needs Screen Recording permission to scan."
    static let screenRecordingSettingsURL =
        URL(string: "x-apple.systempreferences:com.apple.preference.security?Privacy_ScreenCapture")!

    static func text(for error: Error, context: Context, url: URL? = nil) -> String {
        if isScreenRecordingDenied(error) { return screenRecordingDenied }
        if let e = error as? AccountEntryError { return text(for: e) }
        if let e = error as? VaultError { return text(for: e, context: context) }
        if let e = error as? VaultWriteError { return text(for: e) }
        if let e = error as? VaultLockError { return text(for: e) }
        if let e = error as? AccountError { return text(for: e) }
        if let e = error as? Importers.ImporterError { return sentence(e.description) }
        if error is XChaCha.CryptoError || error is DecodingError { return damagedFile(context) }
        if let e = error as? AppKey.AppKeyError { return text(for: e) }
        if let e = error as? SecureEnclaveWrap.SEError { return text(for: e) }
        if let e = error as? QRCapture.CaptureError { return text(for: e) }
        if let e = error as? LAError, let t = text(for: e.code) { return t }
        if let c = error as? CocoaError, fileWriteCodes.contains(c.code),
           let target = url ?? c.url ?? c.filePath.map({ URL(fileURLWithPath: $0) }) {
            let line = "Couldn’t save to \(target.lastPathComponent): \(error.localizedDescription)"
            return context == .export ? line + " Choose another folder." : line
        }
        return error.localizedDescription
    }

    static func textUnlessCancelled(for error: Error, context: Context, url: URL? = nil) -> String? {
        isUserCancel(error) ? nil : text(for: error, context: context, url: url)
    }

    static func isUserCancel(_ error: Error) -> Bool {
        if case AppKey.AppKeyError.keychain(let s) = error, s == errSecUserCanceled { return true }
        if let la = error as? LAError, [.userCancel, .systemCancel, .appCancel].contains(la.code) { return true }
        let ns = error as NSError
        if ns.domain == LAErrorDomain, [-2, -4, -9].contains(ns.code) { return true }
        return ns.code == Int(errSecUserCanceled)
    }

    static func isScreenRecordingDenied(_ error: Error) -> Bool {
        let ns = error as NSError
        guard ns.domain == SCStreamErrorDomain else { return false }
        return ns.code == SCStreamError.Code.userDeclined.rawValue || !CGPreflightScreenCaptureAccess()
    }

    private static let fileWriteCodes: Set<CocoaError.Code> = [
        .fileWriteUnknown, .fileWriteNoPermission, .fileWriteInvalidFileName, .fileWriteFileExists,
        .fileWriteInapplicableStringEncoding, .fileWriteUnsupportedScheme, .fileWriteOutOfSpace,
        .fileWriteVolumeReadOnly,
    ]

    private static let invalidSetupKey = "That setup key isn’t valid. It uses letters A to Z and digits 2 to 7."

    private static func text(for e: AccountEntryError) -> String {
        switch e {
        case .invalidSetupKey: return invalidSetupKey
        case .duplicate: return "This account is already in your vault."
        }
    }

    private static func text(for e: VaultError, context: Context) -> String {
        switch e {
        case .wrongPassphrase:
            switch context {
            case .unlock: return "That passphrase doesn’t open this vault."
            case .restore: return "That password doesn’t open this backup."
            case .add, .scan, .settings, .save, .export: return "Unlock the vault first."
            }
        case .noPassphraseWrap:
            return context == .restore
                ? "This file has no backup password. Choose a file made with Export encrypted backup."
                : "This vault has no passphrase. Open it with Touch ID."
        case .corrupt, .unsupportedVersion, .unsupportedAEAD, .badBase64:
            return damagedFile(context)
        }
    }

    private static func damagedFile(_ context: Context) -> String {
        context == .restore
            ? "This file is damaged or from a newer Tessera. Choose another backup."
            : "This file is damaged or from a newer Tessera. Restore from a backup."
    }

    private static func text(for e: AccountError) -> String {
        switch e {
        case .invalid(let raw):
            var m = Substring(raw)
            if m.hasPrefix("account "), let r = m.range(of: ": ") { m = m[r.upperBound...] }
            if m.hasPrefix("base32:") { return invalidSetupKey }
            return sentence(String(m))
        }
    }

    private static func text(for e: AppKey.AppKeyError) -> String {
        switch e {
        case .keychain(let s):
            return "Couldn’t read the vault key from the Keychain (error \(s)). Lock and unlock your Mac, then try again."
        case .missing:
            return "Couldn’t find the vault key in the Keychain. Quit and reopen Tessera."
        }
    }

    private static func text(for e: SecureEnclaveWrap.SEError) -> String {
        switch e {
        case .notAvailable: return "This Mac has no Secure Enclave, so Touch ID can’t protect the vault."
        case .noWrap: return "This vault has no Touch ID key for this Mac."
        case .deriveFailed: return "The Secure Enclave couldn’t unlock the vault key. Try again."
        }
    }

    private static func text(for e: QRCapture.CaptureError) -> String {
        switch e {
        case .noDisplay: return "No display is available to scan. Wake the display and scan again."
        case .noQRFound: return "No 2FA QR code is visible. Show the QR code on screen and scan again."
        }
    }

    private static func text(for code: LAError.Code) -> String? {
        switch code {
        case .authenticationFailed: return "Touch ID didn’t recognize that finger. Try again."
        case .biometryLockout:
            return "Touch ID is locked after too many tries. Lock and unlock your Mac with its password, then try again."
        default: return nil
        }
    }

    private static func sentence(_ s: String) -> String {
        let t = s.trimmingCharacters(in: .whitespaces)
        guard let first = t.first else { return t }
        let body = first.uppercased() + t.dropFirst()
        return body.hasSuffix(".") ? body : body + "."
    }
}
