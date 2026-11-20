import SwiftUI
import ServiceManagement
import TesseraCore

#if DEBUG
extension Screenshots {
    static var sheetShots: [Shot] {
        let root = { AnyView(RootView()) }
        let backupFile = URL(fileURLWithPath: "/Users/demo/Documents/tessera-backup.json")
        return [
            Shot(screen: "settings", variant: "general-approval") {
                AnyView(SettingsView(tab: .general, launchAtLogin: LaunchAtLogin(service: ShotLoginItems(.requiresApproval))))
            },
            Shot(screen: "settings", variant: "general-error") {
                let login = LaunchAtLogin(service: ShotLoginItems(.notRegistered, fails: true))
                login.setEnabled(true)
                return AnyView(SettingsView(tab: .general, launchAtLogin: login))
            },
            Shot(screen: "settings", variant: "security-locked", state: .locked) { AnyView(SettingsView(tab: .security)) },
            Shot(screen: "settings", variant: "security-feedback") {
                AnyView(SettingsView(tab: .security, feedback: [
                    .touchID: .failure(UserMessage.text(for: CocoaError(.fileWriteNoPermission), context: .save,
                                                        url: URL(fileURLWithPath: "/Users/demo/vault.json"))),
                    .passphrase: .info("Recovery passphrase saved."),
                ]))
            },
            Shot(screen: "settings", variant: "vault-feedback") {
                AnyView(SettingsView(tab: .vault, feedback: [.duplicates: .info("No duplicates found.")]))
            },
            Shot(screen: "settings", variant: "backup-feedback") {
                AnyView(SettingsView(tab: .backup, feedback: [
                    .export: .info("Backup saved as tessera-backup.json."),
                    .restore: .info(BackupRestoreOutcome.restored(3).line ?? ""),
                    .links: .failure(exportFailure("tessera-accounts.txt")),
                ]))
            },
            Shot(screen: "settings", variant: "contrast", highContrast: true) { AnyView(SettingsView(tab: .security)) },

            Shot(screen: "recoverypassphrase", variant: "short") {
                AnyView(RecoveryPassphraseView(passphrase: "abc", confirm: "abc"))
            },
            Shot(screen: "recoverypassphrase", variant: "busy") {
                AnyView(RecoveryPassphraseView(passphrase: "correct-horse", confirm: "correct-horse", saving: true))
            },

            Shot(screen: "backup", variant: "export-mismatch") {
                AnyView(ExportBackupView(password: "correct-horse", confirm: "correct-hors"))
            },
            Shot(screen: "backup", variant: "export-busy") {
                AnyView(ExportBackupView(password: "correct-horse", confirm: "correct-horse", busy: true))
            },
            Shot(screen: "backup", variant: "export-error") {
                AnyView(ExportBackupView(password: "correct-horse", confirm: "correct-horse", error: exportFailure("tessera-backup.json")))
            },
            Shot(screen: "backup", variant: "export-contrast", highContrast: true) {
                AnyView(ExportBackupView(password: "short", confirm: "short"))
            },
            Shot(screen: "backup", variant: "restore-file") {
                AnyView(RestoreBackupView(file: backupFile, password: ""))
            },
            Shot(screen: "backup", variant: "restore-busy") {
                AnyView(RestoreBackupView(file: backupFile, password: "correct-horse", busy: true))
            },
            Shot(screen: "backup", variant: "restore-error") {
                AnyView(RestoreBackupView(file: backupFile, password: "wrong-horse",
                                          error: UserMessage.text(for: VaultError.wrongPassphrase, context: .restore)))
            },
            Shot(screen: "backup", variant: "restore-notbackup") {
                AnyView(RestoreBackupView(file: nil, password: "",
                                          error: BackupFile.problem(in: Data("{}".utf8))))
            },
            Shot(screen: "backup", variant: "restore-contrast", highContrast: true) {
                AnyView(RestoreBackupView(file: backupFile, password: "wrong-horse",
                                          error: UserMessage.text(for: VaultError.wrongPassphrase, context: .restore)))
            },

            Shot(screen: "add", variant: "empty-contrast", highContrast: true) { AnyView(AddAccountView()) },
            Shot(screen: "add", variant: "importing",
                 afterAppear: { $0.importProgress = ImportProgress(done: 1, total: 4) }) { AnyView(AddAccountView()) },
            Shot(screen: "add", variant: "report-failed", afterAppear: { $0.importReport = failedReport }) {
                AnyView(AddAccountView())
            },
            Shot(screen: "add", variant: "drop") {
                AnyView(AddAccountView().overlay { FileDropHighlight() })
            },
            Shot(screen: "add", variant: "manual-contrast", highContrast: true) {
                AnyView(AddAccountView(input: "JBSWY3DPEHPK3PXP", manual: true))
            },

            Shot(screen: "edit", variant: "contrast", highContrast: true) { AnyView(EditAccountView(account: accounts[0])) },
            Shot(screen: "qr", variant: "copied") { AnyView(QRExportView(account: accounts[0], copiedSecret: true)) },

            Shot(screen: "import", variant: "drop", size: mainSize, chrome: .window) {
                AnyView(RootView().overlay { FileDropHighlight().padding(8) })
            },
            Shot(screen: "import", variant: "progress", size: mainSize, chrome: .window,
                 setUp: { $0.importProgress = ImportProgress(done: 2, total: 5) }, view: root),
            Shot(screen: "import", variant: "report", setUp: { $0.importReport = sampleReport }) {
                AnyView(AddAccountView(keepReport: true))
            },

            Shot(screen: "reset", variant: "contrast", size: mainSize, chrome: .window, state: .locked, highContrast: true,
                 setUp: { $0.needsReset = true }, view: root),
            Shot(screen: "recovery", variant: "contrast", size: mainSize, chrome: .window, state: .locked, highContrast: true,
                 setUp: { $0.needsRecovery = true }, view: root),
        ]
    }

    static func exportFailure(_ name: String) -> String {
        UserMessage.text(for: CocoaError(.fileWriteNoPermission), context: .export,
                         url: URL(fileURLWithPath: "/Users/demo/Documents/\(name)"))
    }

    static var failedReport: ImportSummary {
        ImportSummary(added: 0, duplicates: 0, failures: [
            ImportItemFailure(source: "screenshot.png", display: "", reason: "No QR code found.", kind: .noQR),
            ImportItemFailure(source: "notes.pdf", display: "", reason: "Not a QR image or a file Tessera can import."),
        ])
    }
}

@MainActor
private final class ShotLoginItems: LoginItemService {
    let status: SMAppService.Status
    let fails: Bool

    init(_ status: SMAppService.Status, fails: Bool = false) {
        self.status = status
        self.fails = fails
    }

    func register() throws { if fails { throw ShotLoginItemError.refused } }
    func unregister() throws { if fails { throw ShotLoginItemError.refused } }
}

private enum ShotLoginItemError: Error { case refused }
#endif
