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

## Listing metadata (1.1.0)

Paste-ready, and the place to edit the wording. App Store Connect is a copy of
this, not the other way round.

Every claim here is checkable against the repo. Two were wrong in the v1.0.0
submission sheet (`docs/app-store-submission.html`) and are removed for good.
Neither ever reached the live listing; the shipped 1.0.2 description carries
neither string.

- **No menu bar.** The app has no `NSStatusItem` and no `MenuBarExtra`. Menu-bar
  quick access is a post-v1 idea (see the comment in `TesseraApp.swift`). Nothing
  in the listing, the review notes, or the screenshots may mention it until it
  ships.
- **The CLI is not inside the app bundle.** `Tessera.app` contains one binary,
  the app. `tess` is a separate free download, so the copy says so instead of
  "the included tess command-line tool".

A third claim is cut for the same reason: **`go install` is the only live `tess`
install path.** `ibrahemid/homebrew-tap` does not exist on GitHub yet and
`install.sh` sits on the unmerged `cli-install-path` branch, so the description
names neither Homebrew nor curl. Promotional text is editable without a review,
so the fuller list can go back the day both ship and the site is deployed.

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
TOTP, HOTP, and Steam Guard codes in the app, and in your terminal with the tess CLI, a separate download. One encrypted vault.
```

**Description:**

```
Tessera is an open-source authenticator for macOS with a command line. Set a recovery passphrase, or point the app at the vault tess already uses, and both read the same encrypted file.

Generate the codes you already use: TOTP, HOTP, and Steam Guard. Add an account by scanning a QR code on screen, pasting a setup link or a setup key, or importing an export from Google Authenticator, Aegis, 2FAS, Raivo, Bitwarden, Proton Authenticator, Ente Auth, andOTP, FreeOTP+, Stratum, 1Password or Apple Passwords.

Unlock with Touch ID. Search, pin the ones you use most, and group them into folders. Click a row to copy its code (an HOTP row advances the counter).

• Secrets are encrypted on your Mac with argon2id and XChaCha20-Poly1305. Where there is a Secure Enclave, the key is wrapped inside it.
• No account and no servers. The app ships without a network entitlement, so it cannot reach the network.
• Apache-2.0. The source, the vault spec, and the test vectors are public at github.com/ibrahemid/tessera

The vault format is a published spec with two implementations, one in Go and one in Swift, cross-decrypted against shared test vectors on every commit.

tess, the command-line tool, is a free separate download; see tessera.ibrahemid.com. It adds a live watch view with countdown bars, JSON output, and shell completions.

