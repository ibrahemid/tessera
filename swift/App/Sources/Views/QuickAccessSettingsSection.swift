import AppKit
import SwiftUI

struct QuickAccessSettingsSection: View {
    @ObservedObject var settings: QuickAccessSettings
    @State private var monitor: Any?

    var body: some View {
        Section {
            Toggle("Show in menu bar", isOn: Binding(
                get: { settings.showsInMenuBar },
                set: { settings.setShowsInMenuBar($0) }
            ))
            LabeledContent("Shortcut") {
                HStack(spacing: 8) {
                    if settings.shortcut != nil, !settings.isRecording {
                        Button("Clear") { settings.clearShortcut() }
                            .accessibilityLabel("Clear shortcut")
                    }
                    recorder
                }
            }
            VStack(alignment: .leading, spacing: 6) {
                Text(settings.isRecording
                     ? "Press the keys to use. Esc cancels."
                     : "Opens the menu bar list from any app.")
                    .font(Typo.label(11)).foregroundStyle(Palette.textSecondary)
                    .fixedSize(horizontal: false, vertical: true)
                if let issue = settings.issue { ErrorLine(issue.text) }
            }
        }
        .onChange(of: settings.isRecording, initial: true) { _, recording in
            if recording { startMonitor() } else { stopMonitor() }
        }
        .onReceive(NotificationCenter.default.publisher(for: NSApplication.didResignActiveNotification)) { _ in
            settings.cancelRecording()
        }
        .onDisappear {
            settings.cancelRecording()
            stopMonitor()
        }
    }

    private var recorder: some View {
        Button {
            if settings.isRecording { settings.cancelRecording() } else { settings.beginRecording() }
        } label: {
            Text(recorderTitle)
                .font(Typo.label(12, settings.shortcut == nil || settings.isRecording ? .regular : .medium))
                .foregroundStyle(settings.isRecording || settings.shortcut == nil
                                 ? Palette.textSecondary : Palette.textPrimary)
                .frame(minWidth: 120)
                .padding(.horizontal, 10).padding(.vertical, 4)
                .background(Palette.surfaceHi, in: RoundedRectangle(cornerRadius: 6))
                .overlay(RoundedRectangle(cornerRadius: 6)
                    .stroke(settings.isRecording ? Palette.accent : Palette.border, lineWidth: 1))
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityLabel("Shortcut")
        .accessibilityValue(accessibilityValue)
        .accessibilityHint(settings.isRecording ? "Press the keys to use" : "Records a new shortcut")
    }

    private var recorderTitle: String {
        if settings.isRecording { return "Type shortcut" }
        return settings.shortcut?.display ?? "Record Shortcut"
    }

    private var accessibilityValue: String {
        if settings.isRecording { return "Recording" }
        return settings.shortcut?.accessibilityName ?? "None"
    }

    private func startMonitor() {
        guard monitor == nil else { return }
        let settings = settings
        monitor = NSEvent.addLocalMonitorForEvents(matching: .keyDown) { event in
            let key = RecordedKey(event)
            let handled = MainActor.assumeIsolated { settings.handle(key) }
            return handled ? nil : event
        }
    }

    private func stopMonitor() {
        if let monitor { NSEvent.removeMonitor(monitor) }
        monitor = nil
    }
}

struct GeneralTabHeight: ViewModifier {
    @ObservedObject var settings: QuickAccessSettings
    let base: CGFloat

    func body(content: Content) -> some View {
        content.frame(height: base + (settings.issue == nil ? 0 : SettingsTabHeight.infoLine))
    }
}

struct CodeDisplaySettingsSection: View {
    @AppStorage(AppModel.hideCodesPrefKey) private var hideCodes = false

    var body: some View {
        Section {
            VStack(alignment: .leading, spacing: 6) {
                Toggle("Hide codes", isOn: $hideCodes)
                Text("Codes show when you hover over or select an account.")
                    .font(Typo.label(11)).foregroundStyle(Palette.textSecondary)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
    }
}
