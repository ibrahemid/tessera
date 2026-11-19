import AppKit
import SwiftUI
import XCTest
import TesseraCore
@testable import Tessera

enum VaultFixture {
    static func account(_ id: String, issuer: String = "", account: String = "", type: OTPType = .totp,
                        period: Int = 30, folder: String = "", handle: String = "", tags: [String] = [],
                        pinned: Bool = false, updatedAt: Int64 = 0) -> Account {
        Account(id: id, type: type, issuer: issuer, account: account, secret: Data("12345678901234567890".utf8),
                period: period, folder: folder, handle: handle, tags: tags, pinned: pinned, updatedAt: updatedAt)
    }

    static func ids(_ accounts: [Account]) -> [String] { accounts.map(\.id) }
}

final class VaultListTests: XCTestCase {
    private let github = VaultFixture.account("g", issuer: "GitHub", account: "me@example.com", folder: "Work",
                                              handle: "gh", tags: ["dev", "Ops"])

    func testEmptyQueryMatchesEverything() {
        XCTAssertTrue(VaultList.matches(github, query: ""))
        XCTAssertTrue(VaultList.matches(github, query: "   "))
        XCTAssertTrue(VaultList.matches(github, query: "#"))
    }

    func testQueryMatchesIssuerAccountAliasFolderAndTags() {
        for q in ["git", "EXAMPLE", "gh", "work", "dev", "#ops", "ops"] {
            XCTAssertTrue(VaultList.matches(github, query: q), q)
        }
        XCTAssertFalse(VaultList.matches(github, query: "bank"))
    }

    func testEveryTermMustMatchSomeField() {
        XCTAssertTrue(VaultList.matches(github, query: "git #dev"))
        XCTAssertFalse(VaultList.matches(github, query: "git bank"))
    }

    func testQueryIgnoresDiacritics() {
        let cafe = VaultFixture.account("c", issuer: "Café")
        XCTAssertTrue(VaultList.matches(cafe, query: "cafe"))
    }

    func testAllScopeListsPinnedFirstThenOthersInSavedOrder() {
        let accounts = [VaultFixture.account("a"), VaultFixture.account("b", pinned: true),
                        VaultFixture.account("c"), VaultFixture.account("d", pinned: true)]
        let sections = VaultList.sections(accounts, scope: .all, query: "")
        XCTAssertEqual(sections.map(\.kind), [.pinned, .others])
        XCTAssertEqual(VaultFixture.ids(sections[0].accounts), ["b", "d"])
        XCTAssertEqual(VaultFixture.ids(sections[1].accounts), ["a", "c"])
        XCTAssertEqual(VaultFixture.ids(VaultList.visible(accounts, scope: .all, query: "")), ["b", "d", "a", "c"])
    }

    func testAllScopeWithoutPinsIsOneSection() {
        let accounts = [VaultFixture.account("a"), VaultFixture.account("b")]
        let sections = VaultList.sections(accounts, scope: .all, query: "")
        XCTAssertEqual(sections.map(\.kind), [.single])
        XCTAssertEqual(VaultFixture.ids(sections[0].accounts), ["a", "b"])
    }

    func testFolderAndPinnedScopesKeepSavedOrder() {
        let accounts = [VaultFixture.account("a", folder: "Work"), VaultFixture.account("b", folder: "Home", pinned: true),
                        VaultFixture.account("c", folder: "Work", pinned: true)]
        XCTAssertEqual(VaultFixture.ids(VaultList.visible(accounts, scope: .folder("Work"), query: "")), ["a", "c"])
        XCTAssertEqual(VaultFixture.ids(VaultList.visible(accounts, scope: .pinned, query: "")), ["b", "c"])
        XCTAssertEqual(VaultList.sections(accounts, scope: .folder("Work"), query: "").map(\.kind), [.single])
    }

    func testNoMatchesYieldsNoSections() {
        XCTAssertTrue(VaultList.sections([github], scope: .all, query: "zzz").isEmpty)
        XCTAssertTrue(VaultList.sections([], scope: .all, query: "").isEmpty)
    }

    func testOnlyUnfilteredAllIsReorderable() {
        XCTAssertTrue(VaultList.isReorderable(scope: .all, query: ""))
        XCTAssertTrue(VaultList.isReorderable(scope: .all, query: "  "))
        XCTAssertFalse(VaultList.isReorderable(scope: .all, query: "git"))
        XCTAssertFalse(VaultList.isReorderable(scope: .pinned, query: ""))
        XCTAssertFalse(VaultList.isReorderable(scope: .folder("Work"), query: ""))
    }

    func testReorderWithinSectionKeepsOtherAccountsInPlace() {
        let payload = [VaultFixture.account("a"), VaultFixture.account("b", pinned: true),
                       VaultFixture.account("c"), VaultFixture.account("d", pinned: true)]
        let out = VaultList.reorder(payload, section: ["a", "c"], fromOffsets: IndexSet(integer: 1), toOffset: 0)
        XCTAssertEqual(VaultFixture.ids(out), ["c", "b", "a", "d"])
    }

