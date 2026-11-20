import XCTest
import TesseraCore
@testable import Tessera

final class InterfaceFolderRemovalTests: XCTestCase {
    private let accounts = [VaultFixture.account("a", folder: "Work"),
                            VaultFixture.account("b", folder: "Work"),
                            VaultFixture.account("c", folder: "Home"),
                            VaultFixture.account("d")]

    func testCountsOnlyTheAccountsInThatFolder() {
        XCTAssertEqual(FolderRemoval(folder: "Work", accounts: accounts).count, 2)
        XCTAssertEqual(FolderRemoval(folder: "Home", accounts: accounts).count, 1)
        XCTAssertEqual(FolderRemoval(folder: "Travel", accounts: accounts).count, 0)
    }

    func testFolderNamesAreMatchedExactly() {
        XCTAssertEqual(FolderRemoval(folder: "work", accounts: accounts).count, 0)
    }

    func testTitleNamesTheFolder() {
        XCTAssertEqual(FolderRemoval(folder: "Work", accounts: accounts).title, "Remove the Work folder?")
    }

    func testMessageStatesHowManyAccountsLeaveTheFolder() {
        XCTAssertEqual(FolderRemoval(folder: "Work", accounts: accounts).message,
                       "2 accounts in it stay in your vault, outside any folder.")
        XCTAssertEqual(FolderRemoval(folder: "Home", accounts: accounts).message,
                       "1 account in it stays in your vault, outside any folder.")
        XCTAssertEqual(FolderRemoval(folder: "Travel", accounts: accounts).message,
                       "0 accounts in it stay in your vault, outside any folder.")
    }

    func testIdentityFollowsTheFolder() {
        XCTAssertEqual(FolderRemoval(folder: "Work", accounts: accounts).id, "Work")
        XCTAssertEqual(FolderRemoval(folder: "Work", accounts: accounts),
                       FolderRemoval(folder: "Work", accounts: Array(accounts.prefix(2))))
    }
}
