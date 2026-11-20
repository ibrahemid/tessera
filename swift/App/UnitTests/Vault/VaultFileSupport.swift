import Darwin
import Foundation
import os
import XCTest
import TesseraCore
@testable import Tessera

enum VaultFiles {
    static func realPath(_ url: URL) -> URL {
        guard let real = realpath(url.path, nil) else { return url }
        defer { free(real) }
        return URL(fileURLWithPath: String(cString: real))
    }

    static func mode(of url: URL) -> UInt16? {
        var info = stat()
        guard lstat(url.path, &info) == 0 else { return nil }
        return UInt16(info.st_mode & 0o777)
    }

    static func exists(_ url: URL) -> Bool {
        var info = stat()
        return lstat(url.path, &info) == 0
    }

    static func write(_ data: Data, to url: URL, mode: mode_t) throws {
        try data.write(to: url)
        guard chmod(url.path, mode) == 0 else { throw POSIXError(POSIXErrorCode(rawValue: errno) ?? .EIO) }
    }

    static func symlink(_ link: URL, to target: URL) throws {
        try FileManager.default.createSymbolicLink(at: link, withDestinationURL: target)
    }

    static func tempNames(in dir: URL) -> [String] {
        ((try? FileManager.default.contentsOfDirectory(atPath: dir.path)) ?? []).filter(DurableFile.isTempName)
    }

    static func makeEnvelope(accounts: [Account] = []) throws -> (Envelope, Data) {
        var (envelope, dek) = try Envelope.createUnwrapped(accounts: accounts)
        try envelope.addSecureEnclaveWrap(VaultWrap.secureEnclave(keyID: Data(repeating: 1, count: 16),
                                                                  nonce: Data(repeating: 2, count: XChaCha.nonceSize),
                                                                  ct: Data(repeating: 3, count: 48)))
        return (envelope, dek)
    }

    static func appending(_ account: Account, dek: Data, policy: BackupPolicy = .copyCurrent,
                          runs: CallCounter? = nil) -> @Sendable (OpenedVault) throws -> VaultCommit {
        { opened in
            runs?.increment()
            let next = opened.accounts + [account]
            return VaultCommit(envelope: try opened.snapshot.envelope.reseal(accounts: next, dek: dek),
                               accounts: next, dek: dek, backupPolicy: policy)
        }
    }
}

final class CallCounter: Sendable {
    private let state = OSAllocatedUnfairLock(initialState: 0)

    @discardableResult
    func increment() -> Int { state.withLock { $0 += 1; return $0 } }

    var value: Int { state.withLock { $0 } }
}

extension XCTestCase {
    func makeScratchDirectory() throws -> URL {
        let dir = FileManager.default.temporaryDirectory
            .appendingPathComponent("vault-tests-\(UUID().uuidString)", isDirectory: true)
        try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        addTeardownBlock { try? FileManager.default.removeItem(at: dir) }
        return VaultFiles.realPath(dir)
    }
}
