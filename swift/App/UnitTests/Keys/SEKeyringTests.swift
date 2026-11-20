import XCTest
import TesseraCore
@testable import Tessera

final class SEKeyringTests: XCTestCase {
    private let vaultPath = "/private/tmp/tessera-keyring-tests/vault.json"
    private var items: FaultyKeyItemStore!
    private var registry: SEKeyRegistry!
    private var keyring: SEKeyring!

    override func setUpWithError() throws {
        items = FaultyKeyItemStore()
        registry = SEKeyRegistry(defaults: try makeThrowawayDefaults())
        keyring = makeKeyring(enclave: SoftwareEnclaveKeyFactory(deviceTag: KeyFixture.thisMac))
    }

    private func makeKeyring(enclave: SoftwareEnclaveKeyFactory) -> SEKeyring {
        SEKeyring(items: items, enclave: enclave, registry: registry)
    }

    private func envelope(_ wraps: [VaultWrap], dek: Data) throws -> Envelope {
        var envelope = try Envelope.createUnwrapped(accounts: KeyFixture.accounts).0
            .reseal(accounts: KeyFixture.accounts, dek: dek)
        for wrap in wraps { try envelope.addSecureEnclaveWrap(wrap) }
        return envelope
    }

    private func provisionWrap(dek: Data, pending: Bool = false) throws -> (ProvisionedSEKey, VaultWrap) {
        let key = try keyring.createKey(for: vaultPath, requireBiometrics: false, isRotationPending: pending,
                                        context: nil)
        return (key, try keyring.makeWrap(dek: dek, key: key))
    }

    // MARK: - createKey

    func testCreateKeyRegistersWritesAndRestores() throws {
        var stages: [KeyProvisionStage] = []
        let id = SEKeyID.random()
        let key = try keyring.createKey(id: id, for: vaultPath, requireBiometrics: true, isRotationPending: true,
                                        context: nil) { stages.append($0) }
        XCTAssertEqual(stages, [.registered, .itemWritten, .readBack])
        XCTAssertEqual(key.id, id)
        XCTAssertTrue(key.isRotationPending)
        XCTAssertEqual(registry.ids(for: vaultPath), [id])
        let item = try items.read(account: id.account)
        XCTAssertEqual(item.data, key.key.dataRepresentation)
        XCTAssertTrue(item.isRotationPending)
        XCTAssertTrue(item.requiresBiometrics)
        XCTAssertTrue(key.requiresBiometrics)
    }

    func testReadBackWithAnotherBiometricSettingIsAMismatch() throws {
        items.setFaults { $0.readBiometricsFlipped = true }
        let id = SEKeyID.random()
        XCTAssertThrowsError(try keyring.createKey(id: id, for: vaultPath, requireBiometrics: true,
                                                   isRotationPending: false, context: nil)) {
            XCTAssertEqual($0 as? SEKeyError, .readBackMismatch(id))
        }
        XCTAssertEqual(items.accounts, [])
        XCTAssertEqual(registry.ids(for: vaultPath), [])
    }

    func testReadBackMismatchRemovesWhatWasAdded() throws {
        items.setFaults { $0.readData = Data("other bytes".utf8) }
        let id = SEKeyID.random()
        XCTAssertThrowsError(try keyring.createKey(id: id, for: vaultPath, requireBiometrics: false,
                                                   isRotationPending: false, context: nil)) {
            XCTAssertEqual($0 as? SEKeyError, .readBackMismatch(id))
        }
        XCTAssertEqual(items.accounts, [])
        XCTAssertEqual(registry.ids(for: vaultPath), [])
    }

    func testReadBackWithoutTheMarkerIsAMismatch() throws {
        items.setFaults { $0.readPendingFlipped = true }
        let id = SEKeyID.random()
        XCTAssertThrowsError(try keyring.createKey(id: id, for: vaultPath, requireBiometrics: false,
                                                   isRotationPending: true, context: nil)) {
            XCTAssertEqual($0 as? SEKeyError, .readBackMismatch(id))
        }
        XCTAssertEqual(items.accounts, [])
    }

    func testItemMissingOnReadBack() throws {
        items.setFaults { $0.read = .notFound }
        let id = SEKeyID.random()
        XCTAssertThrowsError(try keyring.createKey(id: id, for: vaultPath, requireBiometrics: false,
                                                   isRotationPending: false, context: nil)) {
            XCTAssertEqual($0 as? SEKeyError, .keyItemMissing(id))
        }
        XCTAssertEqual(items.accounts, [])
        XCTAssertEqual(registry.ids(for: vaultPath), [])
    }

    func testDuplicateLeavesTheExistingItem() throws {
        let id = SEKeyID.random()
        try items.add(KeyItem(data: Data("existing".utf8), isRotationPending: false, requiresBiometrics: false),
                      account: id.account)
        XCTAssertThrowsError(try keyring.createKey(id: id, for: vaultPath, requireBiometrics: false,
                                                   isRotationPending: false, context: nil)) {
            XCTAssertEqual($0 as? KeyItemError, .duplicate)
        }
        XCTAssertEqual(try items.read(account: id.account).data, Data("existing".utf8))
        XCTAssertEqual(registry.ids(for: vaultPath), [])
    }

