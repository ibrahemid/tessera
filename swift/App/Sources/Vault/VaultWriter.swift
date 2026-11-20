import Darwin
import Foundation
import os
import TesseraCore

struct VaultWriter: VaultTransaction {
    static let maxTransformRuns = 3

    let location: VaultLocation
    let queue: VaultIOQueue
    private let beforeReplace: (@Sendable () throws -> Void)?

    init(location: VaultLocation, queue: VaultIOQueue = .shared,
         beforeReplace: (@Sendable () throws -> Void)? = nil) {
        self.location = location
        self.queue = queue
        self.beforeReplace = beforeReplace
    }

    private var protectsFiles: Bool { location.access == .container }

    func read() async throws -> VaultSnapshot {
        let url = location.fileURL
        return try await queue.run { try Self.snapshot(at: DurableFile.resolvedTarget(url)) }
    }

    func commit(heldDEK: Data,
                _ transform: @escaping @Sendable (OpenedVault) throws -> VaultCommit) async throws -> CommittedVault {
        let writer = self
        return try await queue.run {
            if writer.location.hasDirectoryAccess {
                return try writer.lockedCommit(heldDEK: heldDEK, transform)
            }
            return try writer.comparingCommit(heldDEK: heldDEK, transform)
        }
    }

    func create(_ commit: VaultCommit) async throws -> CommittedVault {
        let writer = self
        return try await queue.run {
            let target = DurableFile.resolvedTarget(writer.location.fileURL)
            let bytes = try Self.encode(commit.envelope)
            if writer.location.hasDirectoryAccess {
                let lock = try Self.lock(target)
                defer { lock.release() }
                try DurableFile.create(bytes, at: target, hasDirectoryAccess: true, fileProtection: writer.protectsFiles)
            } else {
                try DurableFile.create(bytes, at: target, hasDirectoryAccess: false)
            }
            return Self.committed(commit, bytes: bytes, at: target)
        }
    }

    func restore(verbatim bytes: Data) async throws {
        let writer = self
        try await queue.run {
            let target = DurableFile.resolvedTarget(writer.location.fileURL)
            guard writer.location.hasDirectoryAccess else {
                try Self.refuseLink(target)
                try DurableFile.replace(bytes, at: target, hasDirectoryAccess: false)
                return
            }
            let lock = try Self.lock(target)
            defer { lock.release() }
            try Self.refuseLink(target)
            try DurableFile.replace(bytes, at: target, hasDirectoryAccess: true, fileProtection: writer.protectsFiles)
        }
    }

    func restore(verbatim bytes: Data, ifStill committed: Data) async throws -> RestoreOutcome {
        let writer = self
        return try await queue.run {
            let target = DurableFile.resolvedTarget(writer.location.fileURL)
            guard writer.location.hasDirectoryAccess else {
                return try writer.replace(bytes, at: target, ifStill: committed)
            }
            let lock = try Self.lock(target)
            defer { lock.release() }
            return try writer.replace(bytes, at: target, ifStill: committed)
        }
    }

    func removeCreated(ifStill committed: Data) async throws -> RestoreOutcome {
        let writer = self
        return try await queue.run {
            let target = writer.location.fileURL
            guard writer.location.hasDirectoryAccess else {
                try writer.beforeReplace?()
                guard try Self.holds(committed, at: target) else { return .changedSince }
                do throws(VaultWriteError) {
                    try DurableFile.remove(target)
                } catch {
                    // Without folder access the directory cannot be synced; `create` skips that sync the same way.
                    guard case .io(operation: "flush directory", url: _, errno: let code) = error else { throw error }
                    if code != EPERM, code != EACCES { throw error }
                }
                return .restored
            }
            let lock = try Self.lock(target)
            defer { lock.release() }
            try writer.beforeReplace?()
            guard try Self.holds(committed, at: target) else { return .changedSince }
            try DurableFile.remove(target)
            return .restored
        }
    }

    func readBackup() async throws -> VaultSnapshot? {
        guard location.hasDirectoryAccess else { return nil }
        let url = location.fileURL
        return try await queue.run {
            let backup = VaultSidecar.backupURL(for: DurableFile.resolvedTarget(url))
            do {
                return try Self.snapshot(at: backup)
            } catch VaultWriteError.missingFile {
                return nil
            }
        }
    }

    func deleteBackup() async throws {
        guard location.hasDirectoryAccess else { return }
        let url = location.fileURL
        try await queue.run {
            let target = DurableFile.resolvedTarget(url)
            let lock = try Self.lock(target)
            defer { lock.release() }
            try DurableFile.remove(VaultSidecar.backupURL(for: target))
        }
    }

    private func lockedCommit(heldDEK: Data,
                              _ transform: (OpenedVault) throws -> VaultCommit) throws -> CommittedVault {
        let target = DurableFile.resolvedTarget(location.fileURL)
        let lock = try Self.lock(target)
        defer { lock.release() }
        DurableFile.sweepStaleTemps(in: target.deletingLastPathComponent())
        let current = try Self.snapshot(at: target)
        let commit = try transform(try Self.open(current, dek: heldDEK, at: target))
        let bytes = try Self.encode(commit.envelope)
        if commit.backupPolicy == .copyCurrent {
            try DurableFile.replace(current.bytes, at: VaultSidecar.backupURL(for: target),
                                    hasDirectoryAccess: true, fileProtection: protectsFiles)
        }
        try beforeReplace?()
        try Self.refuseLink(target)
        try DurableFile.replace(bytes, at: target, hasDirectoryAccess: true, fileProtection: protectsFiles)
        return Self.committed(commit, bytes: bytes, at: target)
    }

