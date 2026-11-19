import SwiftUI
import AppKit
import TesseraCore
import os

extension Account: @retroactive Identifiable {}

// MARK: Brand

struct Wordmark: View {
    var size: CGFloat = 17
    var body: some View {
        HStack(spacing: 7) {
            MosaicMark(side: size + 3)
            Text("Tessera").font(Typo.display(size)).foregroundStyle(Palette.textPrimary)
        }
    }
}

struct MosaicMark: View {
    var side: CGFloat = 20
    private var gap: CGFloat { side * 0.14 }
    private var cell: CGFloat { (side - gap) / 2 }
    private func tile(_ color: Color) -> some View {
        RoundedRectangle(cornerRadius: cell * 0.28).fill(color).frame(width: cell, height: cell)
    }
    var body: some View {
        VStack(spacing: gap) {
            HStack(spacing: gap) { tile(Palette.accent); tile(Palette.textSecondary.opacity(0.45)) }
            HStack(spacing: gap) { tile(Palette.textSecondary.opacity(0.45)); tile(Palette.textSecondary.opacity(0.25)) }
        }
        .frame(width: side, height: side)
    }
}

/// Resolve dropped file URLs from item providers, then call `completion` on the
/// main actor once every provider has loaded.
func loadDroppedURLs(_ providers: [NSItemProvider], completion: @escaping @MainActor ([URL]) -> Void) {
    let group = DispatchGroup()
    let urls = OSAllocatedUnfairLock(initialState: [URL]())
    for p in providers where p.canLoadObject(ofClass: URL.self) {
        group.enter()
        _ = p.loadObject(ofClass: URL.self) { url, _ in
            if let url { urls.withLock { $0.append(url) } }
            group.leave()
        }
    }
    group.notify(queue: .main) {
        let resolved = urls.withLock { $0 }
        Task { @MainActor in completion(resolved) }
    }
}

/// Excludes the hosting window from screen sharing and capture. Sheets are
/// separate NSWindows, so the main window's sharingType does not cover them.
struct CaptureShield: NSViewRepresentable {
    func makeNSView(context: Context) -> NSView {
        let view = NSView()
        DispatchQueue.main.async { [weak view] in view?.window?.sharingType = .none }
        return view
    }
    func updateNSView(_ nsView: NSView, context: Context) {
        nsView.window?.sharingType = .none
    }
}

// MARK: Shared building blocks

struct FieldBox<Content: View>: View {
    @ViewBuilder var content: Content
    var body: some View {
        content
            .textFieldStyle(.plain).font(Typo.label(13))
            .padding(.horizontal, 11).padding(.vertical, 9)
            .background(Palette.surfaceHi, in: RoundedRectangle(cornerRadius: 9))
            .overlay(RoundedRectangle(cornerRadius: 9).stroke(Palette.border, lineWidth: 1))
    }
}

/// Native prominent action button — the default (Return) action in a dialog.
/// Replaces the custom fixed-size gold pill, which rendered cramped.
struct ActionButton: View {
    let title: String; var enabled: Bool; var action: () -> Void
    init(_ title: String, enabled: Bool = true, action: @escaping () -> Void) {
        self.title = title; self.enabled = enabled; self.action = action
    }
    var body: some View {
        Button(title, action: action)
            .buttonStyle(.borderedProminent)
            .tint(Palette.accentFill)
            .keyboardShortcut(.defaultAction)
            .disabled(!enabled)
    }
}

struct PrimaryButton: View {
    let title: String; var enabled: Bool; var action: () -> Void
    init(_ title: String, enabled: Bool = true, action: @escaping () -> Void) {
        self.title = title; self.enabled = enabled; self.action = action
    }
    var body: some View {
        Button(action: action) {
            Text(title).font(Typo.label(13, .semibold)).foregroundStyle(Palette.onAccent)
                .frame(maxWidth: .infinity).padding(.vertical, 9)
                .background(Palette.accentFill, in: RoundedRectangle(cornerRadius: 9))
        }
        .buttonStyle(.plain)
        .opacity(enabled ? 1 : 0.45)   // dim the whole control; never blend the fill toward the bg
        .disabled(!enabled)
    }
}

