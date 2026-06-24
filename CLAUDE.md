# Tessera

CLI-first TOTP/2FA authenticator for macOS, plus a native SwiftUI menu-bar app. Free, open source (Apache-2.0), intended for the Mac App Store.

## Layout

- `spec/` — SOURCE OF TRUTH. `vault-format.md`, `otpauth.md`, `testvectors.json`, `canonical_edge.json`. Change behavior here first.
- `go/` — `tess` CLI + core (module `github.com/ibrahemid/tessera/go`, Go 1.26). Security-critical code (OTP, base32, vault) is stdlib/`x/crypto` only.
- `swift/` — `TesseraCore` (Sources/) verified via `swiftc` + the SwiftUI app (App/). SwiftPM-only deps: Argon2Swift, swift-crypto (Linux).
- `docs/` — BUILD.md, APP_STORE.md, design spec under `docs/superpowers/specs/`.

## Hard rules

- The vault wire format and canonical JSON are an interop contract between Go and Swift. Any change MUST update `spec/` and keep both implementations byte-identical against `spec/testvectors.json`. Canonical JSON = sorted keys, Go `encoding/json` escaping with HTML-escaping OFF, base64-standard secrets, no whitespace.
- Crypto: random DEK + XChaCha20-Poly1305 payload; DEK wrapped per method (argon2id passphrase wrap everywhere; Secure Enclave wrap on Mac). Never hand-roll AEAD/KDF — Go uses `x/crypto`, Swift uses CryptoKit (XChaCha = HChaCha20 + ChaChaPoly) and Argon2Swift for argon2id.
- Secrets are raw bytes in the vault; base32 only at otpauth boundaries. Never log secrets.

## Testing

- Go: `go -C go test -race ./...`, `go -C go vet ./...`, `gofmt -l go/` (empty).
- Swift core (no Xcode): `swiftc -O swift/Sources/TesseraCore/*.swift swift/Tools/verify/main.swift -o /tmp/v && /tmp/v "$(pwd)/spec"`.
- Swift argon2id + full Go→Swift cross-decrypt: `cd swift && swift test` (needs full Xcode; CLT SwiftPM is broken).
- After changing vectors: regenerate via `go -C go run ./internal/vectorgen` and `... edge > spec/canonical_edge.json`, re-pin, rerun both suites.

## Known deferred (needs full Xcode on the dev machine)

- Building/running the SwiftUI `.app` (MenuBarExtra, Secure Enclave, ScreenCaptureKit).
- `swift test` (validates the Argon2Swift binding; its exact API is CI/Xcode-verified, not locally).
- App Store submission (Apple ID, signing, App Store Connect) — see docs/APP_STORE.md.
