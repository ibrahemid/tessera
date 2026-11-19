import SwiftUI
import UniformTypeIdentifiers
import TesseraCore

// MARK: Add account

struct AddAccountView: View {
    @EnvironmentObject var model: AppModel
    @Environment(\.dismiss) var dismiss
    @Environment(\.openURL) private var openURL
    @State private var input = ""
    @State private var showManual = false
    @State private var manualSecret = ""
    @State private var scanning = false
    @State private var dropTargeted = false
    @State private var restoreFile: URL?
    @FocusState private var inputFocused: Bool
    /// Opened to show the report of an import started elsewhere (a drop on the
    /// main window), so appearing keeps that report.
    private let keepReport: Bool
    /// A Tessera backup found by that import, offered for restore on appear.
    private let pendingBackup: URL?
    /// Classification and readout are derived once per edit, not per render:
    /// parsing a pasted migration blob or app export on every body evaluation
    /// was the most expensive thing this sheet did.
    @State private var kind: InputDetect.InputKind = .invalid
    @State private var readout: String?

    init(keepReport: Bool = false, pendingBackup: URL? = nil) {
        self.keepReport = keepReport
        self.pendingBackup = pendingBackup
    }

    #if DEBUG
    init(input: String, manual: Bool = false, keepReport: Bool = false) {
        _input = State(initialValue: input)
        _showManual = State(initialValue: manual)
        _manualSecret = State(initialValue: manual ? input : "")
        self.keepReport = keepReport
        self.pendingBackup = nil
    }
    #endif

