import SwiftUI
import TesseraCore

// MARK: Edit account

/// Edit an account's names, tags and code parameters. A change to the type or
/// the secret asks for confirmation first, since the codes change with it. The
/// alias keeps its charset + vault-uniqueness rules, with a factual inline reason
/// on reject.
struct EditAccountView: View {
    @EnvironmentObject var model: AppModel
    @Environment(\.dismiss) var dismiss
    let account: Account
    @State private var issuer: String
    @State private var accountLabel: String
    @State private var alias: String
    @State private var folder: String
    @State private var tags: [String]
    @State private var tagInput = ""
    @State private var draft: ParameterDraft
    @State private var showsAdvanced: Bool
    @State private var confirmingCodes = false
    @State private var error: String?
    @FocusState private var issuerFocused: Bool

    init(account: Account) {
        self.account = account
        _issuer = State(initialValue: account.issuer)
        _accountLabel = State(initialValue: account.account)
        _alias = State(initialValue: account.handle)
        _folder = State(initialValue: account.folder)
        _tags = State(initialValue: account.tags)
        _draft = State(initialValue: ParameterDraft(account))
        _showsAdvanced = State(initialValue: false)
    }

    #if DEBUG
    init(account: Account, draft: ParameterDraft, tagInput: String = "", showsAdvanced: Bool = true) {
        self.init(account: account)
        _draft = State(initialValue: draft)
        _tagInput = State(initialValue: tagInput)
        _showsAdvanced = State(initialValue: showsAdvanced)
    }
    #endif

    private var canSave: Bool {
        !alias.trimmingCharacters(in: .whitespaces).isEmpty
            && !(issuer.trimmingCharacters(in: .whitespaces).isEmpty
            && accountLabel.trimmingCharacters(in: .whitespaces).isEmpty)
            && secretInput != .invalid
    }

    private var secretInput: SecretInput { ParameterEdit.secretInput(draft.secretText) }

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("Edit account").font(Typo.display(16)).foregroundStyle(Palette.textPrimary)
                .frame(maxWidth: .infinity)

            labeledField("Issuer") {
                TextField("Issuer", text: $issuer)
                    .textFieldStyle(.plain).autocorrectionDisabled()
                    .focused($issuerFocused)
                    .onChange(of: issuer) { error = nil }
            }
            labeledField("Account") {
                TextField("Account", text: $accountLabel)
                    .textFieldStyle(.plain).autocorrectionDisabled()
                    .onChange(of: accountLabel) { error = nil }
            }
            labeledField("Alias") {
                TextField("Alias", text: $alias)
                    .font(Typo.code(13)).textFieldStyle(.plain).autocorrectionDisabled()
                    .onChange(of: alias) { error = nil }
            }
            // Every account carries an alias: the vault format lets the field be
            // absent, but both cores assign one at unlock, so clearing it here
            // would only produce a different auto-assigned alias.
            if alias.trimmingCharacters(in: .whitespaces).isEmpty {
                Text("An alias is required. Type one to save.")
                    .font(Typo.label(11)).foregroundStyle(Palette.textSecondary)
            }
            labeledField("Folder") {
                TextField("Folder", text: $folder)
                    .textFieldStyle(.plain).autocorrectionDisabled()
                    .onChange(of: folder) { error = nil }
            }
            tagsField

            DisclosureGroup(isExpanded: $showsAdvanced) {
                advanced.padding(.top, 8)
            } label: {
                HStack {
                    Text("Advanced").font(Typo.label(12, .medium)).foregroundStyle(Palette.textPrimary)
                    Spacer()
                    if !showsAdvanced {
                        Text(paramSummary(draft)).font(Typo.label(11)).foregroundStyle(Palette.textSecondary)
                    }
                }
                .contentShape(Rectangle())
                .onTapGesture { showsAdvanced.toggle() }
            }

            if ParameterEdit.changesCodes(account, draft: draft), secretInput != .invalid {
                Label("Codes will change after saving.", systemImage: "exclamationmark.circle")
                    .font(Typo.label(11)).foregroundStyle(Palette.textSecondary)
            }

            if let e = error { ErrorLine(e) }

