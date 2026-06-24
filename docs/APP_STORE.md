# Mac App Store submission — Tessera

This is the handoff for getting Tessera onto the Mac App Store. The build,
signing config, entitlements, and metadata text are prepared in-repo. The steps
that legally require your Apple ID and identity cannot be automated; they are
marked **YOU**.

## Prerequisites

1. **Install Xcode** (free, Mac App Store). The Command Line Tools alone cannot
   build the app or submit (their SwiftPM is broken on this machine).
2. `brew install xcodegen`, then `cd swift && xcodegen generate`.

## The three Xcode-gated steps (in order)

Each is genuinely gated on Xcode/hardware or your Apple account — not deferred to save effort:

1. **`swift test`** (gated: full Xcode's SwiftPM; the Command Line Tools' SwiftPM is broken here). Validates the Argon2Swift binding + the full Go→Swift passphrase cross-decrypt against the pinned vectors.
2. **Build/sign the `.app`** (gated: Xcode + your signing identity). SwiftUI/MenuBarExtra/Secure Enclave/ScreenCaptureKit need a real build.
3. **App Store submission** (gated: your Apple ID + App Store Connect).

### Exact commands

```sh
brew install xcodegen
cd swift
swift test                 # step 1 — expect all InteropTests green
xcodegen generate          # step 2 — creates Tessera.xcodeproj
open Tessera.xcodeproj      # set Team, then Product > Archive
# step 3: Xcode Organizer > Distribute App > App Store Connect > Upload
```

### Most-likely fix: the Argon2Swift call

argon2id is the one primitive CryptoKit lacks, so it's the single thing most
likely to need a tweak on first `swift test`. The call lives in
`swift/App/Sources/Argon2Provider.swift` and `swift/Tests/.../InteropTests.swift`.
Verified shape:

```swift
Argon2Swift.hashPasswordBytes(
    password: passphrase,          // Data — param is `password:`, not `bytes:`
    salt: Salt(bytes: salt),       // fixed salt for vectors, NOT Salt.newSalt()
    iterations: 3, memory: 131072, // memory is m_cost in KiB → 128 MiB = 131072, NOT 128
    parallelism: 4, length: 32,
    type: .id, version: .V13
).hashData()                       // raw 32-byte key
```

Units are already consistent across Go (`argon2.IDKey(..., m=131072, ...)`), the
spec, and Swift. The pinned **argon2id KAT** (`testvectors.json` → `argon2id`)
asserts the derived 32-byte DEK byte-for-byte, so `testArgon2idVector` fails fast
and isolated if the binding or units are wrong — before the full vault decrypt.

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
- `com.apple.security.files.user-selected.read-write` + `…files.bookmarks.app-scope`
  — so the app can open the CLI-shared vault folder via a security-scoped
  bookmark instead of relocating the vault.
- `com.apple.security.network.client` — for CloudKit sync (fast-follow). Remove
  it for the first release if sync isn't shipping, to keep the permission set
  minimal (reviewers reject unused permissions).

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
