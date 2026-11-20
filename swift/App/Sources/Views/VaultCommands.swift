import AppKit
import SwiftUI
import TesseraCore

enum VaultAction: Equatable {
    case add, scan, importFiles, exportBackup, restoreBackup, find
    case copy, advance, edit, togglePin, showQR, copySetupLink, copySetupKey, delete
    case moveToFolder(String), newFolder, removeFromFolder
    case addTag(String), newTag, removeTag(String)
}

struct VaultMenuContext {
    var selected: [Account]
    var folders: [String]
    var tags: [String]
    var perform: (VaultAction) -> Void
}

struct VaultMenuKey: FocusedValueKey {
    typealias Value = VaultMenuContext
}

extension FocusedValues {
    var vaultMenu: VaultMenuContext? {
        get { self[VaultMenuKey.self] }
        set { self[VaultMenuKey.self] = newValue }
    }
}

struct AccountMenuState: Equatable {
    let targets: [Account]

    var single: Account? { targets.count == 1 ? targets.first : nil }
    var isEmpty: Bool { targets.isEmpty }
    var pins: Bool { targets.isEmpty || targets.contains { !$0.pinned } }
    var pinTitle: String { pins ? "Pin" : "Unpin" }
    var pinSymbol: String { pins ? "star" : "star.slash" }
    var canAdvance: Bool { single?.type == .hotp }
    var ids: Set<String> { Set(targets.map(\.id)) }

    var sharedFolder: String? {
        let folders = Set(targets.map(\.folder))
        return folders.count == 1 ? folders.first : nil
    }

    var removeFromFolderTitle: String {
        if let f = sharedFolder, !f.isEmpty { return "Remove from \(f)" }
        return "Remove from Folder"
    }

    var hasFolder: Bool { targets.contains { !$0.folder.isEmpty } }

    var removableTags: [String] { VaultList.tags(in: targets) }

    func hasTagOnAll(_ tag: String) -> Bool {
        !targets.isEmpty && targets.allSatisfy { $0.tags.contains(tag) }
    }
}

struct AccountActionsMenu: View {
    let state: AccountMenuState
    let folders: [String]
    let tags: [String]
    var inMenuBar = false
    let perform: (VaultAction) -> Void

    var body: some View {
        Button("Copy Code", systemImage: "doc.on.doc") { perform(.copy) }
            .keyboardShortcut(inMenuBar ? KeyboardShortcut("c") : nil)
            .disabled(state.single == nil)
        if inMenuBar || state.canAdvance {
            Button("Advance Counter", systemImage: "forward") { perform(.advance) }
                .disabled(!state.canAdvance)
        }
        Button("Edit…", systemImage: "pencil") { perform(.edit) }
            .keyboardShortcut(inMenuBar ? KeyboardShortcut("e") : nil)
            .disabled(state.single == nil)
        Button(state.pinTitle, systemImage: state.pinSymbol) { perform(.togglePin) }
            .keyboardShortcut(inMenuBar ? KeyboardShortcut("p", modifiers: [.command, .shift]) : nil)
            .disabled(state.isEmpty)
        Menu {
            Button("Copy Setup Link", systemImage: "link") { perform(.copySetupLink) }
            Button("Copy Setup Key", systemImage: "key") { perform(.copySetupKey) }
            Button("Show QR Code", systemImage: "qrcode") { perform(.showQR) }
        } label: {
            Label("Setup Secret", systemImage: "qrcode.viewfinder")
        }
        .disabled(state.single == nil)
        Divider()
        Menu {
            ForEach(folders, id: \.self) { f in
                Button { perform(.moveToFolder(f)) } label: {
                    if state.sharedFolder == f { Label(f, systemImage: "checkmark") } else { Text(f) }
                }
            }
            if !folders.isEmpty { Divider() }
            Button("New Folder…") { perform(.newFolder) }
            if state.hasFolder {
                Divider()
                Button(state.removeFromFolderTitle) { perform(.removeFromFolder) }
            }
        } label: {
            Label("Move to Folder", systemImage: "folder")
        }
        .disabled(state.isEmpty)
        Menu {
            ForEach(tags, id: \.self) { t in
                Button { perform(.addTag(t)) } label: {
                    if state.hasTagOnAll(t) { Label("#\(t)", systemImage: "checkmark") } else { Text("#\(t)") }
                }
            }
            if !tags.isEmpty { Divider() }
            Button("New Tag…") { perform(.newTag) }
        } label: {
            Label("Add Tag", systemImage: "tag")
        }
        .disabled(state.isEmpty)
        Menu {
            ForEach(state.removableTags, id: \.self) { t in
                Button("#\(t)") { perform(.removeTag(t)) }
            }
        } label: {
            Label("Remove Tag", systemImage: "tag.slash")
        }
        .disabled(state.removableTags.isEmpty)
        Divider()
        Button("Delete", systemImage: "trash", role: .destructive) { perform(.delete) }
            .disabled(state.single == nil)
    }
}

struct VaultCommands: Commands {
    @ObservedObject var model: AppModel
    @FocusedValue(\.vaultMenu) private var menu
    @AppStorage(AppModel.compactPrefKey) private var compact = false
    @AppStorage(AppModel.hideCodesPrefKey) private var hideCodes = false

    static let helpURL = URL(string: "https://tessera.ibrahemid.com/support")

    var body: some Commands {
        CommandGroup(replacing: .newItem) {
            Button("Add Account…") { menu?.perform(.add) }
                .keyboardShortcut("n")
                .disabled(menu == nil)
            Button("Scan Screen for QR Code") { menu?.perform(.scan) }
                .keyboardShortcut("n", modifiers: [.command, .shift])
                .disabled(menu == nil)
            Button("Import from Files…") { menu?.perform(.importFiles) }
                .keyboardShortcut("i", modifiers: [.command, .shift])
                .disabled(menu == nil)
            Divider()
            Button("Export Encrypted Backup…") { menu?.perform(.exportBackup) }
                .keyboardShortcut("e", modifiers: [.command, .shift])
                .disabled(menu == nil)
            Button("Restore from Backup…") { menu?.perform(.restoreBackup) }
                .disabled(menu == nil)
            Divider()
            Button("Lock Vault") { model.lock(reason: .manual) }
                .keyboardShortcut("l", modifiers: [.command, .shift])
                .disabled(model.isLocked || !model.vaultExists)
        }
        CommandGroup(after: .textEditing) {
            Button("Find…") { menu?.perform(.find) }
                .keyboardShortcut("f")
                .disabled(menu == nil)
        }
        CommandGroup(before: .toolbar) {
            Toggle("Window Layout", isOn: Binding(get: { !compact }, set: { if $0 { compact = false } }))
                .keyboardShortcut("1")
            Toggle("Compact Layout", isOn: Binding(get: { compact }, set: { if $0 { compact = true } }))
                .keyboardShortcut("2")
            Divider()
            Toggle("Hide Codes", isOn: $hideCodes)
                .keyboardShortcut("h", modifiers: [.command, .shift])
            Divider()
        }
        CommandMenu("Account") {
            AccountActionsMenu(state: AccountMenuState(targets: menu?.selected ?? []),
                               folders: menu?.folders ?? [],
                               tags: menu?.tags ?? [],
                               inMenuBar: true) { menu?.perform($0) }
        }
        CommandGroup(replacing: .help) {
            Button("Tessera Help") {
                if let url = Self.helpURL { NSWorkspace.shared.open(url) }
            }
        }
    }
}