Works with any service that supports standard two-factor authentication. Tessera is not affiliated with Google, Microsoft, Steam, or any other provider.
```

**What's New (1.1.0):**

```
Import from more apps: Bitwarden, Proton Authenticator, Ente Auth, andOTP, FreeOTP+, Stratum, 1Password and Apple Passwords exports, alongside Google Authenticator, Aegis, 2FAS and Raivo. Encrypted exports are refused with a note on how to export them unencrypted.
```

The description's import sentence lists the same apps. `swift/project.yml`
reads `MARKETING_VERSION 1.1.0` / `CURRENT_PROJECT_VERSION 9`.

**Review notes (paste):**

```
Tessera is an offline TOTP authenticator; no login is required. To test: open the app, click the + button in the toolbar (or press Command-N), and paste this link:
otpauth://totp/Demo:tester?secret=JBSWY3DPEHPK3PXP&issuer=Demo
A 6-digit code appears with a 30-second countdown ring. Click the row to copy it. "Scan screen" and "Import from images or files" are the other ways to add accounts.
```

## Screenshots

`Tessera --marketing <outdir>` renders the 2560×1600 frames, light and dark. It
is compiled into DEBUG builds only, so the shipping binary carries no hidden
mode.

The terminal frames are not drawings. `docs/appstore-assets/captures/*.ansi` are
`tmux capture-pane -e` recordings of the real `tess` binary run against a
throwaway vault, and the renderer parses their SGR colors. The vault frame is not
a drawing either: `captures/app-vault-{light,dark}.png` are `screencapture`
recordings of the running app's unlocked window. Re-record rather than edit them;
a hand-edited capture is a fake screenshot.

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

Re-record the vault window (`03-vault`). `ImageRenderer` never lays out the
populated list headless, so that frame renders a real window capture. Build
Debug, then launch the app with the same exported `TESSERA_VAULT` still set (the
app reads it too), unlock it, and size the window to 720×522:

```sh
TESSERA_ALLOW_CAPTURE=1 \
  /tmp/tessera-dd/Build/Products/Debug/Tessera.app/Contents/MacOS/Tessera &
# unlock, then once per appearance (System Settings > Appearance):
screencapture -o -w docs/appstore-assets/captures/app-vault-light.png   # click the window
```

The app sets `window.sharingType = .none`, so the capture comes back empty
without `TESSERA_ALLOW_CAPTURE=1`, which lifts the shield in DEBUG builds only.
Move the pointer off the window first (a hovered row draws its highlight) and
capture mid-period so the countdown rings are visibly partial. 720pt wide
downsamples into the frame's 594pt content column instead of upsampling. `-o`
drops the window shadow, which the renderer draws itself; flatten the
transparent rounded corners it leaves behind, because the renderer re-rounds
them at 22pt:

```sh
python3 -c 'import sys
from PIL import Image
for p in sys.argv[1:]:
    im = Image.open(p).convert("RGBA"); w, h = im.size; px = im.load()
    for y in range(h):
        op = [x for x in range(w) if px[x, y][3] >= 250]
        first, last = op[0], op[-1]
        lc, rc = px[first, y][:3], px[last, y][:3]
        for x in range(w):
            if px[x, y][3] < 250:
                px[x, y] = (lc if x - first < last - x else rc) + (255,)
    im.convert("RGB").save(p, "PNG", optimize=True, compress_level=9)' \
  docs/appstore-assets/captures/app-vault-light.png \
  docs/appstore-assets/captures/app-vault-dark.png
```

Each frame lands at roughly 3.6 MB. Recompress losslessly before committing:

```sh
python3 -c 'import glob
from PIL import Image
for p in glob.glob("docs/appstore-assets/1.0.3/*.png"):
    Image.open(p).convert("RGB").save(p, "PNG", optimize=True, compress_level=9)'
```

### Upload order

App Store Connect takes one ordered set of up to 10 screenshots per
localization, with no light/dark alternate mechanism. Upload these eight files,
in this order. The dark set is for the site and press use; it does not go to
Connect. Guideline 2.3.3 wants the app itself first, so the two app frames lead
and the recorded CLI frames follow.

| # | File | Caption | Source |
|---|---|---|---|
| 1 | `docs/appstore-assets/1.0.3/03-vault-light.png` | Every account, one window. | a real capture of the unlocked window |
| 2 | `docs/appstore-assets/1.0.3/04-touchid-light.png` | Unlock with Touch ID. | the app's real locked screen |
| 3 | `docs/appstore-assets/1.0.3/01-watch-light.png` | Codes in your terminal. | recorded `tess watch` |
| 4 | `docs/appstore-assets/1.0.3/02-code-light.png` | One command, one code. | recorded `tess code github -c` |
| 5 | `docs/appstore-assets/1.0.3/05-import-light.png` | Bring your accounts over. | recorded `tess import --file` |
| 6 | `docs/appstore-assets/1.0.3/06-folders-light.png` | Folders and tags. | recorded `tess move` / `tess tag` / `tess list` |
| 7 | `docs/appstore-assets/1.0.3/07-format-light.png` | One vault file, two cores. | `spec/vault-format.md` |
| 8 | `docs/appstore-assets/1.0.3/08-private-light.png` | Nothing leaves your Mac. | entitlements + LICENSE |

Six of the eight are not app UI: four are recorded `tess` terminal output, one
is the vault-format card, one is entitlements + LICENSE. That is the sharpest
2.3.3 exposure in this submission, and the first two frames answer it: 1 is a
capture of the running app's unlocked window, 2 is its real locked screen.

The vault frame and the recorded terminal frames show the same five accounts in
the same folders. Keep them that way when re-recording either side.

### Still needs a live capture

**QR import from screen.** `ImageRenderer` draws `TextEditor` as an unsupported
placeholder, so `AddAccountView` cannot be rendered headless, and the "Scan
screen" flow needs a running app with screen-recording permission granted: put a
QR on screen, open Add accounts, click Scan screen, and capture the window.
`MarketingShot` renders eight frames and none of them is QR, so this needs a
frame and a caption decided before the capture has anywhere to land.