            HStack {
                Button("Cancel") { dismiss() }.buttonStyle(.plain).foregroundStyle(Palette.textSecondary)
                    .keyboardShortcut(.cancelAction)
                Spacer()
                ActionButton("Save", enabled: canSave) { attemptSave() }
            }.font(Typo.label(12, .medium))
        }
        .padding(24).frame(width: 360).background(Palette.background)
        .background(CaptureShield())
        .onAppear { issuerFocused = true }
        .confirmationDialog(ParameterEdit.confirmationTitle(for: account), isPresented: $confirmingCodes,
                            titleVisibility: .visible) {
            Button("Change Codes", role: .destructive) { save() }
            Button("Cancel", role: .cancel) {}
        } message: {
            Text(ParameterEdit.confirmationMessage)
        }
    }

    // MARK: Tags

    private var tagsField: some View {
        VStack(alignment: .leading, spacing: 6) {
            Text("Tags").font(Typo.label(10, .medium)).foregroundStyle(Palette.textSecondary)
            if !tags.isEmpty {
                ChipFlow(spacing: 6) {
                    ForEach(tags, id: \.self) { tag in
                        TagChip(tag: tag) {
                            tags.removeAll { $0 == tag }
                            error = nil
                        }
                    }
                }
            }
            HStack(spacing: 8) {
                FieldBox {
                    TextField("Add a tag", text: $tagInput)
                        .autocorrectionDisabled()
                        .onSubmit(addTag)
                        .onChange(of: tagInput) { error = nil }
                }
                Button("Add", action: addTag)
                    .disabled(VaultEdit.normalizedTag(tagInput) == nil)
                    .accessibilityLabel("Add tag")
            }
        }
    }

    private func addTag() {
        tags = TagDraft.adding(tagInput, to: tags)
        tagInput = ""
    }

    // MARK: Advanced

    private var advanced: some View {
        VStack(alignment: .leading, spacing: 12) {
            Grid(alignment: .leading, horizontalSpacing: 12, verticalSpacing: 10) {
                GridRow {
                    Text("Type")
                    Picker("Type", selection: Binding(get: { draft.type }, set: { draft.setType($0) })) {
                        Text("Time-based (TOTP)").tag(OTPType.totp)
                        Text("Counter (HOTP)").tag(OTPType.hotp)
                        Text("Steam").tag(OTPType.steam)
                    }
                    .labelsHidden().fixedSize()
                }
                if draft.type != .steam {
                    GridRow {
                        Text("Algorithm")
                        Picker("Algorithm", selection: $draft.algorithm) {
                            ForEach(ParameterDraft.algorithms, id: \.self) { Text($0).tag($0) }
                        }
                        .labelsHidden().fixedSize()
                    }
                    GridRow {
                        Text("Digits")
                        HStack(spacing: 6) {
                            Text("\(draft.digits)").foregroundStyle(Palette.textPrimary).monospacedDigit()
                            Stepper("Digits", value: $draft.digits, in: 6...8)
                                .labelsHidden()
                                .accessibilityValue("\(draft.digits)")
                        }
                    }
                    if draft.type == .hotp {
                        GridRow {
                            Text("Counter")
                            numberField("Counter", value: $draft.counter)
                        }
                    } else {
                        GridRow {
                            Text("Period")
                            HStack(spacing: 8) {
                                numberField("Period in seconds", value:Binding(get: { Int64(draft.period) },
                                                                     set: { draft.period = Int(clamping: $0) }))
                                Text("seconds")
                            }
                        }
                    }
                }
            }
            labeledField("Replace setup key") {
                TextField("Setup key or otpauth link", text: $draft.secretText)
                    .font(Typo.code(13)).textFieldStyle(.plain).autocorrectionDisabled()
                    .onChange(of: draft.secretText) {
                        error = nil
                        if case .link(let link) = secretInput { draft.adopt(link) }
                    }
            }
            if secretInput == .invalid {
                ErrorLine(ParameterEditError.invalidSecret.text)
            }
        }
        .font(Typo.label(12)).foregroundStyle(Palette.textSecondary)
    }

    private func numberField(_ label: String, value: Binding<Int64>) -> some View {
        FieldBox {
            TextField(label, value: value, format: .number.grouping(.never))
                .labelsHidden()
                .onChange(of: value.wrappedValue) { error = nil }
        }
        .frame(width: 110)
    }

    // MARK: Save

    private func attemptSave() {
        if ParameterEdit.needsConfirmation(account, draft: draft) { confirmingCodes = true } else { save() }
    }

    private func save() {
        let pending = TagDraft.adding(tagInput, to: tags)
        do {
            _ = try ParameterEdit.updated(account, with: draft)
        } catch let e as ParameterEditError {
            error = e.text
            return
        } catch let e {
            error = UserMessage.text(for: e, context: .save)
            return
        }
        let names = (TextCleaning.clean(issuer), TextCleaning.clean(accountLabel),
                     TextCleaning.clean(alias), TextCleaning.clean(folder))
        if names != (account.issuer, account.account, account.handle, account.folder) {
            if let reason = model.applyEdit(account, issuer: names.0, accountLabel: names.1,
                                            handle: names.2, folder: names.3) {
                error = reason
                return
            }
        }
        if let reason = model.applyDetails(account, tags: pending, parameters: draft) {
            error = reason
            return
        }
        dismiss()
    }

    @ViewBuilder private func labeledField<Content: View>(_ label: String,
                                                          @ViewBuilder _ field: () -> Content) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(label).font(Typo.label(10, .medium)).foregroundStyle(Palette.textSecondary)
            FieldBox { field() }
        }
    }

    /// The current code parameters, e.g. "TOTP · SHA1 · 6 digits · 30s".
    private func paramSummary(_ d: ParameterDraft) -> String {
        var parts = [d.type.rawValue.uppercased(), d.algorithm, "\(d.digits) digits"]
        parts.append(d.type == .hotp ? "counter \(d.counter)" : "\(d.period)s")
        return parts.joined(separator: " · ")
    }
}

