import Foundation

extension Importers {

    /// Describes one registered CSV export: its source name and the column
    /// indexes the shared OTP-value rule needs.
    struct CSVFormat {
        let name: String
        let titleCol: Int
        let userCol: Int
        let otpCol: Int
    }

    /// Maps an exact header line to its format. Apple publishes no schema for
    /// its CSV, so the header comes from real exports: if Apple changes it,
    /// detection stops matching and the file is reported as unrecognized rather
    /// than parsed against the wrong columns.
    static let csvHeaders: [String: CSVFormat] = [
        "Title,URL,Username,Password,Notes,OTPAuth":
            CSVFormat(name: "Apple Passwords", titleCol: 0, userCol: 2, otpCol: 5),
        "Title,Url,Username,Password,OTPAuth,Favorite,Archived,Tags,Notes":
            CSVFormat(name: "1Password", titleCol: 0, userCol: 2, otpCol: 4),
    ]

    /// The longest registered header is well under this; a first line longer
    /// than the cap cannot match one anyway.
    private static let maxCSVHeaderBytes = 512

    /// Compares the first line of data, byte for byte, against the registered
    /// export headers, after stripping a UTF-8 BOM and a trailing `\r`.
    static func matchCSVHeader(_ data: Data) -> CSVFormat? {
        let bytes = [UInt8](data.prefix(maxCSVHeaderBytes))
        var start = 0
        if bytes.count >= 3, bytes[0] == 0xef, bytes[1] == 0xbb, bytes[2] == 0xbf { start = 3 }
        var end = bytes.count
        if let i = bytes[start...].firstIndex(of: 0x0a) { end = i }
        if end > start, bytes[end - 1] == 0x0d { end -= 1 }
        return csvHeaders[String(decoding: bytes[start..<end], as: UTF8.self)]
    }

    static func parseCSV(_ data: Data, _ format: CSVFormat) throws -> [Account] {
        var bytes = [UInt8](data)
        if bytes.count >= 3, bytes[0] == 0xef, bytes[1] == 0xbb, bytes[2] == 0xbf {
            bytes.removeFirst(3)
        }
        let rows: [[String]]
        do { rows = try readCSV(bytes) }
        catch { throw ImporterError.malformed("\(format.name) csv: \(message(error))") }
        var out: [Account] = []
        out.reserveCapacity(rows.count)
        for (i, row) in rows.enumerated() {
            if i == 0 { continue }
            if format.otpCol >= row.count { continue }
            let title = format.titleCol < row.count ? row[format.titleCol] : ""
            let user = format.userCol < row.count ? row[format.userCol] : ""
            let account: Account?
            do { account = try parseOTPValue(row[format.otpCol], title, user) }
            catch { throw ImporterError.malformed("\(format.name) row \(i + 1): \(message(error))") }
            guard let a = account else { continue }
            out.append(a)
        }
        return out
    }

    /// An RFC 4180 reader: quoted fields, doubled quotes, embedded commas and
    /// newlines, `\r\n` line endings. Foundation ships none, and TesseraCore
    /// takes no new dependencies. Blank lines are not records, matching Go's
    /// `encoding/csv`.
    private static func readCSV(_ bytes: [UInt8]) throws -> [[String]] {
        let comma = UInt8(ascii: ","), quote = UInt8(ascii: "\""), cr: UInt8 = 0x0d, lf: UInt8 = 0x0a
        var rows: [[String]] = []
        var i = 0
        let n = bytes.count
        while i < n {
            if bytes[i] == cr || bytes[i] == lf {
                if bytes[i] == cr, i + 1 < n, bytes[i + 1] == lf { i += 2 } else { i += 1 }
                continue
            }
            var row: [String] = []
            var endOfRecord = false
            while !endOfRecord {
                var field: [UInt8] = []
                if i < n, bytes[i] == quote {
                    i += 1
                    var closed = false
                    while i < n {
                        if bytes[i] == quote {
                            if i + 1 < n, bytes[i + 1] == quote {
                                field.append(quote)
                                i += 2
                                continue
                            }
                            i += 1
                            closed = true
                            break
                        }
                        if bytes[i] == cr, i + 1 < n, bytes[i + 1] == lf {
                            field.append(lf)
                            i += 2
                            continue
                        }
                        field.append(bytes[i])
                        i += 1
                    }
                    if !closed || (i < n && bytes[i] != comma && bytes[i] != cr && bytes[i] != lf) {
                        throw ImporterError.malformed("extraneous or missing \" in quoted-field")
                    }
                } else {
                    while i < n, bytes[i] != comma, bytes[i] != cr, bytes[i] != lf {
                        if bytes[i] == quote {
                            throw ImporterError.malformed("bare \" in non-quoted-field")
                        }
                        field.append(bytes[i])
                        i += 1
                    }
                }
                row.append(String(decoding: field, as: UTF8.self))
                if i >= n {
                    endOfRecord = true
                } else if bytes[i] == comma {
                    i += 1
                } else {
                    if bytes[i] == cr, i + 1 < n, bytes[i + 1] == lf { i += 2 } else { i += 1 }
                    endOfRecord = true
                }
            }
            rows.append(row)
        }
        return rows
    }
}
