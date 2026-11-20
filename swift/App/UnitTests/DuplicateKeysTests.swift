import XCTest
import TesseraCore
@testable import Tessera

final class DuplicateKeysTests: XCTestCase {
    private let dropbox = VaultFixture.account("new", issuer: "Dropbox", account: "me")

    func testKeyIsTheCoreContentKey() {
        let variant = VaultFixture.account("2", issuer: "DROPBOX", account: "ME")
        XCTAssertEqual(DuplicateKeys.key(dropbox), Merge.ContentKey(dropbox))
        XCTAssertEqual(DuplicateKeys.key(dropbox), DuplicateKeys.key(variant))
        var otherSecret = variant
        otherSecret.secret = Data("abcdefghijabcdefghij".utf8)
        XCTAssertNotEqual(DuplicateKeys.key(dropbox), DuplicateKeys.key(otherSecret))
    }

    func testATrashedAccountIsNotADuplicate() {
        let trashed = TrashFixture.vault()
        XCTAssertTrue(trashed.contains { DuplicateKeys.key($0) == DuplicateKeys.key(dropbox) })
        XCTAssertFalse(DuplicateKeys.live(in: trashed).contains(DuplicateKeys.key(dropbox)))
    }

    func testALiveAccountIsADuplicate() {
        var accounts = TrashFixture.vault()
        VaultEdit.restore(["c"], at: 1_800_000_000, in: &accounts)
        XCTAssertTrue(DuplicateKeys.live(in: accounts).contains(DuplicateKeys.key(dropbox)))
        XCTAssertEqual(DuplicateKeys.live(in: accounts).count, 2)
    }
}
