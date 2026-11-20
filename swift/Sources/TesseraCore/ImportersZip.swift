import Foundation
#if canImport(Compression)
import Compression
#endif

extension Importers {

    /// The cap on export.data, so a zip bomb cannot exhaust memory during an
    /// import. Matches the Go importer.
    static let max1PUXDataBytes = 64 << 20

    /// Reads export.data out of a 1Password 1PUX archive, the way the Go
    /// importer does. Only an entry named exactly export.data counts; names
    /// with a directory part are ignored.
    static func parse1PUX(_ data: [UInt8]) throws -> [Account] {
        let archive: ZipArchive
        do { archive = try ZipArchive(data) }
        catch let e as ZipError { throw ImporterError.malformed(.archive, "1password: open archive: \(e)") }
        let name = Array("export.data".utf8)
        guard let entry = archive.entries.first(where: { $0.name == name }) else {
            throw ImporterError.malformed(.archive, "archive holds no export.data; a 1Password .1pux export does")
        }
        if entry.uncompressedSize > UInt64(max1PUXDataBytes) {
            throw ImporterError.malformed(.tooLarge, "1password: export.data in the archive is larger than 64 MiB")
        }
        let body: Int64
        do { body = try archive.open(entry) }
        catch let e as ZipError { throw ImporterError.malformed(.archive, "1password: open export.data: \(e)") }
        let inner: [UInt8]
        do { inner = try archive.read(entry, at: body) }
        catch let e as ZipError { throw ImporterError.malformed(.archive, "1password: read export.data: \(e)") }
        if inner.count > max1PUXDataBytes {
            throw ImporterError.malformed(.tooLarge, "1password: export.data in the archive is larger than 64 MiB")
        }
        guard let doc = GoJSON(inner) else {
            throw ImporterError.malformed(.invalidJSON, "1password: export.data is not valid JSON")
        }
        return try parse1PUXData(doc)
    }
}

/// A failure inside a zip archive, worded the way Go's `archive/zip`,
/// `compress/flate` and `io` word it.
struct ZipError: Error, Equatable, CustomStringConvertible {
    let description: String

    static let format = ZipError(description: "zip: not a valid zip file")
    static let checksum = ZipError(description: "zip: checksum error")
    static let algorithm = ZipError(description: "zip: unsupported compression algorithm")
    static let corrupt = ZipError(description: "flate: corrupt input")
    static let decoderUnavailable = ZipError(description: "flate: decoder failed to start")
    static let eof = ZipError(description: "EOF")
    static let unexpectedEOF = ZipError(description: "unexpected EOF")
    static let negativeOffset = ZipError(description: "bytes.Reader.ReadAt: negative offset")
}

/// The part of Go's `archive/zip` reader that a `.1pux` import needs, over
/// bytes in memory: the central directory, then one entry's body, stored or
/// deflated, checked against its declared size and CRC-32. Offsets use Go's
/// wrapping `int64` arithmetic so a hostile archive fails the same way in
/// both cores.
struct ZipArchive {
    struct Entry: Equatable {
        let name: [UInt8]
        let flags: UInt16
        let method: UInt16
        let crc32: UInt32
        let compressedSize: UInt64
        let uncompressedSize: UInt64
        let headerOffset: Int64
    }

    static let store: UInt16 = 0
    static let deflate: UInt16 = 8

    private let bytes: [UInt8]
    private(set) var entries: [Entry] = []

    private var size: Int64 { Int64(bytes.count) }

    init(_ bytes: [UInt8]) throws {
        self.bytes = bytes
        let end = try readDirectoryEnd()
        var position = end.baseOffset &+ end.directoryOffset
        var stop = ZipError.format
        while true {
            do {
                let (entry, next) = try readDirectoryHeader(at: position, limit: size)
                entries.append(Entry(name: entry.name, flags: entry.flags, method: entry.method,
                                     crc32: entry.crc32, compressedSize: entry.compressedSize,
                                     uncompressedSize: entry.uncompressedSize,
                                     headerOffset: entry.headerOffset &+ end.baseOffset))
                position = next
            } catch let e as ZipError where e == .format || e == .unexpectedEOF {
                stop = e
                break
            }
        }
        if UInt16(truncatingIfNeeded: entries.count) != UInt16(truncatingIfNeeded: end.records) {
            throw stop
        }
    }

