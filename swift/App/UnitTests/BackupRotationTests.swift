import Foundation
import XCTest
@testable import Tessera

enum BackupDates {
    static func at(_ year: Int, _ month: Int, _ day: Int, _ hour: Int = 0, _ minute: Int = 0, _ second: Int = 0,
                   millisecond: Int = 0, offset: Int = 0) -> Date {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = TimeZone(secondsFromGMT: offset) ?? .gmt
        let components = DateComponents(year: year, month: month, day: day, hour: hour, minute: minute,
                                        second: second)
        let whole = calendar.date(from: components) ?? Date(timeIntervalSince1970: 0)
        return whole.addingTimeInterval(Double(millisecond) / 1000)
    }

    static func adding(days: Int, to date: Date) -> Date {
        date.addingTimeInterval(Double(days) * 86_400)
    }

    static func name(_ date: Date) -> String { BackupRotation.fileName(for: date) }
}

final class BackupRotationTests: XCTestCase {
    func testFileNameIsTheUTCDay() {
        XCTAssertEqual(BackupRotation.fileName(for: BackupDates.at(2026, 11, 20, 23, 30, offset: -5 * 3600)),
                       "tessera-backup-20261121.json")
        XCTAssertEqual(BackupRotation.fileName(for: BackupDates.at(2026, 11, 21, 8, 0, offset: 14 * 3600)),
                       "tessera-backup-20261120.json")
        XCTAssertEqual(BackupRotation.fileName(for: BackupDates.at(2026, 1, 2)), "tessera-backup-20260102.json")
        XCTAssertEqual(BackupRotation.fileName(for: BackupDates.at(2026, 11, 20, 23, 59, 59, millisecond: 999)),
                       "tessera-backup-20261120.json")
        XCTAssertEqual(BackupRotation.fileName(for: BackupDates.at(2026, 11, 21)), "tessera-backup-20261121.json")
    }

    func testDateReadsOnlyExactNames() {
        XCTAssertEqual(BackupRotation.date(fromName: "tessera-backup-20261120.json"), BackupDates.at(2026, 11, 20))
        for name in ["tessera-backup-2026112.json", "tessera-backup-20261120.json.bak",
                     "x-tessera-backup-20261120.json", "Tessera-backup-20261120.json",
                     "tessera-backup-2026112a.json", "tessera-backup-20261120.JSON",
                     "tessera-backup-\u{FF12}0261120.json", "tessera-backup-202611201.json", ""] {
            XCTAssertNil(BackupRotation.date(fromName: name), name)
            XCTAssertFalse(BackupRotation.isBackupName(name), name)
        }
    }

    func testDateAcceptsOnlyRealDaysAsGoParsesThem() {
        let real: [(String, TimeInterval)] = [
            ("00000101", -62_167_219_200), ("00000229", -62_162_121_600), ("00040229", -62_035_891_200),
            ("20000229", 951_782_400), ("20240229", 1_709_164_800), ("99991231", 253_402_214_400),
        ]
        for (digits, seconds) in real {
            let name = "tessera-backup-\(digits).json"
            XCTAssertEqual(BackupRotation.date(fromName: name)?.timeIntervalSince1970, seconds, digits)
            XCTAssertEqual(BackupRotation.date(fromName: name).map(BackupRotation.fileName(for:)), name, digits)
        }
        for digits in ["19000229", "20250229", "20260230", "20261300", "00000000", "20261100", "20261131"] {
            let name = "tessera-backup-\(digits).json"
            XCTAssertNil(BackupRotation.date(fromName: name), digits)
            XCTAssertTrue(BackupRotation.isBackupName(name), digits)
        }
    }