    func testAddFailureUnregisters() throws {
        items.setFaults { $0.add = .missingEntitlement }
        XCTAssertThrowsError(try keyring.createKey(for: vaultPath, requireBiometrics: false, isRotationPending: false,
                                                   context: nil)) {
            XCTAssertEqual($0 as? KeyItemError, .missingEntitlement)
        }
        XCTAssertEqual(registry.ids(for: vaultPath), [])
    }

    func testFailedCleanupKeepsTheRegistration() throws {
        items.setFaults {
            $0.readData = Data("other bytes".utf8)
            $0.delete = .interactionNotAllowed
        }
        let id = SEKeyID.random()
        XCTAssertThrowsError(try keyring.createKey(id: id, for: vaultPath, requireBiometrics: false,
                                                   isRotationPending: false, context: nil))
        XCTAssertEqual(items.accounts, [id.account])
        XCTAssertEqual(registry.ids(for: vaultPath), [id])
    }

    func testUnavailableEnclaveAddsNothing() throws {
        keyring = makeKeyring(enclave: SoftwareEnclaveKeyFactory(deviceTag: KeyFixture.thisMac, isAvailable: false))
        XCTAssertFalse(keyring.isAvailable)
        XCTAssertThrowsError(try keyring.createKey(for: vaultPath, requireBiometrics: false, isRotationPending: false,
                                                   context: nil)) {
            XCTAssertEqual($0 as? SEKeyError, .notAvailable)
        }
        XCTAssertEqual(items.accounts, [])
        XCTAssertEqual(registry.ids(for: vaultPath), [])
    }

    func testProgressErrorLeavesEverythingInPlace() throws {
        let id = SEKeyID.random()
        XCTAssertThrowsError(try keyring.createKey(id: id, for: vaultPath, requireBiometrics: false,
                                                   isRotationPending: false, context: nil) { stage in
            if stage == .itemWritten { throw SimulatedCrash() }
        }) { XCTAssertEqual($0 as? SimulatedCrash, SimulatedCrash()) }
        XCTAssertEqual(items.accounts, [id.account])
        XCTAssertEqual(registry.ids(for: vaultPath), [id])
    }

    // MARK: - Wraps

    func testMakeWrapOpensWithTheRestoredKey() throws {
        let dek = SecureRandom.bytes(32)
        let (key, wrap) = try provisionWrap(dek: dek)
        XCTAssertEqual(wrap.secureEnclaveKeyID, key.id)
        let restored = try SoftwareEnclaveKeyFactory(deviceTag: KeyFixture.thisMac)
            .restoreKey(dataRepresentation: try items.read(account: key.id.account).data, context: nil)
        XCTAssertEqual(try SEWrapCodec.open(wrap: wrap, with: restored), dek)
    }

    func testUnwrapTriesCurrentWrapsBeforeLegacy() throws {
        let dek = SecureRandom.bytes(32)
        let legacy = try KeyFixture.legacyWrap(dek: dek)
        let (key, current) = try provisionWrap(dek: dek, pending: true)
        let result = try keyring.unwrap(try envelope([legacy, current], dek: dek), context: nil)
        guard case let .current(opened, id, isPending, requiresBiometrics, wrap) = result else {
            return XCTFail("\(result)")
        }
        XCTAssertEqual(opened, dek)
        XCTAssertEqual(id, key.id)
        XCTAssertTrue(isPending)
        XCTAssertFalse(requiresBiometrics)
        XCTAssertTrue(wrap.isSameWrap(as: current))
    }

    func testUnwrapSkipsForeignIDs() throws {
        let dek = SecureRandom.bytes(32)
        let foreign = try KeyFixture.foreignCurrentWrap()
        let (key, current) = try provisionWrap(dek: dek)
        let result = try keyring.unwrap(try envelope([foreign, current], dek: dek), context: nil)
        guard case let .current(_, id, _, _, _) = result else { return XCTFail("\(result)") }
        XCTAssertEqual(id, key.id)
    }

    func testUnwrapReportsTheBiometricSettingOfTheItem() throws {
        let dek = SecureRandom.bytes(32)
        let key = try keyring.createKey(for: vaultPath, requireBiometrics: true, isRotationPending: false, context: nil)
        let wrap = try keyring.makeWrap(dek: dek, key: key)
        let result = try keyring.unwrap(try envelope([wrap], dek: dek), context: nil)
        guard case let .current(_, id, _, requiresBiometrics, _) = result else { return XCTFail("\(result)") }
        XCTAssertEqual(id, key.id)
        XCTAssertTrue(requiresBiometrics)
        XCTAssertEqual(try keyring.localKeys(in: try envelope([wrap], dek: dek)).map(\.requiresBiometrics), [true])
    }

