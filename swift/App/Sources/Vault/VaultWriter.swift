import Foundation
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
