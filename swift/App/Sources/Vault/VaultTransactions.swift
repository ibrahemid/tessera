import Foundation

enum VaultTransactions {
    static func live(_ location: VaultLocation) -> any VaultTransaction {
        VaultWriter(location: location)
    }
}
