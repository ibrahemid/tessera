import Foundation
import ServiceManagement

/// Toggles the app's launch-at-login state via SMAppService (macOS 13+).
enum LoginItem {
    static func set(enabled: Bool) {
        do {
            if enabled { try SMAppService.mainApp.register() }
            else { try SMAppService.mainApp.unregister() }
        } catch {
            NSLog("Tessera: login item toggle failed: \(error)")
        }
    }
}
