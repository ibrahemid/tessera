import SwiftUI
import UniformTypeIdentifiers

/// Outline and label shown while files are dragged over a drop target.
struct FileDropHighlight: View {
    var cornerRadius: CGFloat = 12

    var body: some View {
        RoundedRectangle(cornerRadius: cornerRadius)
            .stroke(Palette.accentFill, style: StrokeStyle(lineWidth: 2, dash: [6]))
            .background(Palette.accentWash.opacity(0.5), in: RoundedRectangle(cornerRadius: cornerRadius))
            .overlay {
                Text("Drop QR images or export files")
                    .font(Typo.label(12, .medium)).foregroundStyle(Palette.accentInk)
                    .padding(.horizontal, 12).padding(.vertical, 7)
                    .background(Palette.surface, in: Capsule())
                    .overlay(Capsule().stroke(Palette.border, lineWidth: 1))
            }
            .allowsHitTesting(false)
            .accessibilityElement(children: .combine)
    }
}

/// Imports QR images and export files dropped on the main window. Reading runs
/// off the main thread with progress shown at the bottom; a clean import ends in
/// the notice capsule, anything that failed opens the per-item report, and a
/// Tessera backup opens the restore sheet.
struct FileDropImport: ViewModifier {
    @EnvironmentObject var model: AppModel
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    /// False while another sheet covers the window.
    let isEnabled: Bool
    @State private var targeted = false
    @State private var reviewing = false
    @State private var reviewBackup: URL?
    @State private var restoreFile: URL?

    func body(content: Content) -> some View {
        content
            .onDrop(of: [.fileURL], isTargeted: $targeted) { providers in
                guard isEnabled, model.importProgress == nil else { return false }
                loadDroppedURLs(providers) { urls in Task { await importDropped(urls) } }
                return true
            }
            .overlay { if targeted && isEnabled { FileDropHighlight().padding(8) } }
            .overlay(alignment: .bottom) {
                if let p = model.importProgress, !reviewing { ImportProgressCapsule(progress: p).padding(.bottom, 16) }
            }
            .animation(reduceMotion ? nil : .easeOut(duration: 0.15), value: targeted)
            .sheet(isPresented: $reviewing, onDismiss: { reviewBackup = nil }) {
                AddAccountView(keepReport: true, pendingBackup: reviewBackup)
            }
            .sheet(isPresented: Binding(get: { restoreFile != nil }, set: { if !$0 { restoreFile = nil } })) {
                RestoreBackupView(file: restoreFile) { line in model.notice = .info(line) }
            }
    }

    private func importDropped(_ urls: [URL]) async {
        guard !urls.isEmpty else { return }
        let result = await model.importFiles(urls)
        if let e = model.errorMessage {
            model.errorMessage = nil
            model.notice = .failure(e)
            return
        }
        if let report = model.importReport, report.needsReview {
            reviewBackup = result.backup
            reviewing = true
            return
        }
        if let report = model.importReport, report.added == 0 {
            model.notice = .info(report.line)
        }
        if let backup = result.backup { restoreFile = backup }
    }
}

/// Files read so far, over the main window while a dropped import runs.
struct ImportProgressCapsule: View {
    let progress: ImportProgress

    var body: some View {
        HStack(spacing: 8) {
            ProgressView().controlSize(.small)
            Text(ImportProgressView.text(for: progress))
                .font(Typo.label(12, .medium)).foregroundStyle(Palette.textPrimary)
        }
        .padding(.horizontal, 14).padding(.vertical, 8)
        .background(Palette.surface, in: Capsule())
        .overlay(Capsule().stroke(Palette.border, lineWidth: 1))
        .shadow(color: .black.opacity(0.12), radius: 8, y: 2)
        .accessibilityElement(children: .combine)
    }
}

extension View {
    func fileDropImport(isEnabled: Bool) -> some View {
        modifier(FileDropImport(isEnabled: isEnabled))
    }
}