    func testKeepsTheThirtyNewestExactMatches() {
        let start = BackupDates.at(2026, 1, 1, 12)
        let dated = (0..<35).map { BackupDates.name(BackupDates.adding(days: $0, to: start)) }
        let others = ["tessera-backup-2025010.json", "tessera-backup-20250101.json.bak",
                      "Tessera-backup-20250101.json", "tessera-backup-20250101.json~",
                      "tessera-backup-2025010a.json", "notes.txt"]
        let at = BackupDates.at(2026, 3, 1, 9)
        let keep = BackupDates.name(at)
        let doomed = BackupRotation.namesToDelete((dated + others + [keep]).shuffled(), keep: keep, rotated: false,
                                                  today: at)
        XCTAssertEqual(doomed, Array(dated[..<6].reversed()))
    }

    func testRotationDeletesEveryOtherMatchingName() {
        let at = BackupDates.at(2026, 11, 20, 11)
        let keep = BackupDates.name(at)
        let names = ["tessera-backup-20261101.json", "tessera-backup-20261119.json", "tessera-backup-20991231.json",
                     "tessera-backup-20261300.json", "keep-me.json", keep]
        XCTAssertEqual(BackupRotation.namesToDelete(names, keep: keep, rotated: false, today: at), [])
        XCTAssertEqual(BackupRotation.namesToDelete(names, keep: keep, rotated: true, today: at),
                       ["tessera-backup-20991231.json", "tessera-backup-20261300.json",
                        "tessera-backup-20261119.json", "tessera-backup-20261101.json"])
    }

    func testRetentionCountsOnlyRealDates() {
        let day = BackupDates.at(2026, 11, 20, 12)
        let real = (1...10).map { BackupDates.name(BackupDates.adding(days: -$0, to: day)) }
        let fake = (0..<(BackupRotation.retention - 1)).map {
            String(format: "tessera-backup-9999%04d.json", Int32($0))
        }
        let keep = BackupDates.name(day)
        XCTAssertEqual(BackupRotation.namesToDelete(real + fake + [keep], keep: keep, rotated: false, today: day), [])
    }

    func testRetentionNeitherCountsNorPrunesUnrankedNames() {
        let day = BackupDates.at(2026, 11, 20, 12)
        let unranked = ["tessera-backup-20261121.json", "tessera-backup-20991231.json", "tessera-backup-20260230.json",
                        "tessera-backup-20261300.json", "tessera-backup-00000000.json"]
        let real = (1...(BackupRotation.retention + 2)).map { BackupDates.name(BackupDates.adding(days: -$0, to: day)) }
        let keep = BackupDates.name(day)
        let doomed = BackupRotation.namesToDelete(unranked + real + [keep], keep: keep, rotated: false, today: day)
        XCTAssertEqual(doomed, Array(real.suffix(3)))
    }

    func testTodayIsTheUTCDayOfTheWrite() {
        let keep = "tessera-backup-20261120.json"
        let tomorrow = "tessera-backup-20261121.json"
        let earlier = (1...(BackupRotation.retention - 1)).map {
            BackupDates.name(BackupDates.adding(days: -$0, to: BackupDates.at(2026, 11, 20)))
        }
        let names = earlier + [keep, tomorrow]
        let lastMillisecond = BackupDates.at(2026, 11, 20, 23, 59, 59, millisecond: 999)
        XCTAssertEqual(BackupRotation.namesToDelete(names, keep: keep, rotated: false, today: lastMillisecond), [])
        let nextDay = BackupDates.at(2026, 11, 20, 16, offset: -8 * 3600)
        XCTAssertEqual(BackupRotation.namesToDelete(names, keep: keep, rotated: false, today: nextDay),
                       [earlier[earlier.count - 1]])
    }

    func testNeverReturnsTheKeptNameOrANonMatchingName() {
        let day = BackupDates.at(2026, 11, 20)
        let keep = BackupDates.name(day)
        let names = [keep, "notes.json", "tessera-backup-20261120.json.bak", "vault.json"]
        XCTAssertEqual(BackupRotation.namesToDelete(names, keep: keep, rotated: true, today: day), [])
    }
}