    func testReorderRejectsOffsetsOutsideTheSection() {
        let payload = [VaultFixture.account("a"), VaultFixture.account("b")]
        XCTAssertEqual(VaultFixture.ids(VaultList.reorder(payload, section: ["a", "b"],
                                                          fromOffsets: IndexSet(integer: 5), toOffset: 0)), ["a", "b"])
        XCTAssertEqual(VaultFixture.ids(VaultList.reorder(payload, section: ["a", "b"],
                                                          fromOffsets: IndexSet(integer: 0), toOffset: 9)), ["a", "b"])
        XCTAssertEqual(VaultFixture.ids(VaultList.reorder(payload, section: ["a", "x"],
                                                          fromOffsets: IndexSet(integer: 0), toOffset: 2)), ["a", "b"])
    }

    func testCountsCoverAllPinnedAndEachFolder() {
        let accounts = [VaultFixture.account("a", folder: "Work", pinned: true), VaultFixture.account("b", folder: "Work"),
                        VaultFixture.account("c", folder: "Home"), VaultFixture.account("d")]
        XCTAssertEqual(VaultList.counts(accounts), VaultCounts(all: 4, pinned: 1, folders: ["Work": 2, "Home": 1]))
    }

    func testTagsAreUniqueAndSorted() {
        let accounts = [VaultFixture.account("a", tags: ["ops", "beta"]), VaultFixture.account("b", tags: ["Alpha", "ops"]),
                        VaultFixture.account("c")]
        XCTAssertEqual(VaultList.tags(in: accounts), ["Alpha", "beta", "ops"])
    }

    func testDragPayloadCarriesTheWholeSelectionOnlyWhenDraggingASelectedRow() {
        var selection = VaultSelection()
        let order = ["a", "b", "c"]
        XCTAssertEqual(VaultList.dragPayload(for: "b", selection: selection, order: order), "b")
        selection.toggle("c", in: order)
        selection.toggle("a", in: order)
        XCTAssertEqual(VaultList.dragPayload(for: "c", selection: selection, order: order), "a\nc")
        XCTAssertEqual(VaultList.dragPayload(for: "b", selection: selection, order: order), "b")
    }

    func testDroppedIDsKeepOnlyKnownAccounts() {
        XCTAssertEqual(VaultList.droppedIDs(["a\nc", "zz", "b"], known: ["a", "b", "c"]), ["a", "b", "c"])
        XCTAssertEqual(VaultList.droppedIDs(["/tmp/file.png"], known: ["a"]), [])
    }

    func testRowClickReadsModifiers() {
        XCTAssertEqual(RowClick(modifiers: []), .activate)
        XCTAssertEqual(RowClick(modifiers: .command), .toggle)
        XCTAssertEqual(RowClick(modifiers: .shift), .extend)
        XCTAssertEqual(RowClick(modifiers: [.command, .shift]), .toggle)
        XCTAssertEqual(RowClick(modifiers: .option), .activate)
    }

    func testTypeToSearchAcceptsPrintableTextOnly() {
        XCTAssertEqual(TypeToSearch.text(from: "g", modifiers: []), "g")
        XCTAssertEqual(TypeToSearch.text(from: "G", modifiers: .shift), "G")
        XCTAssertNil(TypeToSearch.text(from: "c", modifiers: .command))
        XCTAssertNil(TypeToSearch.text(from: "c", modifiers: .control))
        XCTAssertNil(TypeToSearch.text(from: "c", modifiers: .option))
        XCTAssertNil(TypeToSearch.text(from: "", modifiers: []))
        XCTAssertNil(TypeToSearch.text(from: "\r", modifiers: []))
        XCTAssertNil(TypeToSearch.text(from: "\u{1B}", modifiers: []))
        XCTAssertNil(TypeToSearch.text(from: "\u{F700}", modifiers: []))
    }

    func testKeyRouteMovesWithArrowsWhileSearchHasFocus() {
        let down = String(UnicodeScalar(UInt16(NSDownArrowFunctionKey))!)
        let up = String(UnicodeScalar(UInt16(NSUpArrowFunctionKey))!)
        XCTAssertEqual(VaultKeyRoute.route(key: .downArrow, characters: down, modifiers: .numericPad,
                                           searchFocused: true), .move(by: 1, extending: false))
        XCTAssertEqual(VaultKeyRoute.route(key: .upArrow, characters: up, modifiers: .shift,
                                           searchFocused: true), .move(by: -1, extending: true))
        XCTAssertNil(VaultKeyRoute.route(key: .downArrow, characters: down, modifiers: .command, searchFocused: true))
        XCTAssertNil(VaultKeyRoute.route(key: .downArrow, characters: down, modifiers: [], searchFocused: false))
        XCTAssertNil(VaultKeyRoute.route(key: .leftArrow, characters: "", modifiers: [], searchFocused: true))
    }

    func testKeyRouteTypesIntoSearchOnlyFromElsewhere() {
        XCTAssertEqual(VaultKeyRoute.route(key: KeyEquivalent("g"), characters: "g", modifiers: [], searchFocused: false),
                       .type("g"))
        XCTAssertNil(VaultKeyRoute.route(key: KeyEquivalent("g"), characters: "g", modifiers: [], searchFocused: true))
        XCTAssertNil(VaultKeyRoute.route(key: KeyEquivalent("c"), characters: "c", modifiers: .command, searchFocused: false))
    }
}
