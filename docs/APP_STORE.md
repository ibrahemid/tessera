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

- **`cd swift && swift test`**: 57 tests green, including `testArgon2idVector`
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

## Listing metadata (1.0.3)

Paste-ready, and the place to edit the wording. App Store Connect is a copy of
this, not the other way round.

Every claim here is checkable against the repo. Two that were wrong in the 1.0.0
listing and are removed for good:

- **No menu bar.** The app has no `NSStatusItem` and no `MenuBarExtra`. Menu-bar
  quick access is a post-v1 idea (see the comment in `TesseraApp.swift`). Nothing
  in the listing, the review notes, or the screenshots may mention it until it
  ships.
- **The CLI is not inside the app bundle.** `Tessera.app` contains one binary,
  the app. `tess` is a separate free download (Homebrew, curl, `go install`), so
  the listing says so instead of "the included tess command-line tool".

**App name (≤30):** `Tessera 2FA Authenticator`

**Subtitle (≤30):** `2FA for your Mac and CLI`

**Keywords (≤100, no space after the commas):**

```
totp,hotp,steam guard,terminal,open source,touch id,offline,no account
```

Words already carried by the name and subtitle (authenticator, 2FA, Mac, CLI)
are left out; Apple indexes those fields too, so repeating them wastes the
budget. `steam guard` names a code type the app generates, which reads as a
capability rather than an affiliation, but it is the first keyword to drop if
review ever raises 5.2.

**Promotional text (≤170, editable without a review):**

```
TOTP, HOTP, and Steam Guard codes in the app and in your terminal, from one
encrypted vault.
```

**Description:**

```
Tessera is the only open-source authenticator on the Mac App Store with a
command line. The app and the tess CLI read the same encrypted vault file.

Generate the codes you already use: TOTP, HOTP, and Steam Guard. Add an account
by scanning a QR code on screen, pasting a setup link or a setup key, or
importing a Google Authenticator transfer or an Aegis, 2FAS, or Raivo export.

Unlock with Touch ID. Search, pin the ones you use most, and group them into
folders. Click a row to copy its code.

• Secrets are encrypted on your Mac with argon2id and XChaCha20-Poly1305. Where
  there is a Secure Enclave, the key is wrapped inside it.
• No account and no servers. The app ships without a network entitlement, so it
  cannot reach the network.
• Apache-2.0. The source, the vault spec, and the test vectors are public at
  github.com/ibrahemid/tessera

The vault format is a published spec with two implementations, one in Go and one
in Swift, cross-decrypted against shared test vectors on every commit.

tess, the command-line tool, is a separate free download (Homebrew, curl, or go
install; see tessera.ibrahemid.com). It adds a live watch view with countdown
bars, JSON output, and shell completions.

Works with any service that supports standard two-factor authentication. Tessera
is not affiliated with Google, Microsoft, Steam, or any other provider.
```

**What's New (1.0.3):**

```
No app changes. This version updates the description and screenshots so they
match what the app does.
```

`git log v1.0.2..HEAD` is a single commit and it touches `go/` only, so nothing
user-facing changed in the app since the shipped 1.0.2 build. 1.0.3 is a listing
update, and `swift/project.yml` still reads `MARKETING_VERSION 1.0.2` /
`CURRENT_PROJECT_VERSION 7`. If App Store Connect refuses a new version without a
build, rebuild the same source with `CURRENT_PROJECT_VERSION` raised and
`MARKETING_VERSION` set to 1.0.3, and leave the release note as it stands.

**Review notes (paste):**

```
Tessera is an offline TOTP authenticator; no login is required. To test: open
the app, click the + button in the toolbar (or press Command-N), and paste
this link:
otpauth://totp/Demo:tester?secret=JBSWY3DPEHPK3PXP&issuer=Demo
A 6-digit code appears with a 30-second countdown ring. Click the row to copy
it. "Scan screen" and "Import from images or files" are the other ways to add
accounts.
```

## Screenshots

`Tessera --marketing <outdir>` renders the 2560×1600 frames, light and dark. It
is compiled into DEBUG builds only, so the shipping binary carries no hidden
mode.

The terminal frames are not drawings. `docs/appstore-assets/captures/*.ansi` are
`tmux capture-pane -e` recordings of the real `tess` binary run against a
throwaway vault, and the renderer parses their SGR colors. Re-record rather than
edit them; a hand-edited capture is a fake screenshot.

Regenerate every frame:

```sh
cd swift && xcodegen generate
xcodebuild -project Tessera.xcodeproj -scheme Tessera -configuration Debug \
  -derivedDataPath /tmp/tessera-dd CODE_SIGNING_ALLOWED=NO build
cd .. && /tmp/tessera-dd/Build/Products/Debug/Tessera.app/Contents/MacOS/Tessera \
  --marketing docs/appstore-assets/1.0.3 --captures docs/appstore-assets/captures
```

Re-record a terminal frame (a throwaway vault, never a real one — these frames
show live codes):

```sh
export TESSERA_VAULT=$(mktemp -d)/vault.json TESSERA_PASSPHRASE=demo
go -C go run ./cmd/tess vault init
go -C go run ./cmd/tess add "otpauth://totp/GitHub:you?secret=ZB573K4APD63E6RLD3WAHI3QFZ35RLEP&issuer=GitHub"
tmux new-session -d -s shot -x 84 -y 16 "tess watch"
sleep 4 && tmux capture-pane -t shot -p -e > docs/appstore-assets/captures/tess-watch.ansi
tmux kill-session -t shot
```

84 columns is the width the renderer's terminal box holds without wrapping.
Capture `tess watch` mid-period so the countdown bars are visibly draining.

Each frame lands at roughly 3.6 MB. Recompress losslessly before committing:

```sh
python3 -c 'import glob
from PIL import Image
for p in glob.glob("docs/appstore-assets/1.0.3/*.png"):
    Image.open(p).convert("RGB").save(p, "PNG", optimize=True, compress_level=9)'
```

### Upload order

Eight frames, light set as primary; the dark set is the alternate. App Store
Connect takes up to 10 per localization.

| # | File | Caption | Source |
|---|---|---|---|
| 1 | `01-watch-*.png` | Codes in your terminal. | recorded `tess watch` |
| 2 | `02-code-*.png` | One command, one code. | recorded `tess code github -c` |
| 3 | `03-vault-*.png` | Every account, one window. | the app's real row view, in window chrome |
| 4 | `04-touchid-*.png` | Unlock with Touch ID. | the app's real locked screen |
| 5 | `05-import-*.png` | Bring your accounts over. | recorded `tess import --file` |
| 6 | `06-folders-*.png` | Folders and tags. | recorded `tess move` / `tess tag` / `tess list` |
| 7 | `07-format-*.png` | One vault file, two cores. | `spec/vault-format.md` |
| 8 | `08-private-*.png` | Nothing leaves your Mac. | entitlements + LICENSE |

### Still needs a live capture

Two frames cannot be produced by the headless renderer, because `ImageRenderer`
does not lay out the populated vault list and draws `TextEditor` as an
unsupported placeholder:

- **QR import from screen.** The "Scan screen" flow in `AddAccountView` needs a
  running app with screen-recording permission granted. Capture it on the Xcode
  machine: put a QR on screen, open Add accounts, click Scan screen, and record
  the window.
- **The populated window.** Frame 3 composes the app's real `AccountRowView`
  inside window chrome rather than screenshotting a running window. A real
  window capture of the unlocked vault, light and dark, would replace it.
