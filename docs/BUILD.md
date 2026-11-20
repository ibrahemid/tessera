# Building and testing Tessera

## Toolchains

| Tool | Version | Where it is set |
|------|---------|-----------------|
| Go | 1.27.4 | `go/go.mod` (CI's setup-go reads it) |
| Go in the Linux containers | `golang:1.27` image | `scripts/gate.sh` |
| staticcheck | 2026.2.2 | `.github/workflows/ci.yml` |
| govulncheck | v1.10.0 | `.github/workflows/ci.yml` |
| goreleaser | v2.20.2 | `.github/workflows/release.yml`, `scripts/release-check.sh` |
| cosign | v3.1.4 | `.github/workflows/release.yml` |
| Swift | tools 6.0, language mode 6.0 | `swift/Package.swift`, `swift/project.yml` |
| Xcode | 26.6 on CI (`macos-26`) | `.github/workflows/ci.yml` |
| xcodegen | 2.45.4 on CI | `.github/workflows/ci.yml` |
| Node | 26 | `.github/workflows/ci.yml`, `.github/workflows/site.yml` |

The app targets macOS 14.0.

## Go CLI

```sh
go -C go build ./...
go -C go vet ./...
go -C go test -race ./...
go -C go build -o tess ./cmd/tess
```

## The gate script

`scripts/gate.sh [target...]` runs every check for the named targets and prints
one `ok` or `FAIL` line per check. With no target it runs `go swift app shell`;
`all` runs every target. Each check writes its output to
`.status/gate/<check>.log` (`GATE_LOG_DIR` moves the folder), and the script
exits non-zero when any check failed. It adds `$(go env GOPATH)/bin` to `PATH`.

| Target | Checks | Needs |
|--------|--------|-------|
| `go` | build, vet, gofmt, the `gendocs` build tag's vet and tests, `go test -race ./...`, vet and staticcheck for linux and windows, cross builds for linux and windows (amd64, arm64) and darwin/amd64; govulncheck on the built binaries and staticcheck on the host when they are installed | Go, staticcheck |
| `swift` | `swift-verify`: the interop verifier against `spec/` and the importer fixtures; `swift-test`: `swift test` | swiftc; full Xcode for `swift test` |
| `app` | `app-entitlements`: `Tessera.entitlements` and `Tessera-AdHoc.entitlements` differ only by `keychain-access-groups`; `app-build`: Debug build for testing, signed ad hoc with `App/Resources/Tessera-AdHoc.entitlements`, warnings as errors; `app-unit`: the `TesseraAppTests` unit tests | full Xcode, xcodegen |
| `shell` | shellcheck on every shell script, then `test/install_test.sh` | shellcheck |
| `site` | `npm run build` in `site/` | Node, `npm ci` in `site/` first |
| `integrations` | Raycast build, eslint, prettier, tsc and vitest; Alfred tests, plist lint and build; both launchers' tests against a real `tess` and a scratch vault, which fail when any test is skipped | Node, Go; runs `npm ci` in `integrations/raycast` when `node_modules` is missing |
| `release` | `scripts/release-check.sh` (below) | Docker, a `vX.Y.Z` tag |
| `linux` | `go vet` and `go test` in the `golang:1.27` container, then `test/linux/run.sh` (below) | Docker |

The Go tests run with `XDG_DATA_HOME` set to `.status/gate/xdg` and
`TESSERA_VAULT` empty, so they never reach a real vault.

### App build output

`app-build` runs `xcodegen generate` in `swift/` and builds into derived data
at `~/Library/Caches/tessera-gate/<id>`, where `<id>` is the first 12 hex
digits of the SHA-1 of the checkout path; `GATE_DD_DIR` overrides it.
`.status/gate/dd` links to it. It stays outside the checkout because a failing
hosted test opens its object files, and that never returns under a
privacy-protected folder such as `~/Desktop`. The xcodebuild output is
in `.status/gate/xcodebuild.log` and `.status/gate/xcodebuild-unit.log`.

The Debug app has the bundle id `com.ibrahemid.tessera.dev`; Release is
`com.ibrahemid.tessera`. A Debug build launched as `Tessera --shoot [outdir]`
renders every screen in light and dark to `<screen>-<variant>-<light|dark>.png`
and quits. Without `outdir` the files go to the app's temporary folder,
`~/Library/Containers/com.ibrahemid.tessera.dev/Data/tmp/tessera_shots/`. It
prints each file's path and needs macOS 14.4 or later.

## Swift core without Xcode

The interop verifier compiles `TesseraCore` with `swiftc` and checks it against
every shared vector and the importer fixtures:

```sh
swiftc -O swift/Sources/TesseraCore/*.swift swift/Tools/verify/*.swift -o /tmp/v
/tmp/v "$(pwd)/spec" "$(pwd)/go/internal/importers/testdata"
```

Expected: `ALL SWIFT INTEROP CHECKS PASSED`.

`argon2id` is the one primitive CryptoKit lacks. Tessera vendors the PHC
reference argon2 (portable `ref.c`, no SIMD, threads off) as the `CArgon2`
target, wrapped by `TesseraArgon2`. The passphrase wrap and the full Go to Swift
vault decrypt run under `swift test` in `swift/`, which needs full Xcode: the
Command Line Tools ship a broken SwiftPM.

## The macOS app

```sh
cd swift
xcodegen generate              # Tessera.xcodeproj from project.yml
open Tessera.xcodeproj
```

Set `DEVELOPMENT_TEAM` in the target's signing settings to build signed. See
`docs/APP_STORE.md` for distribution.

## Linux container suites

`test/linux/run.sh [suite...]` builds static `tess` binaries and Go test
binaries into `.status/gate/linux/bin/<arch>`, then runs each suite in each
distro's container: `debian`, `ubuntu`, `fedora`, `alpine` and `arch`
(`GATE_LINUX_DISTROS` picks a subset). Containers run with `--network none` and
stop after 600 seconds. Every suite runs as uid 1000 except `package`, which
runs as root. Arch always runs as amd64. Logs go to
`.status/gate/linux/<distro>-<suite>.log`.

| Suite | Checks |
|-------|--------|
| `smoke` | version, the default vault path, `TESSERA_VAULT`, `vault`, `add`, `code` and `list` |
| `static` | the binaries are static and match the architecture |
| `secret-service` | `vault remember` and `forget` against a Secret Service on a D-Bus session |
| `no-secret-service` | the kernel keyring fallback and `TESSERA_PASSPHRASE` without a Secret Service |
| `keyctl` | the kernel keyring store: round trip, expiry, permissions, a second process |
| `keyctl-blocked` | a kernel keyring that the system refuses |
| `x11` | clipboard copy and timed clear through xclip and xsel under Xvfb |
| `wayland` | clipboard copy and timed clear through wl-clipboard under sway |
| `portal` | screen capture through a stand-in desktop screenshot portal |
| `screen-wayland` | `tess add --screen` on Wayland |
| `package` | installs the release packages with each distro's package manager |

`package` reads packages from `go/dist` (`GATE_LINUX_DIST` overrides it; the
`linux` gate target points it at the release check's output when that exists)
and is skipped when there are none. It installs them with `dpkg -i`,
`dnf install --nogpgcheck` (or `rpm -i --nosignature`),
`apk add --allow-untrusted` and `pacman -U`, since the packages carry no
package-manager signature.

## The install script test

`test/install_test.sh` runs `install.sh` against releases it builds locally,
served over `file://`, with stand-ins for `curl`, `cosign` and `uname`. It
needs no network.

## The release check

`scripts/release-check.sh` needs Docker and a `vX.Y.Z` tag reachable from
`HEAD`. It clones the checkout twice under `.status/gate/release/` (copying
uncommitted changes), then:

1. `goreleaser-check`: `goreleaser check` in the pinned goreleaser image.
2. `snapshot-a`, `snapshot-b`: a snapshot release in each tree, unsigned and unpublished.
3. `reproducible`: the two `checksums.txt` files are identical.
4. `dist-files`: four `.tar.gz`, two `.zip`, two of each Linux package and one `checksums.txt`.
5. `inspect-image`, then `contents-archive`, `contents-deb`, `contents-rpm`, `contents-apk` and `contents-archlinux`: the files inside each archive and package.

## Releases

Pushing a `vX.Y.Z` tag starts the release workflow. It runs goreleaser, which
builds `tess` with `CGO_ENABLED=0` for darwin, linux and windows on amd64 and
arm64, generates man pages and shell completions from the commands, and
produces:

- `tess_<version>_<os>_<arch>.tar.gz` for darwin and linux, `.zip` for windows
- a `tessera` package in deb, rpm, apk and Arch formats for linux
- `checksums.txt` (SHA-256), signed with keyless cosign into `checksums.txt.sigstore.json`

The workflow also records a build provenance attestation for every file in
`checksums.txt`. goreleaser creates the GitHub release as a draft with no
changelog. The `go/vX.Y.Z` module tag and the Homebrew formula
(`ibrahemid/homebrew-tap`, `Formula/tess.rb`: version and the SHA-256 of both
darwin and both linux archives from `checksums.txt`) are updated by hand.

`install.sh` and the release download URL are covered in the
[README](../README.md#install-the-cli).

## Regenerating interop vectors

`go/internal/vectorgen` generates every non-RFC vector; the RFC vectors in
`spec/testvectors.json` are hand-authored. Its tests fail when a spec file and
the generator disagree.

```sh
go -C go run ./internal/vectorgen                 # version 1 sets (Steam, argon2id, AEAD, HChaCha20, handles, vault): snippets to paste
go -C go run ./internal/vectorgen v2 < spec/testvectors.json > /tmp/testvectors.json \
  && mv /tmp/testvectors.json spec/testvectors.json   # version 2 sets, merge and exports, respliced
go -C go run ./internal/vectorgen edge > spec/canonical_edge.json
go -C go run ./internal/vectorgen edge2 > spec/canonical_edge_v2.json
```

The `v2` mode rewrites `vault_v2`, `envelope_strict`, `payload_strict`,
`text_clean`, `otpauth_strict`, `migration_strict`, `hotp_counter_range`,
`display_order`, `rotation`, `rotation_extra_passphrase`, `exports` and
`merge`, and keeps every other key. Run the Go tests and the Swift verifier
after any change.
