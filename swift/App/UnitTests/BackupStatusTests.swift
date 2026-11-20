import Darwin
import Foundation
import XCTest
@testable import Tessera

private struct FakeBookmarks: BackupFolderResolving {
    var made: Data? = Data("fresh".utf8)
    var resolutions: [Data: BookmarkResolution] = [:]

    func bookmark(for url: URL) -> Data? { made }
    func resolve(_ data: Data) -> BookmarkResolution? { resolutions[data] }
}

@MainActor
final class BackupStatusTests: XCTestCase {
    private let folder = URL(fileURLWithPath: "/Users/me/Backups", isDirectory: true)
    private let saved = Data("saved".utf8)
    private let fresh = Data("fresh".utf8)
    private let at = BackupDates.at(2026, 11, 20, 9)

    private var success: BackupResult { BackupResult(at: at, fileName: "tessera-backup-20261120.json", failure: nil) }
    private var failure: BackupResult {
        BackupResult(at: at, fileName: nil, failure: UserMessage.text(for: AutoBackupError.write(errno: ENOSPC)))
    }

    private func settings(saving data: Data?, resolver: FakeBookmarks,
                          last: BackupResult? = nil) throws -> (BackupSettings, UserDefaults) {
        let defaults = try makeThrowawayDefaults()
        if let data { defaults.set(data, forKey: BackupSettings.folderBookmarkKey) }
        if let last { defaults.set(try JSONEncoder().encode(last), forKey: BackupSettings.lastResultKey) }
        return (BackupSettings(defaults: defaults, resolver: resolver), defaults)
    }

    func testStatusFollowsTheFolderThenThePassphrase() {
        XCTAssertEqual(BackupStatus.derive(folder: .none, hasPassphraseWrap: true, last: success), .off)
        XCTAssertEqual(BackupStatus.derive(folder: .none, hasPassphraseWrap: false, last: nil), .off)
        XCTAssertEqual(BackupStatus.derive(folder: .unavailable, hasPassphraseWrap: true, last: success),
                       .folderUnavailable)
        XCTAssertEqual(BackupStatus.derive(folder: .available(folder), hasPassphraseWrap: false, last: success),
                       .needsPassphrase)
        XCTAssertEqual(BackupStatus.derive(folder: .available(folder), hasPassphraseWrap: true, last: nil),
                       .ready(last: nil))
        XCTAssertEqual(BackupStatus.derive(folder: .available(folder), hasPassphraseWrap: true, last: success),
                       .ready(last: success))
        XCTAssertEqual(BackupStatus.derive(folder: .available(folder), hasPassphraseWrap: true, last: failure),
                       .ready(last: failure))
    }

    func testNoBookmarkIsOff() throws {
        let (store, _) = try settings(saving: nil, resolver: FakeBookmarks())
        XCTAssertEqual(store.folder, .none)
        XCTAssertNil(store.folderURL)
        XCTAssertEqual(BackupStatus.derive(folder: store.folder, hasPassphraseWrap: true, last: store.lastResult), .off)
    }

    func testUnresolvableBookmarkIsUnavailable() throws {
        let (store, defaults) = try settings(saving: saved, resolver: FakeBookmarks())
        XCTAssertEqual(store.folder, .unavailable)
        XCTAssertNil(store.folderURL)
        XCTAssertEqual(defaults.data(forKey: BackupSettings.folderBookmarkKey), saved)
        XCTAssertEqual(BackupStatus.derive(folder: store.folder, hasPassphraseWrap: true, last: nil),
                       .folderUnavailable)
    }

    func testStaleBookmarkThatCannotBeRenewedIsUnavailable() throws {
        let resolver = FakeBookmarks(made: nil, resolutions: [saved: BookmarkResolution(url: folder, isStale: true)])
        let (store, defaults) = try settings(saving: saved, resolver: resolver)
        XCTAssertEqual(store.folder, .unavailable)
        XCTAssertEqual(defaults.data(forKey: BackupSettings.folderBookmarkKey), saved)
        XCTAssertEqual(BackupStatus.derive(folder: store.folder, hasPassphraseWrap: true, last: nil),
                       .folderUnavailable)
    }

    func testStaleBookmarkIsRenewedAndStored() throws {
        let resolver = FakeBookmarks(resolutions: [saved: BookmarkResolution(url: folder, isStale: true)])
        let (store, defaults) = try settings(saving: saved, resolver: resolver)
        XCTAssertEqual(store.folder, .available(folder))
        XCTAssertEqual(defaults.data(forKey: BackupSettings.folderBookmarkKey), fresh)
    }

    func testCurrentBookmarkResolvesToItsFolder() throws {
        let resolver = FakeBookmarks(resolutions: [saved: BookmarkResolution(url: folder, isStale: false)])
        let (store, defaults) = try settings(saving: saved, resolver: resolver, last: success)
        XCTAssertEqual(store.folderURL, folder)
        XCTAssertEqual(store.lastResult, success)
        XCTAssertEqual(defaults.data(forKey: BackupSettings.folderBookmarkKey), saved)
        XCTAssertEqual(BackupStatus.derive(folder: store.folder, hasPassphraseWrap: true, last: store.lastResult),
                       .ready(last: success))
    }

    func testRefreshNoticesAFolderThatStoppedResolving() throws {
        let resolver = FakeBookmarks(resolutions: [saved: BookmarkResolution(url: folder, isStale: false)])
        let (store, defaults) = try settings(saving: saved, resolver: resolver)
        defaults.set(Data("gone".utf8), forKey: BackupSettings.folderBookmarkKey)
        store.refresh()
        XCTAssertEqual(store.folder, .unavailable)
    }

