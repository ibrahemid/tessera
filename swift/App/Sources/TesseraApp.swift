import SwiftUI

enum AppTheme: String, CaseIterable, Identifiable {
    case system, light, dark
    var id: String { rawValue }
    var label: String { rawValue.capitalized }
    var colorScheme: ColorScheme? {
        switch self { case .system: return nil; case .light: return .light; case .dark: return .dark }
    }
}

@main
struct TesseraApp: App {
    @NSApplicationDelegateAdaptor(AppDelegate.self) private var appDelegate
    @StateObject private var model = AppModel()
    @AppStorage("tessera.theme") private var theme: AppTheme = .system
    @AppStorage("tessera.compact") private var compact = false

    var body: some Scene {
        WindowGroup(id: "main") {
            RootView()
                .environmentObject(model)
                .preferredColorScheme(theme.colorScheme)
                .frame(minWidth: compact ? 380 : 720, minHeight: 470)
        }
        .defaultSize(width: 920, height: 620)
        .commands {
            CommandGroup(replacing: .newItem) {}
            CommandGroup(after: .appInfo) {
                Button("Lock Vault") { model.lock() }
                    .keyboardShortcut("l", modifiers: [.command, .shift])
                    .disabled(model.isLocked || !model.vaultExists)
            }
        }

        // Menu-bar quick access returns post-v1 via a stable NSStatusItem; the
        // SwiftUI MenuBarExtra(.window) + WindowGroup combo spins the CPU.

        Settings {
            SettingsView()
                .environmentObject(model)
                .preferredColorScheme(theme.colorScheme)
        }
    }
}
