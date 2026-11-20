import Foundation
import ServiceManagement

/// The parts of `SMAppService` launch at login uses, so the state logic can run
/// against a stand-in in tests.
@MainActor
protocol LoginItemService: AnyObject {
    var status: SMAppService.Status { get }
    func register() throws
    func unregister() throws
}

extension SMAppService: LoginItemService {}

/// Launch-at-login state read from the system on every refresh, never from a
/// stored preference, so the toggle matches what System Settings shows.
@MainActor
final class LaunchAtLogin: ObservableObject {
    @Published private(set) var isEnabled = false
    /// Registered, but macOS is waiting for the user to allow it in Login Items.
    @Published private(set) var needsApproval = false
    @Published private(set) var errorText: String?

    private let service: LoginItemService

    init(service: LoginItemService = SMAppService.mainApp) {
        self.service = service
        refresh()
    }

    func refresh() {
        switch service.status {
        case .enabled:
            isEnabled = true; needsApproval = false
        case .requiresApproval:
            isEnabled = true; needsApproval = true
        case .notRegistered, .notFound:
            isEnabled = false; needsApproval = false
        @unknown default:
            isEnabled = false; needsApproval = false
        }
    }

    func setEnabled(_ on: Bool) {
        do {
            if on { try service.register() } else { try service.unregister() }
            errorText = nil
        } catch {
            NSLog("Tessera: launch at login %@ failed: %@", on ? "register" : "unregister", String(describing: error))
            errorText = on
                ? "Couldn’t turn on launch at login. Add Tessera in System Settings > General > Login Items."
                : "Couldn’t turn off launch at login. Remove Tessera in System Settings > General > Login Items."
        }
        refresh()
    }

    func openLoginItemsSettings() {
        SMAppService.openSystemSettingsLoginItems()
    }
}