    private func comparingCommit(heldDEK: Data,
                                 _ transform: (OpenedVault) throws -> VaultCommit) throws -> CommittedVault {
        let target = DurableFile.resolvedTarget(location.fileURL)
        var current = try Self.snapshot(at: target)
        for _ in 0..<Self.maxTransformRuns {
            let commit = try transform(try Self.open(current, dek: heldDEK, at: target))
            let bytes = try Self.encode(commit.envelope)
            try beforeReplace?()
            let fresh = try DurableFile.read(target)
            if fresh == current.bytes {
                try Self.refuseLink(target)
                try DurableFile.replace(bytes, at: target, hasDirectoryAccess: false)
                return Self.committed(commit, bytes: bytes, at: target)
            }
            current = try Self.snapshot(of: fresh, at: target)
        }
        throw VaultWriteError.concurrentChange(target)
    }

    /// Replaces the file with `bytes` after checking, right before the rename,
    /// that it still holds `committed`.
    private func replace(_ bytes: Data, at target: URL, ifStill committed: Data) throws -> RestoreOutcome {
        try Self.refuseLink(target)
        let check = CommittedBytesCheck(target: target, expected: committed, beforeCheck: beforeReplace)
        do {
            try DurableFile.replace(bytes, at: target, hasDirectoryAccess: location.hasDirectoryAccess,
                                    fileProtection: protectsFiles, rename: { from, to in check.rename(from, to) })
        } catch {
            switch check.finding {
            case .success(false)?: return .changedSince
            case .failure(let failure)?: throw failure
            case .success(true)?, nil: throw error
            }
        }
        return .restored
    }

    /// Whether `target` is a regular file holding exactly `expected`. A
    /// missing, oversized, linked or non-regular file holds something else.
    private static func holds(_ expected: Data, at target: URL) throws(VaultWriteError) -> Bool {
        if DurableFile.isSymlink(target) { return false }
        do throws(VaultWriteError) {
            return try DurableFile.read(target) == expected
        } catch .missingFile, .unparseable {
            return false
        } catch .io(operation: _, url: _, errno: EFTYPE) {
            return false
        }
    }

    /// The rename `DurableFile.replace` calls once the new bytes are on disk:
    /// it compares the file first and refuses the rename when it changed.
    private final class CommittedBytesCheck: Sendable {
        private let target: URL
        private let expected: Data
        private let beforeCheck: (@Sendable () throws -> Void)?
        private let found = OSAllocatedUnfairLock<Result<Bool, any Error>?>(initialState: nil)

        init(target: URL, expected: Data, beforeCheck: (@Sendable () throws -> Void)?) {
            self.target = target
            self.expected = expected
            self.beforeCheck = beforeCheck
        }

        var finding: Result<Bool, any Error>? { found.withLock { $0 } }

        func rename(_ from: String, _ to: String) -> Int32 {
            let result = Result<Bool, any Error> {
                try beforeCheck?()
                return try VaultWriter.holds(expected, at: target)
            }
            found.withLock { $0 = result }
            guard case .success(true) = result else { return ECANCELED }
            return DurableFile.systemRename(from, to)
        }
    }

    static func snapshot(at url: URL) throws -> VaultSnapshot {
        try snapshot(of: try DurableFile.read(url), at: url)
    }

    private static func snapshot(of bytes: Data, at url: URL) throws -> VaultSnapshot {
        guard let envelope = try? Envelope.decode(bytes) else { throw VaultWriteError.unparseable(url) }
        return VaultSnapshot(bytes: bytes, envelope: envelope, modifiedAt: DurableFile.modificationDate(of: url))
    }

    private static func open(_ snapshot: VaultSnapshot, dek: Data, at url: URL) throws -> OpenedVault {
        do {
            return OpenedVault(snapshot: snapshot, accounts: try snapshot.envelope.open(dek: dek))
        } catch VaultError.wrongPassphrase {
            throw VaultWriteError.keyChanged
        } catch {
            throw VaultWriteError.unparseable(url)
        }
    }

    private static func encode(_ envelope: Envelope) throws -> Data {
        do {
            return try envelope.encoded()
        } catch EnvelopeError.noWraps {
            throw VaultWriteError.noWraps
        }
    }

    private static func lock(_ target: URL) throws(VaultWriteError) -> VaultFileLock {
        do {
            return try VaultFileLock.acquire(for: target)
        } catch {
            throw .lock(error)
        }
    }

    private static func refuseLink(_ target: URL) throws(VaultWriteError) {
        if DurableFile.isSymlink(target) { throw .missingFile(target) }
    }

    private static func committed(_ commit: VaultCommit, bytes: Data, at url: URL) -> CommittedVault {
        CommittedVault(envelope: commit.envelope, accounts: commit.accounts, dek: commit.dek,
                       bytes: bytes, modifiedAt: DurableFile.modificationDate(of: url))
    }
}