    private var trimmed: String { input.trimmingCharacters(in: .whitespacesAndNewlines) }
    private var isSetupKey: Bool { kind == .setupKey }
    private var importing: Bool { model.importProgress != nil }

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            HStack { Text("Add accounts").font(Typo.display(18)).foregroundStyle(Palette.textPrimary); Spacer() }
            if showManual {
                ManualEntryForm(prefillSecret: manualSecret, onAdded: { dismiss() }, onCancel: { dismiss() })
                Button("Back to paste") { showManual = false; model.errorMessage = nil }
                    .buttonStyle(.plain).font(Typo.label(12)).foregroundStyle(Palette.textSecondary)
            } else {
                primaryField
                secondaryActions
                resultArea
                footer
            }
        }
        .padding(20).frame(width: 440).background(Palette.background)
        .background(CaptureShield())
        .onDrop(of: [.fileURL], isTargeted: $dropTargeted) { handleDrop($0) }
        .overlay { if dropTargeted { FileDropHighlight() } }
        .onAppear {
            model.errorMessage = nil; model.notice = nil
            if !keepReport { model.importReport = nil }
            if !showManual { inputFocused = true }
            if let pendingBackup { restoreFile = pendingBackup }
        }
        .onChange(of: input, initial: true) { _, text in reclassify(text) }
        .sheet(isPresented: Binding(get: { restoreFile != nil }, set: { if !$0 { restoreFile = nil } })) {
            RestoreBackupView(file: restoreFile) { line in
                model.notice = .info(line)
                if model.importReport?.hasDetail != true { dismiss() }
            }
        }
    }

    @ViewBuilder private var primaryField: some View {
        Text("Paste an otpauth link, an app or Google Authenticator export, or a setup key. One per line.")
            .font(Typo.label(12)).foregroundStyle(Palette.textSecondary)
            .fixedSize(horizontal: false, vertical: true)
        FieldBox {
            TextField("otpauth://…", text: $input, axis: .vertical).lineLimit(3...8)
                .autocorrectionDisabled()
                .focused($inputFocused)
        }
        if let r = readout {
            Text(r).font(Typo.label(11, .medium)).foregroundStyle(Palette.textSecondary)
        }
    }

    @ViewBuilder private var secondaryActions: some View {
        HStack(spacing: 16) {
            Button {
                scanning = true
                Task { defer { scanning = false }
                    do {
                        let payloads = try await QRCapture.scanScreen()
                        finish(model.importReporting(payloads.joined(separator: "\n")))
                    } catch { model.errorMessage = UserMessage.text(for: error, context: .scan) } }
            } label: {
                Label(scanning ? "Scanning…" : "Scan screen", systemImage: "qrcode.viewfinder").font(Typo.label(12, .medium))
            }.buttonStyle(.plain).foregroundStyle(Palette.accentInk).disabled(scanning || importing)
            Button {
                let urls = model.chooseImportFiles()
                Task { await importFiles(urls) }
            } label: {
                Label("Import from images or files", systemImage: "doc.text").font(Typo.label(12, .medium))
            }.buttonStyle(.plain).foregroundStyle(Palette.accentInk).disabled(scanning || importing)
        }
    }

    @ViewBuilder private var resultArea: some View {
        if let p = model.importProgress {
            ImportProgressView(progress: p)
        } else if let report = model.importReport, !report.isEmpty {
            ImportReportView(report: report)
        } else if let e = model.errorMessage {
            ErrorLine(e)
            if e == UserMessage.screenRecordingDenied {
                Button("Open Privacy Settings") { openURL(UserMessage.screenRecordingSettingsURL) }
                    .buttonStyle(.plain).font(Typo.label(12, .medium)).foregroundStyle(Palette.accentInk)
            }
        }
    }

    private var footer: some View {
        HStack {
            Spacer()
            Button("Cancel") { dismiss() }.buttonStyle(.plain).foregroundStyle(Palette.textSecondary)
                .keyboardShortcut(.cancelAction)
            ActionButton(isSetupKey ? "Continue" : "Add", enabled: !trimmed.isEmpty && !importing) { primaryAction() }
        }
    }

    private func primaryAction() {
        if isSetupKey {
            manualSecret = trimmed
            model.errorMessage = nil
            model.importReport = nil
            showManual = true
        } else {
            finish(model.importReporting(input))
        }
    }

    /// Dismiss on a clean success; stay so the per-item report stays visible when
    /// anything failed.
    private func finish(_ added: Bool) {
        if added, model.importReport?.hasDetail == false { dismiss() }
    }

    /// A Tessera backup among the files opens the restore sheet; otherwise the
    /// same rule as the paste path.
    private func importFiles(_ urls: [URL]) async {
        guard !urls.isEmpty else { return }
        let result = await model.importFiles(urls)
        if let backup = result.backup {
            restoreFile = backup
        } else {
            finish(result.added > 0)
        }
    }

    /// Classify the field once per edit and build the factual one-line readout of
    /// what it holds.
    private func reclassify(_ text: String) {
        let trimmed = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { kind = .invalid; readout = nil; return }
        kind = InputDetect.classify(text)
        switch kind {
        case .otpauth: readout = "otpauth link"
        case .migration:
            let n = (try? Migration.parse(text).count) ?? 0
            readout = n > 0 ? "Google Authenticator export (\(n) account\(n == 1 ? "" : "s"))"
                            : "Google Authenticator export"
        case .exportJSON, .exportCSV, .exportBinary:
            if let found = try? Importers.parse(Data(text.utf8)) {
                let n = found.accounts.count
                readout = "\(found.source) export (\(n) account\(n == 1 ? "" : "s"))"
            } else {
                readout = "App export"
            }
        case .setupKey: readout = "Setup key"
        case .invalid:
            readout = (trimmed.count >= 4 && !text.contains(where: \.isNewline)) ? "Not recognized" : nil
        }
    }

    private func handleDrop(_ providers: [NSItemProvider]) -> Bool {
        guard !importing else { return false }
        loadDroppedURLs(providers) { urls in
            Task { await importFiles(urls) }
        }
        return true
    }
}

