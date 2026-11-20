import CryptoKit
import Foundation
import os
import TesseraCore
import XCTest
@testable import Tessera

/// A fast stand-in for argon2id: HKDF-SHA256 over the passphrase and salt.
struct TestArgon2: Argon2idProvider {
    func deriveKey(passphrase: Data, salt: Data, memoryKiB: UInt32, iterations: UInt32,
                   parallelism: UInt8, keyLength: Int) throws -> Data {
        HKDF<SHA256>.deriveKey(inputKeyMaterial: SymmetricKey(data: passphrase), salt: salt,
                               info: Data("tessera.test.argon2".utf8), outputByteCount: keyLength)
            .withUnsafeBytes { Data($0) }
    }
}

/// An in-memory key item store whose operations can be made to fail or to
/// return other bytes than were written.
final class FaultyKeyItemStore: KeyItemStore, Sendable {
    struct Faults: Sendable {
        var add: KeyItemError?
        var read: KeyItemError?
        var delete: KeyItemError?
        var readData: Data?
        var readPendingFlipped = false
    }

    let base = InMemoryKeyItemStore()
    private let faults = OSAllocatedUnfairLock(initialState: Faults())

    func setFaults(_ change: (inout Faults) -> Void) {
        faults.withLockUnchecked { change(&$0) }
    }

    func add(_ data: Data, account: String, isRotationPending: Bool) throws(KeyItemError) {
        if let error = faults.withLock({ $0.add }) { throw error }
        try base.add(data, account: account, isRotationPending: isRotationPending)
    }

    func read(account: String) throws(KeyItemError) -> KeyItem {
        let current = faults.withLock { $0 }
        if let error = current.read { throw error }
        let item = try base.read(account: account)
        return KeyItem(data: current.readData ?? item.data,
                       isRotationPending: current.readPendingFlipped ? !item.isRotationPending : item.isRotationPending)
    }

    func delete(account: String) throws(KeyItemError) {
        if let error = faults.withLock({ $0.delete }) { throw error }
        try base.delete(account: account)
    }

    var accounts: [String] { base.accounts }
}

/// Records each step and throws `SimulatedCrash` at `crashAt`; `onStep` runs before the throw.
final class CrashingCheckpoint: MigrationCheckpoint, Sendable {
    let crashAt: MigrationStep?
    private let onStep: @Sendable (MigrationStep) -> Void
    private let seen = OSAllocatedUnfairLock(initialState: [MigrationStep]())

    init(crashAt: MigrationStep? = nil, onStep: @escaping @Sendable (MigrationStep) -> Void = { _ in }) {
        self.crashAt = crashAt
        self.onStep = onStep
    }

    var steps: [MigrationStep] { seen.withLock { $0 } }

    func reached(_ step: MigrationStep) throws {
        seen.withLock { $0.append(step) }
        onStep(step)
        if step == crashAt { throw SimulatedCrash() }
    }

    func isInterruption(_ error: any Error) -> Bool { error is SimulatedCrash }
}

enum KeyFixture {
    static let thisMac = Data(repeating: 0x41, count: 16)
    static let otherMac = Data(repeating: 0x42, count: 16)

    /// A wrap in the form 1.1.0 wrote: the key's data representation in
    /// `se_key`, no `kdf`, the DEK sealed under the legacy derivation.
    static func legacyWrap(dek: Data, device: Data = thisMac, requireBiometrics: Bool = false) throws -> VaultWrap {
        let key = try SoftwareEnclaveKeyFactory(deviceTag: device).makeKey(requireBiometrics: requireBiometrics,
                                                                            context: nil)
        let wrapKey = try SEWrapCodec.deriveWrapKey(key: key, version: .legacy)
        let nonce = SecureRandom.bytes(XChaCha.nonceSize)
        let ct = try XChaCha.seal(dek, key: wrapKey, nonce: nonce)
        return VaultWrap(type: "secure-enclave", kdf: nil, params: nil, salt: nil,
                         se_key: key.dataRepresentation.base64EncodedString(),
                         nonce: nonce.base64EncodedString(), ct: ct.base64EncodedString())
    }

    /// A current-form wrap naming a key item that only another Mac holds.
    static func foreignCurrentWrap() throws -> VaultWrap {
        try VaultWrap.secureEnclave(keyID: SecureRandom.bytes(SEKeyID.byteCount),
                                    nonce: SecureRandom.bytes(XChaCha.nonceSize), ct: SecureRandom.bytes(48))
    }

    static func unknownWrap() -> VaultWrap {
        VaultWrap(type: "hardware-key", kdf: nil, params: nil, salt: nil, se_key: nil,
                  nonce: SecureRandom.bytes(XChaCha.nonceSize).base64EncodedString(),
                  ct: SecureRandom.bytes(48).base64EncodedString())
    }

    static let accounts = [
        VaultFixture.account("g", issuer: "GitHub", account: "me@example.com"),
        VaultFixture.account("a", issuer: "AWS", account: "root"),
    ]

    /// `accounts` in the order a vault payload stores them, by id.
    static let storedAccounts = accounts.sorted { $0.id < $1.id }
}

extension XCTestCase {
    /// A UserDefaults suite of its own, removed when the test ends.
    func makeThrowawayDefaults() throws -> UserDefaults {
        let name = "tessera.tests.\(UUID().uuidString)"
        let defaults = try XCTUnwrap(UserDefaults(suiteName: name))
        addTeardownBlock { UserDefaults(suiteName: name)?.removePersistentDomain(forName: name) }
        return defaults
    }

    /// A new directory under the temporary directory, removed when the test ends.
    func makeTemporaryDirectory(_ prefix: String) throws -> URL {
        let dir = FileManager.default.temporaryDirectory.appendingPathComponent("\(prefix)-\(UUID().uuidString)")
        try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        addTeardownBlock { try? FileManager.default.removeItem(at: dir) }
        return dir
    }
}
