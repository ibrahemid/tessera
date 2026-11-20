import AppKit
import SwiftUI
import TesseraCore

#if DEBUG
extension Screenshots {
    static var vaultShots: [Shot] {
        let narrowSize = CGSize(width: 380, height: 620)
        let tightSize = CGSize(width: 720, height: 620)
        let lastSeconds = Date(timeIntervalSince1970: 1_800_000_027)
        let longFailure = UserMessage.text(for: CocoaError(.fileWriteOutOfSpace), context: .export,
                                           url: URL(fileURLWithPath: "/Users/demo/Documents/shared-work-accounts-backup.json"))
        return [
            Shot(screen: "vault", variant: "selected", size: mainSize, chrome: .window,
                 view: vault(VaultPreset(selected: ["3"]))),
            Shot(screen: "vault", variant: "hover", size: mainSize, chrome: .window,
                 view: vault(VaultPreset(hoverID: "7"))),
            Shot(screen: "vault", variant: "multiselect", size: mainSize, chrome: .window,
                 setUp: { $0.notice = .info("Pinned 3 accounts") },
                 view: vault(VaultPreset(selected: ["3", "4", "5"]))),
            Shot(screen: "vault", variant: "hidden", size: mainSize, chrome: .window,
                 view: vault(VaultPreset(hideCodes: true))),
            Shot(screen: "vault", variant: "hiddenreveal", size: mainSize, chrome: .window,
                 view: vault(VaultPreset(selected: ["3"], hoverID: "1", hideCodes: true))),
            Shot(screen: "vault", variant: "lastseconds", size: mainSize, chrome: .window,
                 setUp: { m in
                     m.ticker.freeze(at: lastSeconds)
                     m.notice = .info("Copied the next code for Google")
                 },
                 view: vault(VaultPreset(copiedID: "3", copiedNext: true))),
            Shot(screen: "vault", variant: "tags", size: mainSize, chrome: .window,
                 setUp: addTags, view: vault(VaultPreset(selected: ["1"]))),
            Shot(screen: "vault", variant: "search", size: mainSize, chrome: .window,
                 setUp: addTags, view: vault(VaultPreset(query: "dev"))),
            Shot(screen: "vault", variant: "sidebar", size: mainSize, chrome: .window,
                 setUp: addFolders, view: vault(VaultPreset(scope: .folder("Home")))),
            Shot(screen: "vault", variant: "narrow", size: narrowSize, chrome: .window, compact: true,
                 setUp: addTags, view: vault(VaultPreset(selected: ["3"]))),
            Shot(screen: "vault", variant: "narrowbulk", size: narrowSize, chrome: .window, compact: true,
                 view: vault(VaultPreset(selected: ["1", "2"]))),
            Shot(screen: "vault", variant: "compactbulk", size: compactSize, chrome: .window, compact: true,
                 setUp: addTags, view: vault(VaultPreset(selected: ["3", "4"], hideCodes: true))),
            Shot(screen: "vault", variant: "contrastselected", size: mainSize, chrome: .window, highContrast: true,
                 view: vault(VaultPreset(selected: ["3", "5"]))),
            Shot(screen: "vault", variant: "contrasthidden", size: mainSize, chrome: .window, highContrast: true,
                 view: vault(VaultPreset(selected: ["3"], hideCodes: true))),
            Shot(screen: "vault", variant: "selectedtight", size: tightSize, chrome: .window,
                 setUp: addTags, view: vault(VaultPreset(selected: ["3"]))),
            Shot(screen: "vault", variant: "selectedcompact", size: compactSize, chrome: .window, compact: true,
                 setUp: addTags, view: vault(VaultPreset(selected: ["3"]))),
            Shot(screen: "vault", variant: "lastsecondsselected", size: mainSize, chrome: .window,
                 setUp: { $0.ticker.freeze(at: lastSeconds) },
                 view: vault(VaultPreset(selected: ["3"]))),
            Shot(screen: "vault", variant: "lastsecondsnarrow", size: narrowSize, chrome: .window, compact: true,
                 setUp: { $0.ticker.freeze(at: lastSeconds) },
                 view: vault(VaultPreset(selected: ["3"]))),
            Shot(screen: "notice", variant: "longscrolled", size: mainSize, chrome: .window,
                 setUp: { m in
                     addAccounts(m)
                     m.notice = .failure(longFailure)
                 },
                 afterAppear: scrollListsToEnd,
                 view: vault(VaultPreset())),
            Shot(screen: "notice", variant: "longnarrow", size: narrowSize, chrome: .window, compact: true,
                 setUp: { m in
                     addAccounts(m)
                     m.notice = .failure(longFailure)
                 },
                 afterAppear: scrollListsToEnd,
                 view: vault(VaultPreset())),
            Shot(screen: "notice", variant: "longbulk", size: compactSize, chrome: .window, compact: true,
                 setUp: { m in
                     addAccounts(m)
                     m.notice = .failure(longFailure)
                 },
                 afterAppear: scrollListsToEnd,
                 view: vault(VaultPreset(selected: ["11", "12"]))),
        ]
    }

    private static func vault(_ preset: VaultPreset) -> () -> AnyView {
        {
            AnyView(
                ZStack {
                    Palette.background.ignoresSafeArea()
                    VaultView(preset: preset)
                }
                .tint(Palette.accent)
                .frame(maxWidth: .infinity, maxHeight: .infinity)
            )
        }
    }

    private static func addTags(_ model: AppModel) {
        let tags: [String: [String]] = ["1": ["dev", "oss"], "2": ["dns"], "3": ["personal"], "7": ["bank", "family"]]
        for i in model.accounts.indices {
            if let t = tags[model.accounts[i].id] { model.accounts[i].tags = t }
        }
    }

    private static func addAccounts(_ model: AppModel) {
        let extra = [("Dropbox", "you@example.com"), ("Fastmail", "you@example.com"), ("Linear", "demo"),
                     ("Notion", "you@example.com"), ("Slack", "demo-team"), ("Stripe", "billing@example.com")]
        var accounts = model.accounts
        for (index, entry) in extra.enumerated() {
            accounts.append(Account(id: "\(index + 7)", type: .totp, issuer: entry.0, account: entry.1,
                                    secret: Data("demosecret\(index)0000000000".utf8)))
        }
        _ = Handles.assign(&accounts)
        model.accounts = accounts
    }

    // The shared scroll-to-bottom step stops short of a list's bottom content inset.
    private static func scrollListsToEnd(_: AppModel) {
        for window in NSApp.windows where window.isVisible {
            guard let root = window.contentView else { continue }
            for scroll in scrollViews(in: root) where scroll.contentInsets.bottom > 0 {
                guard let doc = scroll.documentView, doc.isFlipped else { continue }
                let clip = scroll.contentView
                let end = clip.constrainBoundsRect(NSRect(origin: NSPoint(x: 0, y: doc.frame.height),
                                                          size: clip.bounds.size))
                clip.scroll(to: end.origin)
                scroll.reflectScrolledClipView(clip)
            }
        }
    }

    private static func scrollViews(in view: NSView) -> [NSScrollView] {
        let own = (view as? NSScrollView).map { [$0] } ?? []
        return own + view.subviews.flatMap { scrollViews(in: $0) }
    }

    private static func addFolders(_ model: AppModel) {
        for i in model.accounts.indices where ["3", "4", "7"].contains(model.accounts[i].id) {
            model.accounts[i].folder = "Home"
        }
    }
}
#endif