    /// Go `File.Open`: the offset of the entry's body.
    func open(_ entry: Entry) throws -> Int64 {
        let header = try readAt(entry.headerOffset, 30)
        guard Self.le32(header, 0) == 0x0403_4B50 else { throw ZipError.format }
        let bodyOffset = 30 + Int64(Self.le16(header, 26)) + Int64(Self.le16(header, 28))
        switch entry.method {
        case Self.store: break
        case Self.deflate where Self.canInflate: break
        default: throw ZipError.algorithm
        }
        return entry.headerOffset &+ bodyOffset
    }

    /// Go `io.ReadAll` over an opened entry: the body, inflated when deflated,
    /// failing once it outgrows the declared size, then checked against that
    /// size and the CRC-32.
    func read(_ entry: Entry, at start: Int64) throws -> [UInt8] {
        let n = Int64(bitPattern: entry.compressedSize)
        let body = try section(start, n)
        let out: [UInt8]
        if entry.method == Self.deflate {
            out = try Self.inflate(body, maxOutput: entry.uncompressedSize)
        } else {
            if UInt64(body.count) > entry.uncompressedSize { throw ZipError.format }
            out = Array(body)
        }
        if UInt64(out.count) != entry.uncompressedSize { throw ZipError.unexpectedEOF }
        let sum = Self.crc32(out)
        if entry.flags & 0x8 != 0 {
            try checkDataDescriptor(entry, at: start &+ n)
            if sum != entry.crc32 { throw ZipError.checksum }
        } else if entry.crc32 != 0 && sum != entry.crc32 {
            throw ZipError.checksum
        }
        return out
    }

    // MARK: - directory

    private struct DirectoryEnd {
        var records: UInt64
        var directorySize: UInt64
        var directoryOffset: Int64
        var baseOffset: Int64
    }

    private func readDirectoryEnd() throws -> DirectoryEnd {
        let count = bytes.count
        let windowStart = count - min(count, 65 * 1024)
        var found: Int?
        var i = count - 22
        while i >= windowStart {
            if bytes[i] == 0x50 && bytes[i + 1] == 0x4B && bytes[i + 2] == 0x05 && bytes[i + 3] == 0x06 {
                let commentLength = Int(bytes[i + 20]) | Int(bytes[i + 21]) << 8
                if commentLength + 22 + i <= count { found = i }
                break
            }
            i -= 1
        }
        guard let at = found else { throw ZipError.format }
        let eocd = bytes[at ..< at + 22]
        var records = UInt64(Self.le16(eocd, 10))
        var directorySize = UInt64(Self.le32(eocd, 12))
        var directoryOffset = UInt64(Self.le32(eocd, 16))
        var endOffset = Int64(at)

        if records == 0xFFFF || directorySize == 0xFFFF_FFFF || directoryOffset == 0xFFFF_FFFF {
            let p = findDirectory64End(endOffset)
            if p >= 0 {
                endOffset = p
                let b = try readAt(p, 56)
                guard Self.le32(b, 0) == 0x0606_4B50 else { throw ZipError.format }
                records = Self.le64(b, 32)
                directorySize = Self.le64(b, 40)
                directoryOffset = Self.le64(b, 48)
            }
        }
        let maxInt64 = UInt64(Int64.max)
        if directorySize > maxInt64 || directoryOffset > maxInt64 { throw ZipError.format }
        let offset = Int64(directoryOffset)
        var baseOffset = endOffset &- Int64(directorySize) &- offset
        let start = baseOffset &+ offset
        if start < 0 || start >= size { throw ZipError.format }
        if baseOffset > 0, (try? readDirectoryHeader(at: offset, limit: Self.sectionLimit(offset, size - offset))) != nil {
            baseOffset = 0
        }
        return DirectoryEnd(records: records, directorySize: directorySize,
                            directoryOffset: offset, baseOffset: baseOffset)
    }

    private func findDirectory64End(_ endOffset: Int64) -> Int64 {
        let at = endOffset - 20
        if at < 0 { return -1 }
        let b = bytes[Int(at) ..< Int(at) + 20]
        guard Self.le32(b, 0) == 0x0706_4B50, Self.le32(b, 4) == 0, Self.le32(b, 16) == 1 else { return -1 }
        return Int64(bitPattern: Self.le64(b, 8))
    }

