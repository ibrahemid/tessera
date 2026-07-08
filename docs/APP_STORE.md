# Mac App Store submission — Tessera

This is the handoff for getting Tessera onto the Mac App Store. The build,
signing config, entitlements, and metadata text are prepared in-repo. The steps
that legally require your Apple ID and identity cannot be automated; they are
marked **YOU**.

## Prerequisites

1. **Install Xcode** (free, Mac App Store). The Command Line Tools alone cannot
   build the app or submit (their SwiftPM is broken on this machine).
2. `brew install xcodegen`, then `cd swift && xcodegen generate`.

## Build & test status (verified on Xcode 27)

These were the Xcode-gated steps; all now pass locally:

- **`cd swift && swift test`** — 8 interop tests green, including `testArgon2idVector` (matches Go `x/crypto`) and `testFullVaultCrossDecrypt` (full Go→Swift envelope decrypt with real argon2id).
- **App build** — `xcodegen generate && xcodebuild ... build` succeeds; the app launches without crashing.

argon2id is the one primitive CryptoKit lacks. Rather than a fragile external
wrapper (Argon2Swift's SIMD `opt.c` fails to compile on Apple Silicon), Tessera
**vendors the PHC reference argon2** (portable `ref.c`, no SIMD, threads off) as
the `CArgon2` target, wrapped by `TesseraArgon2`. It matches Go's `x/crypto`
argon2id for the pinned params (m=131072 KiB, t=3, p=4), proven by the KAT.

### What remains (genuinely gated on your Apple account)

```sh
cd swift
xcodegen generate          # if not already generated
open Tessera.xcodeproj      # set DEVELOPMENT_TEAM, then Product > Archive
# Xcode Organizer > Distribute App > App Store Connect > Upload
```

## Where the pinned vectors live (CI proves cross-decrypt)

`spec/testvectors.json` (+ `spec/canonical_edge.json`) is the shared source of
truth. The Go suite, the swiftc verifier (`swift/Tools/verify`), and the XCTest
suite all run against it. `.github/workflows/ci.yml` runs all three on push once a
GitHub remote exists, proving Go↔Swift cross-decrypt on every change.

## Apple setup (YOU — cannot be automated)

1. Enroll in / confirm the Apple Developer Program ($99/yr) and accept the
   current Program License + Paid/Free Apps agreements; complete tax/banking if
   charging (Tessera is free, so banking is optional).
2. In the Developer portal create an **App ID** `com.ibrahemid.tessera`. Enable
   only the capabilities Tessera uses (App Sandbox is implicit; add iCloud/
   CloudKit later when sync ships).
3. Generate an **Apple Distribution** certificate and a **Mac App Store**
   provisioning profile, or let Xcode "Automatically manage signing" with your
   team selected.
4. In **App Store Connect**, create a new macOS app record, bundle id
   `com.ibrahemid.tessera`.

## Build & upload

1. In Xcode, set the target's **Team** (DEVELOPMENT_TEAM) and confirm
   `ENABLE_HARDENED_RUNTIME = YES` and the entitlements file is attached.
2. Product → Archive → Distribute App → **App Store Connect** → Upload.
   - Mac App Store apps are **not** notarized; Apple re-signs on approval.
3. In App Store Connect, attach the build, fill metadata (below), submit.

## Entitlements (already configured)

`swift/App/Resources/Tessera.entitlements`:
- `com.apple.security.app-sandbox` — required.
- `com.apple.security.files.user-selected.read-write` — open/save panels for
  backups, imports, and QR exports.

Nothing else. Add `com.apple.security.network.client` only when CloudKit sync
ships (reviewers reject unused permissions).

No camera entitlement (on-screen QR uses ScreenCaptureKit, which is TCC-gated at
runtime, not entitlement-gated). Local Keychain/Secure Enclave needs no
entitlement for a device-local app id.

## App Privacy (nutrition label)

Declare **Data Not Collected**. Tessera keeps secrets on-device and, when sync is
enabled, in the user's own iCloud private database (end-to-end encrypted) which
Apple's definition does not count as "collected." This stays honest only as long
as there is no analytics/crash SDK and no Tessera-operated server.

## Review notes / common rejections to avoid

- **Trademarks (5.2):** do not imply official affiliation with Google, Microsoft,
  or Steam. Describe Tessera as "works with any TOTP/2FA service." Don't
  keyword-stuff brand names in the App Store listing.
- **Permissions (5.1.1):** request only what you use. If sync isn't in v1, drop
  the network entitlement.
- **Completeness (2.1):** no placeholder UI; provide a demo passphrase/flow if a
  reviewer needs to see a populated vault.

## Suggested listing copy

> Tessera is a fast, private two-factor authenticator. Generate TOTP, HOTP, and
> Steam Guard codes, import from other apps or Google Authenticator, and keep
> everything in an encrypted vault. Unlock with Touch ID, reach your codes from
> the menu bar, and — uniquely — manage everything from a real command-line tool.
> Open source. No accounts, no tracking.

Keywords: authenticator, 2FA, TOTP, one-time password, OTP, two-factor, menu bar.

## What is automated vs manual

- Automated (in repo / CI): build config, entitlements, Info.plist, signing
  settings scaffold, metadata text, and `xcodegen` project generation.
- Manual (YOU): Apple enrollment, certificates/profiles, App Store Connect
  record, screenshots/icon, privacy questionnaire answers, pricing, and the
  final Submit + review responses.
