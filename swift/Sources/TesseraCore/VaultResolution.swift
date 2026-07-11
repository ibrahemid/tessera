import Foundation

/// Pure routing decisions for opening a vault, extracted so the data-loss-critical
/// rules are unit-testable without the app target or a sandboxed run:
///
/// - An external vault (a file shared with the `tess` CLI) that is configured but
///   missing or unresolvable must NEVER silently fall back to — or create — the
///   built-in container vault. That is how accounts appear to vanish.
/// - The app must NEVER offer to delete an external file it did not create; when
///   an external vault can't be opened, the only outs are relocate or detach.
///
/// The app layer (`VaultStore` / `AppModel`) performs the IO — resolving
/// bookmarks, loading bytes, decrypting — and consumes these decisions.
public enum VaultOpen {

    /// What to do before loading, from the vault source and whether a file exists.
    /// `isExternal` is derived as `hasBookmark && bookmarkResolves`.
    public enum PreLoad: Sendable, Equatable {
        /// External vault configured but missing/unresolvable: relocate or detach,
        /// never fall back to or create the container, never delete.
        case unreachable
        /// Built-in vault with no file yet: create a fresh one.
        case createContainer
        /// A file is present and reachable: load it and inspect its wraps.
        case loadExisting
    }

    public static func preLoad(hasBookmark: Bool, bookmarkResolves: Bool, fileExists: Bool) -> PreLoad {
        if hasBookmark && !bookmarkResolves { return .unreachable }
        let isExternal = hasBookmark && bookmarkResolves
        if fileExists { return .loadExisting }
        return isExternal ? .unreachable : .createContainer
    }

    /// What to do when a loaded vault carries no wrap this Mac can open silently
    /// (no Secure Enclave wrap and no Keychain app key).
    public enum NoSilentWrap: Sendable, Equatable {
        /// A passphrase wrap exists (e.g. a `tess` CLI vault): ask for it.
        case passphrase
        /// External and no passphrase: relocate/detach, never delete the CLI's file.
        case unreachable
        /// Built-in and otherwise unrecoverable: offer to reset.
        case reset
    }

    public static func noSilentWrap(hasPassphraseWrap: Bool, isExternal: Bool) -> NoSilentWrap {
        if hasPassphraseWrap { return .passphrase }
        return isExternal ? .unreachable : .reset
    }
}
