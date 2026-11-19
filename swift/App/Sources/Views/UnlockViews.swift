import SwiftUI

// MARK: Root

struct RootView: View {
    @EnvironmentObject var model: AppModel
    @AppStorage(AppModel.compactPrefKey) private var compact = false
    var body: some View {
        ZStack {
            Palette.background.ignoresSafeArea()
            Group {
                if model.needsReset {
                    ResetPromptView()
                } else if model.needsRecovery {
                    RecoveryView()
                } else if model.vaultUnreachable {
                    VaultUnreachableView()
                } else if model.isOpening {
                    LaunchView()
                } else if model.needsPassphrase {
                    PassphraseUnlockView()
                } else if model.isLocked {
                    UnlockView()
                } else {
                    VaultView()
                }
            }
        }
        .tint(Palette.accent)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(WindowConfigurator(compact: compact))
        .task { await model.openOnLaunch() }
    }
}

// MARK: Launch / lock / reset

struct LaunchView: View {
    var body: some View {
        MosaicMark(side: 56)
            .frame(maxWidth: .infinity, maxHeight: .infinity)
    }
}

/// The one confirm dialog in front of the destructive reset, shared by every
/// surface that offers it. Its wording comes from the model, because reset means
/// two different things: it deletes the built-in vault, but only disconnects an
/// external file the tess CLI owns.
extension View {
    func resetTesseraDialog(isPresented: Binding<Bool>, model: AppModel, note: String? = nil) -> some View {
        confirmationDialog("Reset Tessera?", isPresented: isPresented, titleVisibility: .visible) {
            Button(model.resetActionTitle, role: .destructive) { model.resetTessera() }
            Button("Cancel", role: .cancel) {}
        } message: {
            Text([model.resetWarning, note].compactMap { $0 }.joined(separator: " "))
        }
    }
}

struct UnlockView: View {
    @EnvironmentObject var model: AppModel
    @State private var confirmReset = false
    var body: some View {
        VStack(spacing: 16) {
            Spacer()
            MosaicMark(side: 50)
            Text("Tessera is locked").font(Typo.display(20)).foregroundStyle(Palette.textPrimary)
            Button { model.unlock() } label: {
                Label(model.requireBiometrics ? "Unlock with Touch ID" : "Unlock",
                      systemImage: model.requireBiometrics ? "touchid" : "lock.open")
                    .font(Typo.label(13, .semibold)).foregroundStyle(Palette.onAccent)
                    .frame(maxWidth: 260).padding(.vertical, 10)
                    .background(Palette.accent, in: RoundedRectangle(cornerRadius: 10))
            }.buttonStyle(.plain)
            if let e = model.errorMessage { ErrorLine(e) }
            Button("Can't unlock? Reset Tessera") { confirmReset = true }
                .buttonStyle(.plain).font(Typo.label(12)).foregroundStyle(Palette.textSecondary)
            Spacer()
        }
        .padding(32)
        .resetTesseraDialog(isPresented: $confirmReset, model: model)
    }
}

/// Unlock for a vault whose key isn't on this Mac but that carries a passphrase
/// wrap — typically one created with the tess CLI.
struct PassphraseUnlockView: View {
    @EnvironmentObject var model: AppModel
    @State private var passphrase = ""
    @State private var unlocking = false
    @State private var confirmReset = false
    var body: some View {
        VStack(spacing: 16) {
            Spacer()
            MosaicMark(side: 50)
            Text(model.passphraseIsRecovery ? "Touch ID can't open this vault" : "This vault has a passphrase")
                .font(Typo.display(20)).foregroundStyle(Palette.textPrimary)
            Text(model.passphraseIsRecovery
                 ? "Its Secure Enclave key no longer works, which normally means the Mac's enrolled fingerprints changed. Enter the recovery passphrase to open it and set Touch ID up again."
                 : "It was created outside this app, for example with the tess CLI. After it opens once, Tessera unlocks it automatically.")
                .multilineTextAlignment(.center).font(Typo.label(13)).foregroundStyle(Palette.textSecondary)
                .frame(maxWidth: 360)
            FieldBox { SecureField("Vault passphrase", text: $passphrase).onSubmit(unlock) }
                .frame(maxWidth: 260)
            PrimaryButton(unlocking ? "Unlocking…" : "Unlock", enabled: !passphrase.isEmpty && !unlocking) { unlock() }
                .frame(maxWidth: 260)
            if let e = model.errorMessage { ErrorLine(e) }
            if model.passphraseIsRecovery {
                Button("Try Touch ID again") { model.retryUnlock() }
                    .buttonStyle(.plain).font(Typo.label(12)).foregroundStyle(Palette.accent)
            }
            Button("Can't unlock? Reset Tessera") { confirmReset = true }
                .buttonStyle(.plain).font(Typo.label(12)).foregroundStyle(Palette.textSecondary)
            Spacer()
        }
        .padding(32)
        .resetTesseraDialog(isPresented: $confirmReset, model: model)
    }