/// A Settings row action carrying a subtitle that spells out what it does — used
/// to separate switching the active vault from restoring a backup, two flows that
/// otherwise look identical.
struct VaultActionButton: View {
    let title: String
    let subtitle: String
    let action: () -> Void
    var body: some View {
        Button(action: action) {
            VStack(alignment: .leading, spacing: 2) {
                Text(title).font(Typo.label(13)).foregroundStyle(Palette.accentInk)
                Text(subtitle).font(Typo.label(11)).foregroundStyle(Palette.textSecondary)
                    .fixedSize(horizontal: false, vertical: true)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
    }
}

/// Segmented-style filter chip used in the compact filter row.
struct FilterPill: View {
    let title: String
    let selected: Bool
    let action: () -> Void
    var body: some View {
        Button(action: action) {
            Text(title)
                .font(Typo.label(12.5, .medium))
                .foregroundStyle(selected ? Palette.accentInk : Palette.textSecondary)
                .padding(.horizontal, 12).padding(.vertical, 5)
                .background(selected ? Palette.accentWash : Color.clear,
                            in: RoundedRectangle(cornerRadius: 7))
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
    }
}

struct SectionLabel: View {
    let text: String
    init(_ text: String) { self.text = text }
    var body: some View {
        HStack {
            Text(text.uppercased()).font(Typo.label(10, .semibold)).tracking(0.6).foregroundStyle(Palette.textSecondary)
            Spacer()
        }.padding(.horizontal, 4).padding(.top, 2)
    }
}

struct ErrorLine: View {
    let text: String
    init(_ text: String) { self.text = text }
    var body: some View {
        Label(text, systemImage: "exclamationmark.triangle.fill").font(Typo.label(11)).foregroundStyle(Palette.warning)
    }
}

struct StatusLine: View {
    let text: String
    init(_ text: String) { self.text = text }
    var body: some View {
        Label(text, systemImage: "checkmark.circle.fill").font(Typo.label(11)).foregroundStyle(Palette.accentInk)
    }
}

struct NoticeLine: View {
    let notice: AppModel.Notice
    init(_ notice: AppModel.Notice) { self.notice = notice }
    var body: some View {
        switch notice {
        case .info(let text): StatusLine(text)
        case .failure(let text): ErrorLine(text)
        }
    }
}

struct EmptyState: View {
    var add: () -> Void
    var openExisting: (() -> Void)? = nil
    var body: some View {
        VStack(spacing: 12) {
            MosaicMark(side: 44)
            Text("No accounts yet").font(Typo.display(16)).foregroundStyle(Palette.textPrimary)
            Text("Add your first account from an otpauth link or a QR code on screen.")
                .multilineTextAlignment(.center).font(Typo.label(12)).foregroundStyle(Palette.textSecondary).frame(maxWidth: 320)
            Button(action: add) {
                Label("Add account", systemImage: "plus").font(Typo.label(12, .semibold)).foregroundStyle(Palette.accentInk)
            }.buttonStyle(.plain).padding(.top, 2)
            if let openExisting {
                Button(action: openExisting) {
                    Text("Use a vault created by the tess CLI").font(Typo.label(12)).foregroundStyle(Palette.textSecondary)
                }.buttonStyle(.plain)
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }
}

// MARK: Window chrome

/// Reaches the hosting NSWindow to give Tessera one seamless titlebar — the
/// content background runs continuously under the traffic lights instead of
/// leaving a detached strip — and narrows the window when Compact is on.
///
/// Xcode-gated: titlebar appearance and window resizing only render under a real
/// AppKit window, so this is verified by building the app, not in a preview.
struct WindowConfigurator: NSViewRepresentable {
    var compact: Bool

    // Tracks the last density we resized for, so resizing only happens on an
    // actual density change and never fights the user's manual resize when
    // updateNSView runs for some unrelated state change.
    final class Coordinator { var lastCompact: Bool?; var titlebarDone = false }
    func makeCoordinator() -> Coordinator { Coordinator() }

    func makeNSView(context: Context) -> NSView {
        // Seed with the default (window) density: a window-mode launch respects
        // the restored frame, while a compact-persisted launch resizes once to
        // the compact width instead of opening wide.
        context.coordinator.lastCompact = false
        let view = NSView()
        let coordinator = context.coordinator
        DispatchQueue.main.async { [weak view] in
            coordinator.titlebarDone = applyTitlebar(view?.window)
        }
        return view
    }

    func updateNSView(_ nsView: NSView, context: Context) {
        let changed = context.coordinator.lastCompact != compact
        context.coordinator.lastCompact = compact
        // A no-op once the titlebar is styled and the density is unchanged.
        guard changed || !context.coordinator.titlebarDone else { return }
        let coordinator = context.coordinator
        DispatchQueue.main.async { [weak nsView] in
            if !coordinator.titlebarDone {
                coordinator.titlebarDone = applyTitlebar(nsView?.window)
            }
            if changed { resize(nsView?.window) }
        }
    }

    @discardableResult
    private func applyTitlebar(_ window: NSWindow?) -> Bool {
        guard let window else { return false }
        window.titlebarAppearsTransparent = true
        window.titleVisibility = .hidden
        // Exclude the window from screen sharing and recording: live codes and QR
        // sheets must not travel through a call or a screen capture. Standard for
        // an authenticator, and there is no reason to make it optional.
        window.sharingType = .none
        #if DEBUG
        // The App Store frames are real window captures, which the line above
        // blocks outright. TESSERA_ALLOW_CAPTURE=1 lifts the shield for that one
        // run against a throwaway vault; DEBUG-only, so no shipping binary can
        // reach it. See docs/APP_STORE.md, Screenshots.
        if ProcessInfo.processInfo.environment["TESSERA_ALLOW_CAPTURE"] == "1" {
            window.sharingType = .readOnly
        }
        #endif
        return true
    }

    /// Snap to the mode's nominal width on a density toggle. Manual sizing
    /// between toggles is left untouched.
    private func resize(_ window: NSWindow?) {
        guard let window else { return }
        let target: CGFloat = compact ? 480 : 920
        guard abs(window.frame.width - target) > 1 else { return }
        var frame = window.frame
        let delta = frame.width - target
        frame.origin.x += delta / 2          // keep the window visually centered
        frame.size.width = target
        window.setFrame(frame, display: true, animate: true)
    }
}
