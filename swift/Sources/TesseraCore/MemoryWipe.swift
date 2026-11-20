import Foundation
#if canImport(Darwin)
import Darwin
#endif

/// Overwrites key material with zeros through `memset_s`, which the compiler
/// may not remove as a dead store.
enum MemoryWipe {
    static func zero<T>(_ array: inout [T]) {
        array.withUnsafeMutableBytes { zero($0) }
    }

    static func zero(_ data: inout Data) {
        guard !data.isEmpty else { return }
        data.withUnsafeMutableBytes { zero($0) }
    }

    private static func zero(_ buf: UnsafeMutableRawBufferPointer) {
        guard let base = buf.baseAddress, buf.count > 0 else { return }
        #if canImport(Darwin)
        _ = memset_s(base, buf.count, 0, buf.count)
        #else
        buf.initializeMemory(as: UInt8.self, repeating: 0)
        #endif
    }
}
