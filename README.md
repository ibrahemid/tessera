<div align="center">

<img src="docs/media/icon-256.png" width="72" alt="">

# Tessera

A TOTP/2FA authenticator for macOS. CLI-first, with a native app.

[![CI](https://img.shields.io/github/actions/workflow/status/ibrahemid/tessera/ci.yml?branch=main&label=CI&logo=github)](https://github.com/ibrahemid/tessera/actions)
[![License](https://img.shields.io/github/license/ibrahemid/tessera)](LICENSE)
[![Platform](https://img.shields.io/badge/platform-macOS-lightgrey)](https://apps.apple.com/app/tessera-2fa-authenticator/id6788814172)

[**Mac App Store**](https://apps.apple.com/app/tessera-2fa-authenticator/id6788814172) · [**Website**](https://tessera.ibrahemid.com) · [**CLI quick start**](#cli-quick-start)

</div>

<img src="docs/media/tess-cli.gif" alt="tess in a terminal: creating a vault, adding an account, printing codes with countdown bars, copying a code, and filtering the live watch view" width="100%">

Tessera keeps your 2FA codes in an encrypted vault on your Mac, reachable from a native app and a real command line. It ships free what other Mac authenticators paywall (Touch ID, auto-launch), and adds what they lack: a CLI, HOTP, Steam Guard, folders and tags, and one vault that the CLI and the app share. Free, open source, Apache-2.0.

## Features

- TOTP (RFC 6238), HOTP (RFC 4226), Steam Guard
- Import: `otpauth://`, Google Authenticator export (`otpauth-migration://`), QR images (CLI) / on-screen QR (app)
- Encrypted vault: random DEK, XChaCha20-Poly1305 payload, unlocked by an argon2id passphrase wrap, a Touch ID (Secure Enclave) wrap, or both
- Search, folders, tags, pinning (app)
- No account, no server, no analytics, no network

## The app

<picture>
  <source media="(prefers-color-scheme: light)" srcset="docs/appstore-assets/01-vault-light.png">
  <img src="docs/appstore-assets/01-vault-dark.png" alt="Tessera app showing accounts with live codes and countdown rings" width="100%">
</picture>

A native SwiftUI app: live codes with countdown rings, one-click copy, on-screen QR scanning, Touch ID unlock. It opens a CLI-created vault in place (asks for its passphrase once, then unlocks via the Secure Enclave; the CLI keeps working on the same file). App-created vaults are Secure-Enclave-bound: set a recovery passphrase in Settings to add a passphrase wrap (it survives a fingerprint change and lets the CLI open the same vault), or move accounts across with the app's encrypted export.

## CLI quick start

```sh
brew install ibrahemid/tap/tess
```

Homebrew 6 loads a third-party tap only after `brew trust ibrahemid/tap`; run that first.

Or without Homebrew (downloads the release for your os/arch and verifies its sha256):

```sh
curl -fsSL https://raw.githubusercontent.com/ibrahemid/tessera/main/install.sh | sh
```

Or from source, with Go 1.26+:

```sh
go install github.com/ibrahemid/tessera/go/cmd/tess@latest
```

Then:

```sh
tess vault init                      # create an encrypted vault
tess add "otpauth://totp/ACME:me@x.com?secret=JBSWY3DPEHPK3PXP&issuer=ACME"
tess add --qr ~/Desktop/code.png     # from a QR image
tess add --screen                    # select a QR code on screen (macOS)
tess import --migration "otpauth-migration://offline?data=..."
cat export.json | tess import -      # read the input from stdin (also `tess add -`)
tess                                 # print current codes (colored, with countdown bars)
tess watch                           # live TUI: countdown bars, search (/), copy (enter/c), q to quit
tess acme                            # one account's code, copied to the clipboard
tess ac --no-copy                    # by handle (shown by `tess list`), printed only
tess code acme --clear 30            # copy, then clear the clipboard after 30s
tess alias ac work                   # set an account's handle
tess code --json                     # machine-readable output for scripts
tess ls --json                       # alias for `tess list`
tess vault remember                  # store the passphrase in the macOS login keychain
tess vault status                    # path, file details, wrap methods, keychain state
tess export --uri acme               # otpauth URI (cleartext secret)
tess completion zsh > ...            # shell completions (bash/zsh/fish)
```

Binaries for macOS and Linux (arm64 and x86_64) are attached to every [release](https://github.com/ibrahemid/tessera/releases), with `checksums.txt`. Unpack the tarball and put `tess` on your `PATH`.

`tess <query>` is short for `tess code <query>`; a subcommand name always wins over an account handle. The code goes to the clipboard on a terminal, so `tess acme` prints it and copies it. Piped or with `--json`, tess prints the digits and leaves the clipboard alone. `--clear <seconds>` (default `$TESSERA_CLIP_CLEAR`) wipes the copy afterwards, and only while the clipboard still holds that code. A code with under four seconds left is held back until the next one, unless you pass `--now`.

Completions offer your account handles once the vault opens without a prompt, from `$TESSERA_PASSPHRASE` or the login keychain.

Colored output auto-disables when piped or when `NO_COLOR` is set.

Vault path: `$TESSERA_VAULT` or `~/.local/share/tessera/vault.json`. For scripting, set `TESSERA_PASSPHRASE` to avoid the prompt (`tess vault passwd` also reads `TESSERA_NEW_PASSPHRASE`).

On macOS, `tess vault remember` opts into storing the passphrase in the login keychain (`tess vault forget` removes it) so `tess` stops prompting; the entry is protected by the login keychain at the same trust level as an ssh key on disk.

## Security model

- Secrets are stored as raw bytes inside an encrypted vault; base32 only appears at otpauth boundaries.
- Vault: a random 256-bit DEK encrypts the account payload (XChaCha20-Poly1305, 24-byte nonces). The DEK is wrapped per unlock method: an argon2id-derived passphrase key, a biometric-gated Secure Enclave key, or both. Adding/removing an unlock method re-wraps the DEK without re-encrypting the payload.
- Two independent implementations (Go, Swift) share one documented format and a byte-exact interop vector suite, with cross-decrypt and negative (tamper / wrong-passphrase / base64url) tests.
- No analytics, no servers. See [SECURITY.md](SECURITY.md) for reporting.

The exact rules live in [`spec/vault-format.md`](spec/vault-format.md) and [`spec/otpauth.md`](spec/otpauth.md).

## Repository layout

| Path | What |
|------|------|
| `spec/` | Source of truth: vault format, otpauth/OTP rules, shared interop test vectors |
| `go/` | The `tess` CLI and its core (Go 1.26) |
| `swift/` | `TesseraCore` library + the SwiftUI app |
| `interop` / CI | Both implementations are checked against `spec/testvectors.json` |

Build instructions in [`docs/BUILD.md`](docs/BUILD.md).

## License

Apache-2.0.
