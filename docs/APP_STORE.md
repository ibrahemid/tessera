# Shipping Tessera to the Mac App Store

Tessera is live as "Tessera 2FA Authenticator" (app id 6788814172). This is the
runbook for shipping an update. Everything below the one-time section repeats
per release.

## Prerequisites

1. **Full Xcode.** The Command Line Tools alone cannot build the app or submit
   (their SwiftPM is broken on this machine).
2. `brew install xcodegen`.

## One-time setup (done)

Kept as a record. None of it repeats per release.

- Apple Developer Program enrollment, Program License and Free Apps agreements.
- App ID `com.ibrahemid.tessera` in the Developer portal, with only the
  capabilities Tessera uses (App Sandbox is implicit).
- Apple Distribution certificate and Mac App Store provisioning profile, or
  "Automatically manage signing" with the team selected.
- The macOS app record in App Store Connect, bundle id `com.ibrahemid.tessera`.

## Shipping an update

1. Bump `MARKETING_VERSION` and `CURRENT_PROJECT_VERSION` in
   `swift/project.yml`. A build number App Store Connect has already seen is
   rejected at upload, so the build number must go up even for a rebuild of the
   same marketing version.
2. `cd swift && xcodegen generate` to regenerate `Tessera.xcodeproj` from
   `project.yml`.
3. Open `Tessera.xcodeproj`. Confirm the target's Team (`DEVELOPMENT_TEAM`) is
   set, `ENABLE_HARDENED_RUNTIME = YES`, and the entitlements file is attached.
4. Product > Archive, then Organizer > Distribute App > App Store Connect >
   Upload. Mac App Store apps are not notarized; Apple re-signs on approval.
5. In App Store Connect, create the new version, attach the build once
   processing finishes, fill "What's New", and submit for review.

## Build & test status (verified on Xcode 27)

- **`cd swift && swift test`**: 45 tests green, including `testArgon2idVector`
  (matches Go `x/crypto`) and `testFullVaultCrossDecrypt` (full Go to Swift
  envelope decrypt with real argon2id).
- **App build**: `xcodegen generate && xcodebuild ... build` succeeds; the app
  launches without crashing.

argon2id is the one primitive CryptoKit lacks. Rather than a fragile external
wrapper (Argon2Swift's SIMD `opt.c` fails to compile on Apple Silicon), Tessera
**vendors the PHC reference argon2** (portable `ref.c`, no SIMD, threads off) as
the `CArgon2` target, wrapped by `TesseraArgon2`. It matches Go's `x/crypto`
argon2id for the pinned params (m=131072 KiB, t=3, p=4), proven by the KAT.

## Where the pinned vectors live (CI proves cross-decrypt)

`spec/testvectors.json` (+ `spec/canonical_edge.json`) is the shared source of
truth. The Go suite, the swiftc verifier (`swift/Tools/verify`), and the XCTest
suite all run against it. `.github/workflows/ci.yml` runs all three on push,
proving Go and Swift cross-decrypt on every change.

## Entitlements

`swift/App/Resources/Tessera.entitlements`:
- `com.apple.security.app-sandbox`: required.
- `com.apple.security.files.user-selected.read-write`: open/save panels for
  backups, imports, and QR exports.
- `com.apple.security.files.bookmarks.app-scope`: persist a security-scoped
  bookmark to a vault file the user opens (a vault shared with the `tess` CLI),
  so the app reopens it on later launches without re-prompting.

Nothing else. Tessera makes no network requests; adding a network entitlement it
doesn't use invites rejection.

No camera entitlement (on-screen QR uses ScreenCaptureKit, which is TCC-gated at
runtime, not entitlement-gated). Local Keychain/Secure Enclave needs no
entitlement for a device-local app id.

## Privacy manifest

`swift/App/Resources/PrivacyInfo.xcprivacy` must declare every required-reason
API the app uses (UserDefaults, file timestamps). A missing declaration gets the
upload auto-rejected with ITMS-91053, so re-check it whenever the app touches a
new system API.

## App Privacy (nutrition label)

Declare **Data Not Collected**. Tessera keeps secrets on-device only. That stays
honest as long as there is no analytics/crash SDK and no Tessera-operated
server.

## Review notes / common rejections to avoid

- **Trademarks (5.2):** do not imply official affiliation with Google, Microsoft,
  or Steam. Describe Tessera as "works with any TOTP/2FA service." Don't
  keyword-stuff brand names in the App Store listing.
- **Permissions (5.1.1):** request only what you use.
- **Completeness (2.1):** no placeholder UI; provide a demo passphrase/flow if a
  reviewer needs to see a populated vault.

## Live listing copy

Transcript of what is on the store. Keep it in sync with App Store Connect
rather than editing it here for style.

> Tessera is a fast, private two-factor authenticator. Generate TOTP, HOTP, and
> Steam Guard codes, import from other apps or Google Authenticator, and keep
> everything in an encrypted vault. Unlock with Touch ID, and manage everything
> from a real command-line tool. Open source. No accounts, no tracking.

Keywords: authenticator, 2FA, TOTP, one-time password, OTP, two-factor, CLI.