    func testChoosingAFolderStoresItsBookmarkAndClearsTheLastResult() throws {
        let resolver = FakeBookmarks(resolutions: [fresh: BookmarkResolution(url: folder, isStale: false)])
        let (store, defaults) = try settings(saving: nil, resolver: resolver, last: failure)
        XCTAssertTrue(store.chooseFolder(folder))
        XCTAssertEqual(store.folder, .available(folder))
        XCTAssertNil(store.lastResult)
        XCTAssertEqual(defaults.data(forKey: BackupSettings.folderBookmarkKey), fresh)
        XCTAssertNil(defaults.data(forKey: BackupSettings.lastResultKey))
    }

    func testAFolderWithoutABookmarkIsNotChosen() throws {
        let (store, defaults) = try settings(saving: nil, resolver: FakeBookmarks(made: nil), last: success)
        XCTAssertFalse(store.chooseFolder(folder))
        XCTAssertEqual(store.folder, .none)
        XCTAssertEqual(store.lastResult, success)
        XCTAssertNil(defaults.data(forKey: BackupSettings.folderBookmarkKey))
    }

    func testTurningOffForgetsTheFolderAndTheLastResult() throws {
        let resolver = FakeBookmarks(resolutions: [saved: BookmarkResolution(url: folder, isStale: false)])
        let (store, defaults) = try settings(saving: saved, resolver: resolver, last: success)
        store.turnOff()
        XCTAssertEqual(store.folder, .none)
        XCTAssertNil(store.lastResult)
        XCTAssertNil(defaults.data(forKey: BackupSettings.folderBookmarkKey))
        XCTAssertNil(defaults.data(forKey: BackupSettings.lastResultKey))
    }

    func testRecordedResultSurvivesARelaunch() throws {
        let resolver = FakeBookmarks(resolutions: [saved: BookmarkResolution(url: folder, isStale: false)])
        let (store, defaults) = try settings(saving: saved, resolver: resolver)
        store.record(.success(success), at: at)
        XCTAssertEqual(store.lastResult, success)
        XCTAssertEqual(BackupSettings(defaults: defaults, resolver: resolver).lastResult, success)

        store.record(.failure(.write(errno: ENOSPC)), at: at)
        XCTAssertEqual(BackupSettings(defaults: defaults, resolver: resolver).lastResult, failure)
    }

    func testFailedOutcomesKeepTheirText() {
        XCTAssertEqual(BackupSettings.result(of: .success(success), at: at), success)
        XCTAssertEqual(BackupSettings.result(of: .failure(.write(errno: ENOSPC)), at: at), failure)
        XCTAssertEqual(BackupSettings.result(of: .failure(.prune(failed: 2)), at: at),
                       BackupResult(at: at, fileName: "tessera-backup-20261120.json",
                                    failure: UserMessage.text(for: AutoBackupError.prune(failed: 2))))
        XCTAssertEqual(BackupSettings.result(of: .failure(.needsPassphrase), at: at),
                       BackupResult(at: at, fileName: nil, failure: UserMessage.backupNeedsPassphrase))
    }

    func testOnlyTheFirstFailureAfterASuccessCounts() {
        XCTAssertTrue(BackupStatus.isFirstFailure(failure, after: nil))
        XCTAssertTrue(BackupStatus.isFirstFailure(failure, after: success))
        XCTAssertFalse(BackupStatus.isFirstFailure(failure, after: failure))
        XCTAssertFalse(BackupStatus.isFirstFailure(success, after: failure))
        XCTAssertFalse(BackupStatus.isFirstFailure(success, after: nil))
    }

    func testEveryErrorHasItsOwnText() {
        let errors: [AutoBackupError] = [.folderUnavailable, .needsPassphrase, .write(errno: ENOSPC),
                                         .write(errno: EACCES), .prune(failed: 0), .prune(failed: 1),
                                         .prune(failed: 3)]
        let texts = errors.map { UserMessage.text(for: $0) }
        XCTAssertEqual(Set(texts).count, errors.count)
        for (error, text) in zip(errors, texts) {
            XCTAssertEqual(UserMessage.text(for: error as Error, context: .settings), text)
            XCTAssertEqual(error.localizedDescription, text)
            XCTAssertFalse(text.contains("operation couldn"), text)
            XCTAssertFalse(text.contains("AutoBackupError"), text)
            XCTAssertTrue(text.hasSuffix("."), text)
        }
        XCTAssertTrue(UserMessage.text(for: AutoBackupError.write(errno: ENOSPC)).contains("no space left on device"))
        XCTAssertTrue(UserMessage.text(for: AutoBackupError.prune(failed: 3)).contains("3"))
    }

    func testEveryStatusHasItsOwnText() {
        let statuses: [BackupStatus] = [.off, .needsPassphrase, .folderUnavailable, .ready(last: nil)]
        let texts = statuses.map { UserMessage.text(for: $0) }
        XCTAssertEqual(Set(texts).count, statuses.count)
        XCTAssertFalse(texts.contains(""))
        XCTAssertEqual(UserMessage.text(for: BackupStatus.ready(last: failure)),
                       UserMessage.text(for: BackupStatus.ready(last: nil)))
    }

    func testLastBackupLineShowsTheFileOrTheFailure() {
        XCTAssertEqual(UserMessage.lastAutomaticBackup(nil), UserMessage.noAutomaticBackupYet)
        let line = UserMessage.lastAutomaticBackup(success)
        XCTAssertTrue(line.contains("tessera-backup-20261120.json"), line)
        XCTAssertTrue(line.contains(at.formatted(date: .abbreviated, time: .shortened)), line)
        XCTAssertEqual(UserMessage.lastAutomaticBackup(failure), failure.failure)
    }
}