    private func readDirectoryHeader(at position: Int64, limit: Int64) throws -> (Entry, Int64) {
        let h = try readFull(position, 46, limit: limit)
        guard Self.le32(h, 0) == 0x0201_4B50 else { throw ZipError.format }
        let compressed32 = Self.le32(h, 20)
        let uncompressed32 = Self.le32(h, 24)
        let nameLength = Int(Self.le16(h, 28))
        let extraLength = Int(Self.le16(h, 30))
        let commentLength = Int(Self.le16(h, 32))
        let rest = try readFull(position + 46, nameLength + extraLength + commentLength, limit: limit)
        let name = Array(rest.prefix(nameLength))
        var extra = rest.dropFirst(nameLength).prefix(extraLength)

        var compressedSize = UInt64(compressed32)
        var uncompressedSize = UInt64(uncompressed32)
        var headerOffset = Int64(Self.le32(h, 42))
        while extra.count >= 4 {
            let tag = Self.le16(extra, 0)
            let fieldSize = Int(Self.le16(extra, 2))
            extra = extra.dropFirst(4)
            if extra.count < fieldSize { break }
            var field = extra.prefix(fieldSize)
            extra = extra.dropFirst(fieldSize)
            guard tag == 0x0001 else { continue }
            if uncompressed32 == 0xFFFF_FFFF {
                guard field.count >= 8 else { throw ZipError.format }
                uncompressedSize = Self.le64(field, 0)
                field = field.dropFirst(8)
            }
            if compressed32 == 0xFFFF_FFFF {
                guard field.count >= 8 else { throw ZipError.format }
                compressedSize = Self.le64(field, 0)
                field = field.dropFirst(8)
            }
            if headerOffset == 0xFFFF_FFFF {
                guard field.count >= 8 else { throw ZipError.format }
                headerOffset = Int64(bitPattern: Self.le64(field, 0))
            }
        }
        let entry = Entry(name: name, flags: Self.le16(h, 8), method: Self.le16(h, 10),
                          crc32: Self.le32(h, 16), compressedSize: compressedSize,
                          uncompressedSize: uncompressedSize, headerOffset: headerOffset)
        return (entry, position + 46 + Int64(rest.count))
    }

    private func checkDataDescriptor(_ entry: Entry, at offset: Int64) throws {
        let limit = Self.sectionLimit(offset, 16)
        do {
            let first = try readFull(offset, 4, limit: limit)
            let crc: UInt32
            if Self.le32(first, 0) == 0x0807_4B50 {
                crc = Self.le32(try readFull(offset &+ 4, 12, limit: limit), 0)
            } else {
                _ = try readFull(offset &+ 4, 8, limit: limit)
                crc = Self.le32(first, 0)
            }
            if crc != entry.crc32 { throw ZipError.checksum }
        } catch let e as ZipError where e == .eof {
            throw ZipError.unexpectedEOF
        }
    }

    // MARK: - reads with Go's io semantics

    /// Go `bytes.Reader.ReadAt`: any shortfall is `EOF`.
    private func readAt(_ offset: Int64, _ count: Int) throws -> ArraySlice<UInt8> {
        if offset < 0 { throw ZipError.negativeOffset }
        if offset >= size || Int64(count) > size - offset { throw ZipError.eof }
        return bytes[Int(offset) ..< Int(offset) + count]
    }

    /// Go `io.ReadFull` from an `io.SectionReader` positioned at `offset`
    /// whose section ends at `limit`.
    private func readFull(_ offset: Int64, _ count: Int, limit: Int64) throws -> ArraySlice<UInt8> {
        if count == 0 { return [] }
        if offset >= limit { throw ZipError.eof }
        if offset < 0 { throw ZipError.negativeOffset }
        let end = min(limit, size)
        if offset >= end { throw ZipError.eof }
        if end - offset < Int64(count) { throw ZipError.unexpectedEOF }
        return bytes[Int(offset) ..< Int(offset) + count]
    }

