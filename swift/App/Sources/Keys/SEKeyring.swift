import Foundation
import LocalAuthentication
import TesseraCore

/// A key that was created, written to the keychain, read back by a fresh
/// query and restored from what the keychain returned.
struct ProvisionedSEKey {
    let id: SEKeyID
    let key: any EnclaveKey
    let isRotationPending: Bool
}

/// How far `createKey` got, reported after each step.
enum KeyProvisionStage: Sendable, Equatable { case registered, itemWritten, readBack }

/// The wrap that opened, and the DEK it holds.
enum SEUnwrap {
    case current(dek: Data, id: SEKeyID, isRotationPending: Bool, wrap: VaultWrap)
    case legacy(dek: Data, wrap: VaultWrap)

    var dek: Data {
        switch self {
        case .current(let dek, _, _, _), .legacy(let dek, _): return dek
        }
    }
}

/// A current-form wrap whose key item is in this Mac's keychain.
struct LocalSEKey {
    let id: SEKeyID
    let item: KeyItem
    let wrap: VaultWrap
}

struct SEKeyring: Sendable {
    let items: any KeyItemStore
    let enclave: any EnclaveKeyFactory
    let registry: SEKeyRegistry

    init(items: any KeyItemStore, enclave: any EnclaveKeyFactory, registry: SEKeyRegistry) {
        self.items = items
        self.enclave = enclave
        self.registry = registry
    }

    var isAvailable: Bool { enclave.isAvailable }

    /// Registers `id` for the vault, makes the key, adds its item, then reads
    /// the item back and restores the key from it. A failure in any of those
    /// steps removes what the call added; an error thrown by `progress` is
    /// passed on with everything left in place.
    func createKey(id: SEKeyID = .random(), for vaultPath: String, requireBiometrics: Bool,
                   isRotationPending: Bool, context: LAContext?,
                   progress: (KeyProvisionStage) throws -> Void = { _ in }) throws -> ProvisionedSEKey {
        registry.register(id, for: vaultPath)
        try progress(.registered)

        let made: any EnclaveKey
        do {
            made = try enclave.makeKey(requireBiometrics: requireBiometrics, context: context)
            try items.add(made.dataRepresentation, account: id.account, isRotationPending: isRotationPending)
        } catch let error as KeyItemError where error == .duplicate {
            registry.unregister(id, for: vaultPath)
            throw error
        } catch {
            discard(id, for: vaultPath)
            throw error
        }
        try progress(.itemWritten)

        let provisioned: ProvisionedSEKey
        do {
            provisioned = try readBack(id, expecting: made, isRotationPending: isRotationPending, context: context)
        } catch {
            discard(id, for: vaultPath)
            throw error
        }
        try progress(.readBack)
        return provisioned
    }

    /// A current-form wrap of `dek`, opened again with the same key before it is returned.
    func makeWrap(dek: Data, key: ProvisionedSEKey) throws -> VaultWrap {
        let wrap = try SEWrapCodec.seal(dek: dek, with: key.key, id: key.id)
        var opened = try SEWrapCodec.open(wrap: wrap, with: key.key)
        defer { opened.resetBytes(in: 0..<opened.count) }
        guard opened == dek else { throw SEKeyError.verifyFailed }
        return wrap
    }

    /// Tries each current-form wrap whose item is in the keychain, then each
    /// legacy wrap whose key restores on this Mac.
    func unwrap(_ envelope: Envelope, context: LAContext?) throws -> SEUnwrap {
        for wrap in envelope.secureEnclaveWraps(.current) {
            guard let id = wrap.secureEnclaveKeyID else { continue }
            let item: KeyItem
            do {
                item = try items.read(account: id.account)
            } catch KeyItemError.notFound {
                continue
            }
            let key = try enclave.restoreKey(dataRepresentation: item.data, context: context)
            return .current(dek: try SEWrapCodec.open(wrap: wrap, with: key), id: id,
                            isRotationPending: item.isRotationPending, wrap: wrap)
        }
        for wrap in envelope.secureEnclaveWraps(.legacy) {
            guard let blob = try? SEWrapCodec.legacyKeyData(wrap),
                  let key = try? enclave.restoreKey(dataRepresentation: blob, context: context) else { continue }
            return .legacy(dek: try SEWrapCodec.open(wrap: wrap, with: key), wrap: wrap)
        }
        throw SEKeyError.noWrap
    }

    /// Whether a legacy wrap's key restores here. Restoring does not use the
    /// key, so it never prompts.
    func isLegacyWrapFromThisMac(_ wrap: VaultWrap) -> Bool {
        guard enclave.isAvailable, let blob = try? SEWrapCodec.legacyKeyData(wrap) else { return false }
        return (try? enclave.restoreKey(dataRepresentation: blob, context: nil)) != nil
    }

    /// The current-form wraps of `envelope` whose items are in the keychain, in envelope order.
    func localKeys(in envelope: Envelope) throws -> [LocalSEKey] {
        var out: [LocalSEKey] = []
        for wrap in envelope.secureEnclaveWraps(.current) {
            guard let id = wrap.secureEnclaveKeyID else { continue }
            do {
                out.append(LocalSEKey(id: id, item: try items.read(account: id.account), wrap: wrap))
            } catch KeyItemError.notFound {
                continue
            }
        }
        return out
    }

    func deleteKey(_ id: SEKeyID, for vaultPath: String) throws {
        try items.delete(account: id.account)
        registry.unregister(id, for: vaultPath)
    }

    /// Deletes the ids registered to the vault that `referenced` does not
    /// hold. Best effort: an id whose item cannot be deleted stays registered.
    @discardableResult
    func sweep(vaultPath: String, referenced: Set<SEKeyID>) -> [SEKeyID] {
        registry.ids(for: vaultPath).filter { id in
            guard !referenced.contains(id) else { return false }
            return (try? deleteKey(id, for: vaultPath)) != nil
        }
    }

    private func readBack(_ id: SEKeyID, expecting made: any EnclaveKey, isRotationPending: Bool,
                          context: LAContext?) throws -> ProvisionedSEKey {
        let item: KeyItem
        do {
            item = try items.read(account: id.account)
        } catch KeyItemError.notFound {
            throw SEKeyError.keyItemMissing(id)
        }
        guard item.data == made.dataRepresentation, item.isRotationPending == isRotationPending else {
            throw SEKeyError.readBackMismatch(id)
        }
        let restored = try enclave.restoreKey(dataRepresentation: item.data, context: context)
        guard restored.dataRepresentation == item.data,
              restored.publicKey.rawRepresentation == made.publicKey.rawRepresentation else {
            throw SEKeyError.readBackMismatch(id)
        }
        return ProvisionedSEKey(id: id, key: restored, isRotationPending: item.isRotationPending)
    }

    private func discard(_ id: SEKeyID, for vaultPath: String) {
        if (try? items.delete(account: id.account)) != nil { registry.unregister(id, for: vaultPath) }
    }
}
