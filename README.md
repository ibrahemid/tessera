# Tessera

A TOTP/2FA authenticator for macOS. CLI-first, with a native menu-bar app. Free and open source (Apache-2.0).

Tessera ships free what other Mac authenticators paywall (menu bar, Touch ID, auto-launch), and adds what they lack: a real CLI, HOTP, Steam Guard, folders/tags, and an encrypted vault that the CLI and the app share.

## What's here

| Path | What |
|------|------|
| `spec/` | Source of truth: vault format, otpauth/OTP rules, shared interop test vectors |
| `go/` | The `tess` CLI and its core (Go 1.26) |
| `swift/` | `TesseraCore` library + the SwiftUI menu-bar app |
| `interop` / CI | Both implementations are checked against `spec/testvectors.json` |

## Features

- TOTP (RFC 6238), HOTP (RFC 4226), Steam Guard
- Import: `otpauth://`, Google Authenticator export (`otpauth-migration://`), QR images (CLI) / on-screen QR (app)
- Encrypted vault: random DEK, XChaCha20-Poly1305 payload, argon2id passphrase wrap, optional Touch ID (Secure Enclave) wrap
- Search, folders, tags, pinning
- The app opens a CLI-created vault in place (asks for its passphrase once, then unlocks via the Secure Enclave; the CLI keeps working on the same file). App-created vaults are Secure-Enclave-bound; move them to the CLI with the app's encrypted export.

## CLI quick start

```sh
go -C go install ./cmd/tess          # installs `tess`
tess vault init                      # create an encrypted vault
tess add "otpauth://totp/ACME:me@x.com?secret=JBSWY3DPEHPK3PXP&issuer=ACME"
tess add --qr ~/Desktop/code.png     # from a QR image
tess import --migration "otpauth-migration://offline?data=..."
tess                                 # print current codes (colored, with countdown bars)
tess watch                           # live TUI: countdown bars, search (/), copy (enter/c), q to quit
tess code acme -c                    # code for one account, copied to the clipboard
tess code --json                     # machine-readable output for scripts
tess list --json
tess export --uri acme               # otpauth URI (cleartext secret)
tess completion zsh > ...            # shell completions (bash/zsh/fish)
```

Colored output auto-disables when piped or when `NO_COLOR` is set.

Vault path: `$TESSERA_VAULT` or `~/.local/share/tessera/vault.json`. For scripting, set `TESSERA_PASSPHRASE` to avoid the prompt.

## Security model

- Secrets are stored as raw bytes inside an encrypted vault; base32 only appears at otpauth boundaries.
- Vault: a random 256-bit DEK encrypts the account payload (XChaCha20-Poly1305, 24-byte nonces). The DEK is wrapped per unlock method (argon2id-derived passphrase key on every platform; a biometric-gated Secure Enclave key on the Mac). Adding/removing an unlock method re-wraps the DEK without re-encrypting the payload.
- Two independent implementations (Go, Swift) share one documented format and a byte-exact interop vector suite, with cross-decrypt and negative (tamper / wrong-passphrase / base64url) tests.
- No analytics, no servers. Optional sync uses your own iCloud (CloudKit private DB), end-to-end encrypted.

See `spec/vault-format.md` and `spec/otpauth.md` for the exact rules, and `docs/` for build and App Store instructions.

## License

Apache-2.0.
