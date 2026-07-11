import XCTest
@testable import TesseraCore

/// Exhaustive truth tables for the data-loss-critical vault routing. These are the
/// rules behind the CLI↔app shared-vault fix: an external vault that's missing or
/// unresolvable must never silently become (or create) the container vault, and
/// the app must never route an external vault toward deletion.
final class VaultResolutionTests: XCTestCase {

    func testPreLoadTruthTable() {
        // (hasBookmark, bookmarkResolves, fileExists) -> PreLoad
        let cases: [(Bool, Bool, Bool, VaultOpen.PreLoad)] = [
            // No bookmark: built-in container vault.
            (false, false, false, .createContainer),   // first run
            (false, false, true,  .loadExisting),       // existing container
            (false, true,  false, .createContainer),    // resolves irrelevant without a bookmark
            (false, true,  true,  .loadExisting),
            // Bookmark configured but unresolvable this launch: relocate, never fall
            // back to or create the container — even if a container file exists.
            (true,  false, false, .unreachable),
            (true,  false, true,  .unreachable),
            // Bookmark resolved (external vault).
            (true,  true,  false, .unreachable),        // external file missing: relocate, never create
            (true,  true,  true,  .loadExisting),       // external file present: load it
        ]
        for (hasBookmark, resolves, exists, want) in cases {
            let got = VaultOpen.preLoad(hasBookmark: hasBookmark, bookmarkResolves: resolves, fileExists: exists)
            XCTAssertEqual(got, want, "preLoad(hasBookmark:\(hasBookmark), resolves:\(resolves), exists:\(exists))")
        }
    }

    func testNoSilentWrapTruthTable() {
        // (hasPassphraseWrap, isExternal) -> NoSilentWrap
        let cases: [(Bool, Bool, VaultOpen.NoSilentWrap)] = [
            (true,  false, .passphrase),   // container CLI vault: ask for the passphrase
            (true,  true,  .passphrase),   // external CLI vault: ask for the passphrase
            (false, false, .reset),        // container, unrecoverable: offer reset
            (false, true,  .unreachable),  // external, no passphrase: relocate, never delete
        ]
        for (hasPassphrase, isExternal, want) in cases {
            let got = VaultOpen.noSilentWrap(hasPassphraseWrap: hasPassphrase, isExternal: isExternal)
            XCTAssertEqual(got, want, "noSilentWrap(hasPassphraseWrap:\(hasPassphrase), isExternal:\(isExternal))")
        }
    }
}
