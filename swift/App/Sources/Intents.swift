import AppIntents
import AppKit
import Combine
import LocalAuthentication
import TesseraCore

enum CopyCodeError: Error, Equatable, CustomLocalizedStringResourceConvertible {
    case authenticationUnavailable
    case notAuthenticated
    case vaultUnavailable
    case accountMissing
    case copyFailed

    var text: String {
        switch self {
        case .authenticationUnavailable: return "This Mac can’t confirm it’s you, so Tessera didn’t copy the code."
        case .notAuthenticated: return "Tessera didn’t copy the code because authentication didn’t finish."
        case .vaultUnavailable: return "Tessera can’t open the vault. Open Tessera to unlock it."
        case .accountMissing: return "That account isn’t in your vault."
        case .copyFailed: return "Tessera couldn’t copy the code."
        }
    }

    var localizedStringResource: LocalizedStringResource { LocalizedStringResource(stringLiteral: text) }
}

@MainActor
protocol IntentAuthenticating {
    func authenticate(reason: String) async throws
}

struct DeviceOwnerAuthenticator: IntentAuthenticating {
    func authenticate(reason: String) async throws {
        let context = LAContext()
        guard context.canEvaluatePolicy(.deviceOwnerAuthentication, error: nil) else {
            throw CopyCodeError.authenticationUnavailable
        }
        do {
            guard try await context.evaluatePolicy(.deviceOwnerAuthentication, localizedReason: reason) else {
                throw CopyCodeError.notAuthenticated
            }
        } catch let e as CopyCodeError {
            throw e
        } catch {
            throw CopyCodeError.notAuthenticated
        }
    }
}

@MainActor
protocol CodeCopyTarget: AnyObject {
    var isLocked: Bool { get }
    func openForIntent() async -> Bool
    func account(withID id: String) -> Account?
    func copyCodeForIntent(_ account: Account) -> Bool
}

enum CopyCodeGate {
    static let reason = "copy a code"

    @MainActor
    static func run(accountID: String, target: CodeCopyTarget, authenticator: IntentAuthenticating) async throws -> String {
        try await authenticator.authenticate(reason: reason)
        if target.isLocked {
            guard await target.openForIntent(), !target.isLocked else { throw CopyCodeError.vaultUnavailable }
        }
        guard let account = target.account(withID: accountID) else { throw CopyCodeError.accountMissing }
        guard target.copyCodeForIntent(account) else { throw CopyCodeError.copyFailed }
        return account.displayName
    }
}

extension AppModel: CodeCopyTarget {
    func openForIntent() async -> Bool {
        guard isLocked else { return true }
        guard !isDemo, QuickAccessLockState(self) != .needsWindow else { return false }
        if isOpening, AppSession.shared?.isMainWindowVisible == true {
            await waitUntilOpened()
        } else {
            await open(reason: "Unlock your Tessera vault")
        }
        return !isLocked
    }

    func account(withID id: String) -> Account? {
        accounts.first { $0.id == id }
    }

    func copyCodeForIntent(_ account: Account) -> Bool {
        copyFromQuickAccess(account, at: Date())
    }

    private func waitUntilOpened() async {
        guard isOpening else { return }
        var subscription: AnyCancellable?
        await withCheckedContinuation { (done: CheckedContinuation<Void, Never>) in
            subscription = $isOpening.first { !$0 }.sink { _ in done.resume() }
        }
        subscription?.cancel()
    }
}

struct AccountEntity: AppEntity, Identifiable, Sendable {
    static var typeDisplayRepresentation: TypeDisplayRepresentation { "Account" }
    static var defaultQuery: AccountQuery { AccountQuery() }

    let id: String
    let name: String

    var displayRepresentation: DisplayRepresentation { DisplayRepresentation(title: "\(name)") }
}

struct AccountQuery: EntityQuery {
    @MainActor
    func entities(for identifiers: [String]) async throws -> [AccountEntity] {
        guard let model = AppSession.shared?.model, !model.isLocked else {
            return identifiers.map { AccountEntity(id: $0, name: "Account") }
        }
        let byID = Dictionary(model.accounts.map { ($0.id, $0) }, uniquingKeysWith: { a, _ in a })
        return identifiers.compactMap { id in byID[id].map { AccountEntity(id: id, name: $0.displayName) } }
    }

    @MainActor
    func suggestedEntities() async throws -> [AccountEntity] {
        guard let model = AppSession.shared?.model, !model.isLocked else { return [] }
        return model.accounts.map { AccountEntity(id: $0.id, name: $0.displayName) }
    }
}

struct CopyCodeIntent: AppIntent {
    static var title: LocalizedStringResource { "Copy Code" }
    static var description: IntentDescription {
        IntentDescription("Copies the current code for an account after Touch ID or your Mac password.")
    }
    static var openAppWhenRun: Bool { false }
    static var parameterSummary: some ParameterSummary { Summary("Copy the code for \(\.$account)") }

    @Parameter(title: "Account")
    var account: AccountEntity

    init() {}

    @MainActor
    func perform() async throws -> some IntentResult & ProvidesDialog {
        guard let model = AppSession.shared?.model else { throw CopyCodeError.vaultUnavailable }
        let name = try await CopyCodeGate.run(accountID: account.id, target: model,
                                              authenticator: DeviceOwnerAuthenticator())
        return .result(dialog: "Copied the code for \(name).")
    }
}

struct LockTesseraIntent: AppIntent {
    static var title: LocalizedStringResource { "Lock Tessera" }
    static var description: IntentDescription { IntentDescription("Locks the Tessera vault.") }
    static var openAppWhenRun: Bool { false }

    init() {}

    @MainActor
    func perform() async throws -> some IntentResult {
        AppSession.shared?.model.lock()
        return .result()
    }
}

struct TesseraShortcuts: AppShortcutsProvider {
    static var appShortcuts: [AppShortcut] {
        AppShortcut(intent: CopyCodeIntent(),
                    phrases: ["Copy a code with \(.applicationName)", "Copy a \(.applicationName) code"],
                    shortTitle: "Copy Code", systemImageName: "doc.on.doc")
        AppShortcut(intent: LockTesseraIntent(),
                    phrases: ["Lock \(.applicationName)"],
                    shortTitle: "Lock Tessera", systemImageName: "lock")
    }
}