/// Per-item outcome of a bulk import: a summary line plus a scrollable list of
/// anything that did not import (source and reason, never a full secret).
struct ImportReportView: View {
    let report: ImportSummary
    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            if report.needsReview { ErrorLine(report.line) } else { StatusLine(report.line) }
            if report.hasDetail {
                ScrollView {
                    VStack(alignment: .leading, spacing: 4) {
                        ForEach(report.failures) { f in
                            HStack(alignment: .firstTextBaseline, spacing: 6) {
                                Text(f.source).font(Typo.label(11, .medium)).foregroundStyle(Palette.textSecondary)
                                Text(f.display.isEmpty ? f.reason : "\(f.display): \(f.reason)")
                                    .font(Typo.label(11)).foregroundStyle(Palette.textSecondary)
                                    .lineLimit(2).truncationMode(.middle)
                                    .fixedSize(horizontal: false, vertical: true)
                                Spacer(minLength: 0)
                            }
                        }
                    }.frame(maxWidth: .infinity, alignment: .leading)
                }.frame(maxHeight: 120)
            }
        }
        .padding(.horizontal, 11).padding(.vertical, 9)
        .background(Palette.surfaceHi, in: RoundedRectangle(cornerRadius: 9))
        .overlay(RoundedRectangle(cornerRadius: 9).stroke(Palette.border, lineWidth: 1))
    }
}

/// Files read so far in a running import.
struct ImportProgressView: View {
    let progress: ImportProgress
    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            ProgressView(value: Double(progress.done), total: Double(max(progress.total, 1)))
            Text(Self.text(for: progress))
                .font(Typo.label(11)).foregroundStyle(Palette.textSecondary)
        }
        .accessibilityElement(children: .combine)
    }

    static func text(for progress: ImportProgress) -> String {
        progress.total == 1 ? "Reading 1 file…"
            : "Reading \(min(progress.done + 1, progress.total)) of \(progress.total) files…"
    }
}

struct ManualEntryForm: View {
    @EnvironmentObject var model: AppModel
    var prefillSecret: String = ""
    var onAdded: () -> Void
    var onCancel: (() -> Void)? = nil
    @FocusState private var issuerFocused: Bool
    @State private var type: OTPType = .totp
    @State private var issuer = ""
    @State private var account = ""
    @State private var secret = ""
    @State private var algorithm = "SHA1"
    @State private var digits = 6
    @State private var period = 30
    @State private var counter: Int64 = 0

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            Picker("Type", selection: $type) {
                Text("Time-based (TOTP)").tag(OTPType.totp)
                Text("Counter (HOTP)").tag(OTPType.hotp)
            }
            FieldBox { TextField("Issuer (e.g. GitHub)", text: $issuer).focused($issuerFocused) }
            FieldBox {
                TextField(text: $account, prompt: Text(verbatim: "Account (e.g. you@example.com)")) { Text("Account") }
                    .autocorrectionDisabled()
            }
            FieldBox { TextField("Setup key (base32 secret)", text: $secret).autocorrectionDisabled() }
            HStack(spacing: 10) {
                Picker("Algorithm", selection: $algorithm) {
                    ForEach(["SHA1", "SHA256", "SHA512"], id: \.self) { Text($0).tag($0) }
                }.fixedSize()
                Stepper("Digits: \(digits)", value: $digits, in: 6...8)
                if type == .hotp {
                    Stepper("Counter: \(counter)", value: $counter, in: 0...Int64.max)
                } else {
                    Stepper("Period: \(period)s", value: $period, in: 15...120, step: 15)
                }
            }.font(Typo.label(12)).foregroundStyle(Palette.textSecondary)
            if let e = model.errorMessage { ErrorLine(e) }
            HStack {
                Spacer()
                if let onCancel {
                    Button("Cancel", action: onCancel).buttonStyle(.plain).foregroundStyle(Palette.textSecondary)
                        .keyboardShortcut(.cancelAction)
                }
                ActionButton("Add", enabled: !secret.isEmpty && !(issuer.isEmpty && account.isEmpty)) {
                    if model.addManual(type: type, issuer: issuer, account: account, secretBase32: secret,
                                       algorithm: algorithm, digits: digits, period: period, counter: counter) {
                        onAdded()
                    }
                }
            }
        }
        .onAppear {
            if secret.isEmpty { secret = prefillSecret }
            issuerFocused = true
        }
    }
}