    /// Everything an `io.SectionReader` of `n` bytes at `offset` yields.
    private func section(_ offset: Int64, _ n: Int64) throws -> ArraySlice<UInt8> {
        let limit = Self.sectionLimit(offset, n)
        if offset >= limit { return [] }
        if offset < 0 { throw ZipError.negativeOffset }
        let end = min(limit, size)
        if offset >= end { return [] }
        return bytes[Int(offset) ..< Int(end)]
    }

    /// Go `io.NewSectionReader`'s end offset, saturating on overflow.
    private static func sectionLimit(_ offset: Int64, _ n: Int64) -> Int64 {
        offset <= Int64.max &- n ? offset &+ n : Int64.max
    }

    private static func le16(_ b: ArraySlice<UInt8>, _ at: Int) -> UInt16 {
        let i = b.startIndex + at
        return UInt16(b[i]) | UInt16(b[i + 1]) << 8
    }

    private static func le32(_ b: ArraySlice<UInt8>, _ at: Int) -> UInt32 {
        let i = b.startIndex + at
        return UInt32(b[i]) | UInt32(b[i + 1]) << 8 | UInt32(b[i + 2]) << 16 | UInt32(b[i + 3]) << 24
    }

    private static func le64(_ b: ArraySlice<UInt8>, _ at: Int) -> UInt64 {
        UInt64(le32(b, at)) | UInt64(le32(b, at + 4)) << 32
    }

    // MARK: - CRC-32 (IEEE)

    private static let crcTable: [UInt32] = (0 ..< 256).map { n in
        var c = UInt32(n)
        for _ in 0 ..< 8 { c = c & 1 != 0 ? 0xEDB8_8320 ^ (c >> 1) : c >> 1 }
        return c
    }

    static func crc32(_ data: [UInt8]) -> UInt32 {
        var c: UInt32 = 0xFFFF_FFFF
        crcTable.withUnsafeBufferPointer { table in
            for byte in data { c = table[Int((c ^ UInt32(byte)) & 0xFF)] ^ (c >> 8) }
        }
        return c ^ 0xFFFF_FFFF
    }

    // MARK: - DEFLATE

    #if canImport(Compression)
    static let canInflate = true

    /// Raw DEFLATE through the Compression framework, failing with
    /// `ZipError.format` as soon as the output passes `maxOutput`.
    static func inflate(_ input: ArraySlice<UInt8>, maxOutput: UInt64) throws -> [UInt8] {
        if input.isEmpty { throw ZipError.unexpectedEOF }
        let stream = UnsafeMutablePointer<compression_stream>.allocate(capacity: 1)
        defer { stream.deallocate() }
        guard compression_stream_init(stream, COMPRESSION_STREAM_DECODE, COMPRESSION_ZLIB) == COMPRESSION_STATUS_OK else {
            throw ZipError.decoderUnavailable
        }
        defer { compression_stream_destroy(stream) }
        let chunk = 64 << 10
        let destination = UnsafeMutablePointer<UInt8>.allocate(capacity: chunk)
        defer { destination.deallocate() }
        var out: [UInt8] = []
        try input.withUnsafeBufferPointer { source in
            guard let base = source.baseAddress else { throw ZipError.unexpectedEOF }
            stream.pointee.src_ptr = base
            stream.pointee.src_size = source.count
            while true {
                stream.pointee.dst_ptr = destination
                stream.pointee.dst_size = chunk
                let pending = stream.pointee.src_size
                let status = compression_stream_process(stream, Int32(COMPRESSION_STREAM_FINALIZE.rawValue))
                let produced = chunk - stream.pointee.dst_size
                out.append(contentsOf: UnsafeBufferPointer(start: destination, count: produced))
                if UInt64(out.count) > maxOutput { throw ZipError.format }
                switch status {
                case COMPRESSION_STATUS_END:
                    return
                case COMPRESSION_STATUS_OK:
                    if produced == 0 && stream.pointee.src_size == pending {
                        throw pending == 0 ? ZipError.unexpectedEOF : ZipError.corrupt
                    }
                default:
                    throw ZipError.corrupt
                }
            }
        }
        return out
    }
    #else
    static let canInflate = false

    static func inflate(_ input: ArraySlice<UInt8>, maxOutput: UInt64) throws -> [UInt8] {
        throw ZipError.algorithm
    }
    #endif
}