    func testUnwrapFallsBackToALegacyWrapFromThisMac() throws {
        let dek = SecureRandom.bytes(32)
        let other = try KeyFixture.legacyWrap(dek: dek, device: KeyFixture.otherMac)
        let local = try KeyFixture.legacyWrap(dek: dek)
        let env = try envelope([try KeyFixture.foreignCurrentWrap(), other, local], dek: dek)
        let result = try keyring.unwrap(env, context: nil)
        guard case let .legacy(opened, wrap) = result else { return XCTFail("\(result)") }
        XCTAssertEqual(opened, dek)
        XCTAssertTrue(wrap.isSameWrap(as: local))
    }

    func testUnwrapWithNothingOfThisMacIsNoWrap() throws {
        let dek = SecureRandom.bytes(32)
        let env = try envelope([try KeyFixture.foreignCurrentWrap(),
                                try KeyFixture.legacyWrap(dek: dek, device: KeyFixture.otherMac)], dek: dek)
        XCTAssertThrowsError(try keyring.unwrap(env, context: nil)) {
            XCTAssertEqual($0 as? SEKeyError, .noWrap)
        }
    }

    func testUnwrapPassesOnAReadFailureOtherThanNotFound() throws {
        let dek = SecureRandom.bytes(32)
        let (_, current) = try provisionWrap(dek: dek)
        items.setFaults { $0.read = .interactionNotAllowed }
        XCTAssertThrowsError(try keyring.unwrap(try envelope([current], dek: dek), context: nil)) {
            XCTAssertEqual($0 as? KeyItemError, .interactionNotAllowed)
        }
    }

    func testLegacyRestoreCheck() throws {
        let dek = SecureRandom.bytes(32)
        XCTAssertTrue(keyring.isLegacyWrapFromThisMac(try KeyFixture.legacyWrap(dek: dek)))
        XCTAssertTrue(keyring.isLegacyWrapFromThisMac(try KeyFixture.legacyWrap(dek: dek, requireBiometrics: true)))
        XCTAssertFalse(keyring.isLegacyWrapFromThisMac(try KeyFixture.legacyWrap(dek: dek, device: KeyFixture.otherMac)))
        XCTAssertFalse(keyring.isLegacyWrapFromThisMac(try provisionWrap(dek: dek).1))
        let unavailable = makeKeyring(enclave: SoftwareEnclaveKeyFactory(deviceTag: KeyFixture.thisMac,
                                                                         isAvailable: false))
        XCTAssertFalse(unavailable.isLegacyWrapFromThisMac(try KeyFixture.legacyWrap(dek: dek)))
    }

    func testLocalKeysListsOnlyItemsInTheKeychain() throws {
        let dek = SecureRandom.bytes(32)
        let (first, firstWrap) = try provisionWrap(dek: dek)
        let (second, secondWrap) = try provisionWrap(dek: dek, pending: true)
        let env = try envelope([secondWrap, try KeyFixture.foreignCurrentWrap(), firstWrap,
                                try KeyFixture.legacyWrap(dek: dek)], dek: dek)
        let local = try keyring.localKeys(in: env)
        XCTAssertEqual(local.map(\.id), [second.id, first.id])
        XCTAssertEqual(local.map(\.item.isRotationPending), [true, false])
    }

    // MARK: - Delete and sweep

    func testDeleteKeyRemovesItemAndRegistration() throws {
        let (key, _) = try provisionWrap(dek: SecureRandom.bytes(32))
        try keyring.deleteKey(key.id, for: vaultPath)
        XCTAssertEqual(items.accounts, [])
        XCTAssertEqual(registry.ids(for: vaultPath), [])
        try keyring.deleteKey(key.id, for: vaultPath)
    }

    func testSweepDeletesOnlyUnreferencedIDsOfThisVault() throws {
        let dek = SecureRandom.bytes(32)
        let (kept, _) = try provisionWrap(dek: dek)
        let (dropped, _) = try provisionWrap(dek: dek)
        let otherPath = "/private/tmp/tessera-keyring-tests/copy.json"
        let elsewhere = try keyring.createKey(for: otherPath, requireBiometrics: false, isRotationPending: false,
                                              context: nil)
        let orphan = SEKeyID.random()
        registry.register(orphan, for: vaultPath)

        let swept = keyring.sweep(vaultPath: vaultPath, referenced: [kept.id])
        XCTAssertEqual(Set(swept), [dropped.id, orphan])
        XCTAssertEqual(registry.ids(for: vaultPath), [kept.id])
        XCTAssertEqual(Set(items.accounts), [kept.id.account, elsewhere.id.account])
        XCTAssertEqual(registry.ids(for: otherPath), [elsewhere.id])
    }

    func testSweepKeepsTheRegistrationWhenDeleteFails() throws {
        let (key, _) = try provisionWrap(dek: SecureRandom.bytes(32))
        items.setFaults { $0.delete = .interactionNotAllowed }
        XCTAssertEqual(keyring.sweep(vaultPath: vaultPath, referenced: []), [])
        XCTAssertEqual(registry.ids(for: vaultPath), [key.id])
        items.setFaults { $0.delete = nil }
        XCTAssertEqual(keyring.sweep(vaultPath: vaultPath, referenced: []), [key.id])
        XCTAssertEqual(items.accounts, [])
    }
}
