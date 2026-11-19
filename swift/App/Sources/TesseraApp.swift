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

    init() {
        let model = TesseraApp.makeModel()
        _model = StateObject(wrappedValue: model)
        if AppSession.isEnabled { AppSession.start(model: model) }
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

    /// Render modes get an inert model: no vault file and no Keychain access.
    private static func makeModel() -> AppModel {
        #if DEBUG
        if Screenshots.isRequested || CommandLine.arguments.contains("--marketing") {
            return AppModel(demo: .locked)
        }
        #endif
        return AppModel()
    }
}
