import Foundation

struct AppDependencies {
    var makeTransaction: @Sendable (VaultLocation) -> any VaultTransaction

    static func live() -> AppDependencies {
        AppDependencies(makeTransaction: { VaultTransactions.live($0) })
    }
}

extension AppModel {
    /// Render modes get an inert model: no vault file and no Keychain access.
    static func makeForLaunch() -> AppModel {
        #if DEBUG
        if Screenshots.isRequested || CommandLine.arguments.contains("--marketing") {
            return AppModel(demo: .locked)
        }
        #endif
        return AppModel()
    }
}
