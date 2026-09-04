# Tessera

CLI-first TOTP/2FA authenticator for macOS, plus a native SwiftUI app. Free, open source (Apache-2.0), on the Mac App Store.

## Layout

- `spec/` — SOURCE OF TRUTH. `vault-format.md`, `otpauth.md`, `testvectors.json`, `canonical_edge.json`. Change behavior here first.
- `go/` — `tess` CLI + core (module `github.com/ibrahemid/tessera/go`, Go 1.26). Security-critical code (OTP, base32, vault) is stdlib/`x/crypto` only.
- `swift/` — `TesseraCore` (Sources/) verified via `swiftc` + the SwiftUI app (App/). Deps: swift-crypto (Linux only) and the vendored PHC argon2 (`CArgon2` target, wrapped by `TesseraArgon2`).
- `docs/` — BUILD.md, APP_STORE.md, design spec under `docs/superpowers/specs/`.

## Hard rules

- The vault wire format and canonical JSON are an interop contract between Go and Swift. Any change MUST update `spec/` and keep both implementations byte-identical against `spec/testvectors.json`. Canonical JSON = sorted keys, Go `encoding/json` escaping with HTML-escaping OFF, base64-standard secrets, no whitespace.
- Crypto: random DEK + XChaCha20-Poly1305 payload; DEK wrapped per method (argon2id passphrase wrap everywhere; Secure Enclave wrap on Mac). Never hand-roll AEAD/KDF — Go uses `x/crypto`, Swift uses CryptoKit (XChaCha = HChaCha20 + ChaChaPoly) and the vendored CArgon2/TesseraArgon2 for argon2id.
- Secrets are raw bytes in the vault; base32 only at otpauth boundaries. Never log secrets.

## Testing

- Go: `go -C go test -race ./...`, `go -C go vet ./...`, `gofmt -l go/` (empty).
- Swift core (no Xcode): `swiftc -O swift/Sources/TesseraCore/*.swift swift/Tools/verify/main.swift -o /tmp/v && /tmp/v "$(pwd)/spec" "$(pwd)/go/internal/importers/testdata"`.
- Swift argon2id + full Go→Swift cross-decrypt: `cd swift && swift test` (needs full Xcode; CLT SwiftPM is broken).
- After changing vectors: regenerate via `go -C go run ./internal/vectorgen` and `... edge > spec/canonical_edge.json`, re-pin, rerun both suites.

## Needs full Xcode (the M1 Pro server has it; the Air's CLT does not)

- Building/running the SwiftUI `.app` (Secure Enclave, ScreenCaptureKit). Menu-bar quick access is a post-v1 idea, not shipped.
- `swift test` (CLT SwiftPM is broken).
- App Store archive/upload — see docs/APP_STORE.md.
