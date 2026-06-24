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
    @StateObject private var model = AppModel()
    @AppStorage("tessera.theme") private var theme: AppTheme = .system

    var body: some Scene {
        MenuBarExtra("Tessera", systemImage: "lock.shield") {
            RootView()
                .environmentObject(model)
                .preferredColorScheme(theme.colorScheme)
                .frame(width: 360, height: 460)
        }
        .menuBarExtraStyle(.window)

        Settings {
            SettingsView()
                .environmentObject(model)
                .preferredColorScheme(theme.colorScheme)
        }
    }
}

struct RootView: View {
    @EnvironmentObject var model: AppModel
    var body: some View {
        Group {
            if !model.vaultExists {
                CreateVaultView()
            } else if model.isLocked {
                UnlockView()
            } else {
                MenuView()
            }
        }
        .padding(12)
    }
}
