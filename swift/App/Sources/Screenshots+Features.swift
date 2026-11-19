import SwiftUI
import Carbon.HIToolbox
import TesseraCore

#if DEBUG
extension Screenshots {
    static var featureShots: [Shot] {
        let lastSeconds = Date(timeIntervalSince1970: 1_800_000_027)
        let shortcut = GlobalShortcut(keyCode: UInt32(kVK_ANSI_T), modifiers: UInt32(optionKey | cmdKey), key: "t")
        let link = "otpauth://totp/GitHub:octocat?secret=JBSWY3DPEHPK3PXP&issuer=GitHub"
        let migration = "otpauth-migration://offline?data=CjEKCkhlbGxvId6tvu8SGEV4YW1wbGU6YWxpY2VAZ29vZ2xlLmNvbRoHRXhhbXBsZSABKAEwAhABGAEgACjr4JKkBg%3D%3D"
        let tagged = editable("1", tags: ["dev", "oss"])
        return [
            Shot(screen: "quick", variant: "list", view: quick()),
            Shot(screen: "quick", variant: "search", view: quick(query: "g")),
            Shot(screen: "quick", variant: "cursor", view: quick(cursor: "3")),
            Shot(screen: "quick", variant: "nextcode", view: quick(cursor: "3", at: lastSeconds)),
            Shot(screen: "quick", variant: "nomatch", view: quick(query: "zzz")),
            Shot(screen: "quick", variant: "empty", state: .empty, view: quick()),
            Shot(screen: "quick", variant: "hidden", view: quick(hideCodes: true)),
            Shot(screen: "quick", variant: "hiddencursor", view: quick(cursor: "3", hideCodes: true)),
            Shot(screen: "quick", variant: "locked", state: .locked, view: quick()),
            Shot(screen: "quick", variant: "lockedplain", state: .locked,
                 setUp: { $0.requireBiometrics = false }, view: quick()),
            Shot(screen: "quick", variant: "needswindow", state: .locked,
                 setUp: { $0.needsPassphrase = true }, view: quick()),

            Shot(screen: "settings", variant: "general-none", setUp: { $0.quickAccess.preview() }) {
                AnyView(SettingsView(tab: .general))
            },
            Shot(screen: "settings", variant: "general-shortcut",
                 setUp: { $0.quickAccess.preview(shortcut: shortcut) }) { AnyView(SettingsView(tab: .general)) },
            Shot(screen: "settings", variant: "general-recording",
                 setUp: { $0.quickAccess.preview(recording: true) }) { AnyView(SettingsView(tab: .general)) },
            Shot(screen: "settings", variant: "general-taken",
                 setUp: { $0.quickAccess.preview(issue: .taken(shortcut.display)) }) {
                AnyView(SettingsView(tab: .general))
            },
            Shot(screen: "settings", variant: "general-menu",
                 setUp: { $0.quickAccess.preview(showsInMenuBar: false, shortcut: shortcut,
                                                 issue: .usedByMenu("⌘N", item: "Add Account")) }) {
                AnyView(SettingsView(tab: .general))
            },

            Shot(screen: "edit", variant: "tags") {
                AnyView(EditAccountView(account: tagged, draft: ParameterDraft(tagged), tagInput: "work",
                                        showsAdvanced: false))
            },
            Shot(screen: "edit", variant: "advanced") {
                AnyView(EditAccountView(account: tagged, draft: ParameterDraft(tagged)))
            },
            Shot(screen: "edit", variant: "changed") {
                var draft = ParameterDraft(tagged)
                draft.digits = 8
                draft.period = 60
                return AnyView(EditAccountView(account: tagged, draft: draft))
            },
            Shot(screen: "edit", variant: "secret") {
                var draft = ParameterDraft(tagged)
                draft.secretText = "JBSWY3DPEHPK3PXP"
                return AnyView(EditAccountView(account: tagged, draft: draft))
            },
            Shot(screen: "edit", variant: "invalid") {
                var draft = ParameterDraft(tagged)
                draft.secretText = "not-a-key-1"
                return AnyView(EditAccountView(account: tagged, draft: draft))
            },
            Shot(screen: "edit", variant: "hotp") {
                let bank = editable("6")
                return AnyView(EditAccountView(account: bank, draft: ParameterDraft(bank)))
            },
            Shot(screen: "edit", variant: "steam") {
                let steam = editable("4")
                return AnyView(EditAccountView(account: steam, draft: ParameterDraft(steam)))
            },

            Shot(screen: "add", variant: "link") { AnyView(AddAccountView(prefill: link)) },
            Shot(screen: "add", variant: "migrationlink") { AnyView(AddAccountView(prefill: migration)) },
        ]
    }

    private static func quick(query: String = "", cursor: String? = nil, hideCodes: Bool = false,
                              at time: Date = frozenNow) -> () -> AnyView {
        {
            let clock = QuickAccessClock()
            clock.freeze(at: time)
            return AnyView(QuickAccessView(clock: clock, query: query, cursor: cursor, hideCodes: hideCodes))
        }
    }

    private static func editable(_ id: String, tags: [String] = []) -> Account {
        var account = accounts.first { $0.id == id } ?? accounts[0]
        account.tags = tags
        return account
    }
}
#endif