private struct TagChip: View {
    let tag: String
    let remove: () -> Void

    var body: some View {
        HStack(spacing: 4) {
            Text(tag).font(Typo.label(11.5, .medium)).foregroundStyle(Palette.accentInk)
            Button(action: remove) {
                Image(systemName: "xmark").font(Typo.label(8, .bold)).foregroundStyle(Palette.textSecondary)
                    .frame(width: 24, height: 24)
                    .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .accessibilityLabel("Remove tag \(tag)")
            .padding(.vertical, -10)
            .padding(.trailing, -8)
        }
        .padding(.horizontal, 9).padding(.vertical, 4)
        .background(Palette.accentWash, in: Capsule())
        .accessibilityElement(children: .contain)
    }
}

private struct ChipFlow: Layout {
    var spacing: CGFloat

    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        let rows = arrange(subviews, width: proposal.width ?? .infinity)
        let height = rows.last.map { $0.y + $0.height } ?? 0
        let width = rows.map(\.width).max() ?? 0
        return CGSize(width: proposal.width ?? width, height: height)
    }

    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        for row in arrange(subviews, width: bounds.width) {
            var x = bounds.minX
            for i in row.items {
                let size = subviews[i].sizeThatFits(.unspecified)
                subviews[i].place(at: CGPoint(x: x, y: bounds.minY + row.y), proposal: ProposedViewSize(size))
                x += size.width + spacing
            }
        }
    }

    private struct Row {
        var items: [Int] = []
        var y: CGFloat = 0
        var width: CGFloat = 0
        var height: CGFloat = 0
    }

    private func arrange(_ subviews: Subviews, width: CGFloat) -> [Row] {
        var rows: [Row] = []
        var row = Row()
        for (i, subview) in subviews.enumerated() {
            let size = subview.sizeThatFits(.unspecified)
            if !row.items.isEmpty, row.width + spacing + size.width > width {
                rows.append(row)
                row = Row(y: row.y + row.height + spacing)
            }
            row.width += (row.items.isEmpty ? 0 : spacing) + size.width
            row.height = max(row.height, size.height)
            row.items.append(i)
        }
        if !row.items.isEmpty { rows.append(row) }
        return rows
    }
}
