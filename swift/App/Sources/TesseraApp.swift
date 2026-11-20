import SwiftUI

enum AppTheme: String, CaseIterable, Identifiable {
    case system, light, dark
    var id: String { rawValue }
    var label: String { rawValue.capitalized }
    var colorScheme: ColorScheme? {
        switch self { case .system: return nil; case .light: return .light; case .dark: return .dark }
    }
}

enum AppLaunch {
    enum Mode: Equatable { case app, inertTestHost }

    @MainActor fileprivate(set) static var current: Mode?

    #if DEBUG
    private static let testHostKeys = ["XCTestConfigurationFilePath", "XCTestBundlePath", "XCTestSessionIdentifier"]

    static func isUnitTestHost(environment: [String: String] = ProcessInfo.processInfo.environment,
                               arguments: [String] = CommandLine.arguments) -> Bool {
        guard !arguments.contains("-uitest") else { return false }
        return testHostKeys.contains { environment[$0] != nil }
    }

    static func mode(environment: [String: String] = ProcessInfo.processInfo.environment,
                     arguments: [String] = CommandLine.arguments) -> Mode {
        isUnitTestHost(environment: environment, arguments: arguments) ? .inertTestHost : .app
    }
    #else
    static func mode() -> Mode { .app }
    #endif
}

@main
enum AppLauncher {
    @MainActor static func main() {
        let mode = AppLaunch.mode()
        AppLaunch.current = mode
        switch mode {
        case .inertTestHost: InertTestHostApp.main()
        case .app: TesseraApp.main()
        }
    }
}

struct InertTestHostApp: App {
    var body: some Scene {
        Settings { EmptyView() }
    }
}

struct TesseraApp: App {
    @NSApplicationDelegateAdaptor(AppDelegate.self) private var appDelegate
    @StateObject private var model: AppModel
    @AppStorage("tessera.theme") private var theme: AppTheme = .system
    @AppStorage("tessera.compact") private var compact = false
    private let lockPolicy: LockPolicy?

    init() {
        let model = AppModel.makeForLaunch()
        _model = StateObject(wrappedValue: model)
        guard AppSession.isEnabled else {
            lockPolicy = nil
            return
        }
        lockPolicy = LockPolicy(
            defaults: .standard,
            idle: SystemIdleTimeSource(),
            workspace: NSWorkspace.shared.notificationCenter,
            distributed: DistributedNotificationCenter.default(),
            appCenter: .default,
            isVaultUnlocked: { [weak model] in model?.isLocked == false },
            onLock: { [weak model] reason in model?.lock(reason: reason) }
        )
        AppSession.start(model: model)
    }

    var body: some Scene {
        // Single Window scene (not WindowGroup): lists "Tessera" in the Window
        // menu so the main window can be reopened after close (App Review 4.0).
        Window("Tessera", id: "main") {
            RootView()
                .environmentObject(model)
                .preferredColorScheme(theme.colorScheme)
                .frame(minWidth: compact ? 380 : 720, minHeight: 470)
                .background(MainWindowBridge())
        }
        .defaultSize(width: 920, height: 620)
        .commands { VaultCommands(model: model) }

        // MenuBarExtra(.window) with a window scene spun the CPU; the menu bar item is an NSStatusItem.

        Settings {
            SettingsView()
                .environmentObject(model)
                .preferredColorScheme(theme.colorScheme)
        }
    }
}
