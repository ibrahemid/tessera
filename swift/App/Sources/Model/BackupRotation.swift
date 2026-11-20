import Foundation

enum BackupRotation {
    static let retention = 30

    private static let prefix = Array("tessera-backup-".utf8)
    private static let suffix = Array(".json".utf8)
    private static let digitCount = 8
    private static let secondsPerDay = 86_400.0

    static func fileName(for date: Date) -> String {
        let (year, month, day) = civil(fromDays: dayNumber(of: date))
        return String(decoding: prefix, as: UTF8.self) + padded(year, width: 4) + padded(month, width: 2)
            + padded(day, width: 2) + String(decoding: suffix, as: UTF8.self)
    }

    static func date(fromName name: String) -> Date? {
        dayNumber(fromName: name).map { Date(timeIntervalSince1970: Double($0) * secondsPerDay) }
    }

    static func isBackupName(_ name: String) -> Bool { digits(of: name) != nil }

    static func namesToDelete(_ names: [String], keep: String, rotated: Bool, today: Date) -> [String] {
        let todayNumber = dayNumber(of: today)
        var kept = 1
        var doomed: [String] = []
        for name in names.filter(isBackupName).sorted(by: >) where name != keep {
            if !rotated {
                guard let day = dayNumber(fromName: name), day <= todayNumber else { continue }
                if kept < retention {
                    kept += 1
                    continue
                }
            }
            doomed.append(name)
        }
        return doomed
    }

    private static func digits(of name: String) -> [Int64]? {
        let bytes = Array(name.utf8)
        guard bytes.count == prefix.count + digitCount + suffix.count,
              bytes.starts(with: prefix), bytes.suffix(suffix.count).elementsEqual(suffix) else { return nil }
        let body = bytes[prefix.count..<(prefix.count + digitCount)]
        guard body.allSatisfy({ $0 >= 0x30 && $0 <= 0x39 }) else { return nil }
        return body.map { Int64($0 - 0x30) }
    }

    private static func dayNumber(fromName name: String) -> Int64? {
        guard let d = digits(of: name) else { return nil }
        let year = d[0] * 1000 + d[1] * 100 + d[2] * 10 + d[3]
        let month = d[4] * 10 + d[5]
        let day = d[6] * 10 + d[7]
        guard (1...12).contains(month), day >= 1, day <= daysIn(month: month, year: year) else { return nil }
        return days(fromCivil: year, month, day)
    }

    private static func dayNumber(of date: Date) -> Int64 {
        Int64((date.timeIntervalSince1970 / secondsPerDay).rounded(.down))
    }

    private static func daysIn(month: Int64, year: Int64) -> Int64 {
        switch month {
        case 2: return year % 4 == 0 && (year % 100 != 0 || year % 400 == 0) ? 29 : 28
        case 4, 6, 9, 11: return 30
        default: return 31
        }
    }

    private static func days(fromCivil year: Int64, _ month: Int64, _ day: Int64) -> Int64 {
        let y = month <= 2 ? year - 1 : year
        let era = (y >= 0 ? y : y - 399) / 400
        let yearOfEra = y - era * 400
        let dayOfYear = (153 * ((month + 9) % 12) + 2) / 5 + day - 1
        let dayOfEra = yearOfEra * 365 + yearOfEra / 4 - yearOfEra / 100 + dayOfYear
        return era * 146_097 + dayOfEra - 719_468
    }

    private static func civil(fromDays number: Int64) -> (Int64, Int64, Int64) {
        let z = number + 719_468
        let era = (z >= 0 ? z : z - 146_096) / 146_097
        let dayOfEra = z - era * 146_097
        let yearOfEra = (dayOfEra - dayOfEra / 1460 + dayOfEra / 36524 - dayOfEra / 146_096) / 365
        let dayOfYear = dayOfEra - (365 * yearOfEra + yearOfEra / 4 - yearOfEra / 100)
        let mp = (5 * dayOfYear + 2) / 153
        let day = dayOfYear - (153 * mp + 2) / 5 + 1
        let month = mp < 10 ? mp + 3 : mp - 9
        return (yearOfEra + era * 400 + (month <= 2 ? 1 : 0), month, day)
    }

    private static func padded(_ value: Int64, width: Int) -> String {
        let digits = String(value.magnitude)
        let zeros = String(repeating: "0", count: max(0, width - digits.count))
        return (value < 0 ? "-" : "") + zeros + digits
    }
}