    private func unlock() {
        guard !passphrase.isEmpty, !unlocking else { return }
        unlocking = true
        Task {
            await model.unlockWithPassphrase(passphrase)
            unlocking = false
        }
    }
}

struct ResetPromptView: View {
    @EnvironmentObject var model: AppModel
    var body: some View {
        VStack(spacing: 16) {
            Spacer()
            MosaicMark(side: 50)
            Text("Can't open this vault").font(Typo.display(20)).foregroundStyle(Palette.textPrimary)
            Text("Its key isn't on this Mac, so the accounts can't be decrypted. Reset to start a new vault.")
                .multilineTextAlignment(.center).font(Typo.label(13)).foregroundStyle(Palette.textSecondary)
                .frame(maxWidth: 360)
            PrimaryButton("Reset Tessera") { model.resetTessera() }.frame(maxWidth: 260)
            if let e = model.errorMessage { ErrorLine(e) }
            Spacer()
        }
        .padding(32)
    }
}

/// The Secure Enclave key that opened this vault no longer works — almost always
/// because the Mac's enrolled fingerprints changed, which invalidates a
/// `.biometryCurrentSet` key. There is no passphrase wrap to fall back to, so
/// name the cause and the two real options instead of only offering a reset.
struct RecoveryView: View {
    @EnvironmentObject var model: AppModel
    @State private var confirmReset = false
    var body: some View {
        VStack(spacing: 16) {
            Spacer()
            MosaicMark(side: 50)
            Text("Touch ID can't open this vault").font(Typo.display(20)).foregroundStyle(Palette.textPrimary)
            Text("The vault key is tied to the fingerprints that were enrolled when it was set up. Adding or removing a fingerprint invalidates it, and this vault has no recovery passphrase.")
                .multilineTextAlignment(.center).font(Typo.label(13)).foregroundStyle(Palette.textSecondary)
                .frame(maxWidth: 380)
            PrimaryButton("Try Touch ID again") { model.retryUnlock() }.frame(maxWidth: 260)
            Button("Open another vault file…") { model.openExistingVault() }
                .buttonStyle(.plain).font(Typo.label(12)).foregroundStyle(Palette.accent)
            Button("Reset Tessera") { confirmReset = true }
                .buttonStyle(.plain).font(Typo.label(12)).foregroundStyle(Palette.textSecondary)
            if let e = model.errorMessage { ErrorLine(e) }
            Spacer()
        }
        .padding(32)
        .resetTesseraDialog(isPresented: $confirmReset, model: model)
    }
}

/// The external vault (a file shared with the tess CLI) is configured but can't
/// be read this launch. Never offers to delete it — only to relocate or detach.
struct VaultUnreachableView: View {
    @EnvironmentObject var model: AppModel
    var body: some View {
        VStack(spacing: 16) {
            Spacer()
            MosaicMark(side: 50)
            Text("Can't reach your vault").font(Typo.display(20)).foregroundStyle(Palette.textPrimary)
            Text("The vault file you opened isn't available. It may have moved, or the disk isn't mounted.")
                .multilineTextAlignment(.center).font(Typo.label(13)).foregroundStyle(Palette.textSecondary)
                .frame(maxWidth: 360)
            PrimaryButton("Locate vault…") { model.openExistingVault() }.frame(maxWidth: 260)
            Button("Use built-in vault") { model.useBuiltInVault() }
                .buttonStyle(.plain).font(Typo.label(12)).foregroundStyle(Palette.textSecondary)
            if let e = model.errorMessage { ErrorLine(e) }
            Spacer()
        }
        .padding(32)
    }
}
