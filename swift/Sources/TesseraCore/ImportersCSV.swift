import Foundation

extension Importers {

    /// Describes one registered CSV export: its source name and the column
    /// indexes the shared OTP-value rule needs.
    struct CSVFormat {
        let name: String
        let titleCol: Int
        let userCol: Int
        let otpCol: Int
        let noteCol: Int
    }

    /// Maps an exact header line to its format. Apple publishes no schema for
    /// its CSV, so the header comes from real exports: if Apple changes it,
    /// detection stops matching and the file is reported as unrecognized rather
    /// than parsed against the wrong columns.
    static let csvHeaders: [(header: [UInt8], format: CSVFormat)] = [
        (Array("Title,URL,Username,Password,Notes,OTPAuth".utf8),
         CSVFormat(name: "Apple Passwords", titleCol: 0, userCol: 2, otpCol: 5, noteCol: 4)),
        (Array("Title,Url,Username,Password,OTPAuth,Favorite,Archived,Tags,Notes".utf8),
         CSVFormat(name: "1Password", titleCol: 0, userCol: 2, otpCol: 4, noteCol: 8)),
    ]

    private static let utf8BOM: [UInt8] = [0xEF, 0xBB, 0xBF]

    private static func trimBOM<D: Collection>(_ data: D) -> ArraySlice<UInt8> where D.Element == UInt8 {
        let bytes = ArraySlice(data)
        return bytes.starts(with: utf8BOM) ? bytes.dropFirst(3) : bytes
    }

    /// Compares the first line of data, byte for byte, against the registered
    /// export headers, after stripping a UTF-8 BOM and a trailing `\r`.
    static func matchCSVHeader<D: Collection>(_ data: D) -> CSVFormat? where D.Element == UInt8 {
        var line = trimBOM(data)
        if let i = line.firstIndex(of: 0x0A) { line = line[..<i] }
        if line.last == 0x0D { line = line.dropLast() }
        return csvHeaders.first { $0.header.elementsEqual(line) }?.format
    }

    static func parseCSV<D: Collection>(_ data: D, _ format: CSVFormat) throws -> [Account] where D.Element == UInt8 {
        var reader = GoCSVReader(Array(trimBOM(data)))
        let rows: [[String]]
        do { rows = try reader.readAll() }
        catch { throw ImporterError.malformed(.invalidCSV, "\(format.name) csv: \(message(error))") }
        var out: [Account] = []
        out.reserveCapacity(rows.count)
        for (i, row) in rows.enumerated() {
            if i == 0 || format.otpCol >= row.count { continue }
            let title = format.titleCol < row.count ? row[format.titleCol] : ""
            let user = format.userCol < row.count ? row[format.userCol] : ""
            let parsed: Account?
            do { parsed = try parseOTPValue(row[format.otpCol], title, user) }
            catch { throw entryError("\(format.name) row \(i + 1): ", error) }
            guard var a = parsed else { continue }
            if format.noteCol < row.count { a.note = TextClean.note(row[format.noteCol]) }
            out.append(a)
        }
        return out
    }
}

/// Go `encoding/csv.Reader` with its default settings (comma delimiter, no
/// comment character, strict quotes, a variable number of fields), reading
/// an in-memory buffer. Records, positions and error text are Go's.
struct GoCSVReader {
    struct ParseError: Error, CustomStringConvertible {
        let startLine: Int
        let line: Int
        let column: Int
        let reason: String

        static let bareQuote = "bare \" in non-quoted-field"
        static let quote = "extraneous or missing \" in quoted-field"

        var description: String {
            if startLine != line {
                return "record on line \(startLine); parse error on line \(line), column \(column): \(reason)"
            }
            return "parse error on line \(line), column \(column): \(reason)"
        }
    }

    private let data: [UInt8]
    private var offset = 0
    private var numLine = 0

    init(_ data: [UInt8]) {
        self.data = data
    }

    private static func lengthNL(_ b: ArraySlice<UInt8>) -> Int {
        b.last == 0x0A ? 1 : 0
    }

    /// One line through its `\n`, or the rest of the input. `eof` is set only
    /// when nothing was left to read.
    private mutating func readLine() -> (line: ArraySlice<UInt8>, eof: Bool) {
        numLine += 1
        guard offset < data.count else { return ([], true) }
        var line: [UInt8]
        if let nl = data[offset...].firstIndex(of: 0x0A) {
            line = Array(data[offset...nl])
            offset = nl + 1
        } else {
            line = Array(data[offset...])
            offset = data.count
            if line.last == 0x0D { line.removeLast() }
        }
        let n = line.count
        if n >= 2, line[n - 2] == 0x0D, line[n - 1] == 0x0A {
            line[n - 2] = 0x0A
            line.removeLast()
        }
        return (line[...], false)
    }

    /// The next record's fields as raw bytes, or nil at the end of input.
    private mutating func readRecord() throws -> [[UInt8]]? {
        var line: ArraySlice<UInt8> = []
        var eof = false
        while true {
            (line, eof) = readLine()
            if !eof && line.count == Self.lengthNL(line) { continue }
            break
        }
        if eof { return nil }

        let recLine = numLine
        var fields: [[UInt8]] = []
        var field: [UInt8] = []
        var posLine = numLine
        var posCol = 1
        parseField: while true {
            if line.isEmpty || line.first != 0x22 {
                let comma = line.firstIndex(of: 0x2C)
                let value = comma.map { line[..<$0] } ?? line.dropLast(Self.lengthNL(line))
                if let j = value.firstIndex(of: 0x22) {
                    throw ParseError(startLine: recLine, line: numLine,
                                     column: posCol + (j - value.startIndex), reason: ParseError.bareQuote)
                }
                fields.append(Array(value))
                guard let i = comma else { break parseField }
                posCol += i - line.startIndex + 1
                line = line[(i + 1)...]
                continue parseField
            }
            line = line.dropFirst()
            posCol += 1
            field = []
            while true {
                if let i = line.firstIndex(of: 0x22) {
                    field.append(contentsOf: line[..<i])
                    posCol += i - line.startIndex + 1
                    line = line[(i + 1)...]
                    if line.first == 0x22 {
                        field.append(0x22)
                        line = line.dropFirst()
                        posCol += 1
                    } else if line.first == 0x2C {
                        line = line.dropFirst()
                        posCol += 1
                        fields.append(field)
                        continue parseField
                    } else if Self.lengthNL(line) == line.count {
                        fields.append(field)
                        break parseField
                    } else {
                        throw ParseError(startLine: recLine, line: numLine, column: posCol - 1,
                                         reason: ParseError.quote)
                    }
                } else if !line.isEmpty {
                    field.append(contentsOf: line)
                    posCol += line.count
                    (line, _) = readLine()
                    if !line.isEmpty {
                        posLine += 1
                        posCol = 1
                    }
                } else {
                    throw ParseError(startLine: recLine, line: posLine, column: posCol, reason: ParseError.quote)
                }
            }
        }
        return fields
    }

    /// Go `Reader.ReadAll`: every record, or the first parse error.
    mutating func readAll() throws -> [[String]] {
        var records: [[String]] = []
        while let record = try readRecord() {
            records.append(record.map { GoText.string($0) })
        }
        return records
    }
}
