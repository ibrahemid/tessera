# Tessera — Design Spec

A TOTP/2FA authenticator that beats G2FA. CLI-first (Go) plus a native SwiftUI macOS app, publishable to the Mac App Store. Free and open source.

Status: approved by product owner (decision record Q1–Q7, 2026-06-25). Source of truth for `/spec`, `/go`, `/swift`, `/interop`.

## 1. Goals and positioning

- CLI-first: a scriptable, `go install`-able authenticator. No existing macOS authenticator ships a CLI; this is the primary differentiator.
- Native macOS app: menu-bar resident, Touch ID, sleek, App Store quality.
- Beat G2FA: ship free and present every feature G2FA paywalls ($3.99 for menu bar / Touch ID / auto-launch / auto-backup), plus HOTP, Steam Guard, folders/tags, and real sync — none of which G2FA has.
- Security tool ethos: encrypted at rest, open source (auditability is a feature), "Data Not Collected" privacy label, offline-first code generation.

Non-goals (v1): browser extension, Windows/Linux GUI, custom account icons (fast-follow), self-hosted sync server.

## 2. Architecture (Q1: approved — two native implementations)

Monorepo, two independent native cores sharing one documented encrypted vault format and a byte-exact interop test-vector suite. NOT a Go c-shared library via FFI — that would drag the Go runtime into a sandboxed, Apple-re-signed App Store binary and complicate review. Two cores keep the App Store app pure Swift.

```
tessera/
  spec/          source of truth: vault-format.md, otpauth.md, testvectors.json
  go/            cobra CLI + core (pquerna/otp, gozxing, protobuf, x/crypto)
  swift/         SwiftUI MenuBarExtra app (CryptoKit + swift-sodium, Keychain+SE, Vision)
  interop/       CI harness: both impls must agree on testvectors.json (cross-decrypt + negatives)
  docs/
```

Risk accepted: two crypto implementations = two chances at a crypto bug. Mitigation: interop CI does cross-decryption and negative tests, not just "both produce the same TOTP" (see §7).

## 3. Vault format (Q2/Q3: argon2id + XChaCha20-Poly1305, envelope encryption)

Load-bearing decision: **envelope encryption**, not per-method key derivation. A random 256-bit Data Encryption Key (DEK) encrypts the payload. The DEK is wrapped independently by each unlock method; all wraps are stored in the envelope. This lets a Mac-created (Secure Enclave) vault be opened by the passphrase CLI and lets CloudKit round-trip without breaking the single shared format.

```jsonc
{
  "version": 1,
  "aead": "xchacha20poly1305",          // shared AEAD; Go x/crypto/chacha20poly1305, Swift swift-sodium
  "wraps": [
    {
      "type": "passphrase",
      "kdf": "argon2id",
      "params": { "v": 1, "m": 131072, "t": 3, "p": 4 },   // pinned; m in KiB (128 MiB), versioned for migration
      "salt":  "<16 random bytes, b64>",
      "nonce": "<24 random bytes, b64>",
      "ct":    "<XChaCha20-Poly1305(DEK), tag appended, b64>"
    },
    {
      "type": "secure-enclave",          // Mac app only; key wrapped by biometric-gated SE P-256
      "se_key": "<SE key dataRepresentation blob, b64>",
      "nonce": "<24 random bytes, b64>",
      "ct":    "<XChaCha20-Poly1305(DEK), tag appended, b64>"
    }
  ],
  "payload": {
    "nonce": "<24 random bytes, b64>",
    "ct":    "<XChaCha20-Poly1305(serialized accounts, DEK), tag appended, b64>"
  }
}
```

- DEK: 32 random bytes, never persisted unwrapped.
- Passphrase wrap: argon2id(passphrase, salt, params) -> 32-byte key -> XChaCha20-Poly1305 seal of DEK.
- Secure Enclave wrap (Mac): SE P-256 key (biometry-gated, non-extractable) -> ECDH -> HKDF-SHA256 -> 32-byte key -> seal of DEK. Plaintext DEK never rests on disk.
- Every nonce is 24 bytes, fresh per encryption (192-bit space removes nonce-reuse risk).
- Adding/removing an unlock method re-wraps the existing DEK; it does not re-encrypt the payload.
- Payload (plaintext before sealing): canonical serialization (CBOR or stable-key JSON; pinned in vault-format.md) of the account list.

Account record:
```jsonc
{ "id": "uuid", "type": "totp|hotp|steam", "issuer": "", "account": "",
  "secret": "<raw bytes, b64>", "algorithm": "SHA1|SHA256|SHA512",
  "digits": 6, "period": 30, "counter": 0, "folder": "", "tags": [], "pinned": false,
  "created_at": 0, "updated_at": 0 }
```
Note: `secret` stored as raw bytes (matches Google migration export); base32 only at otpauth import/export boundaries.

## 4. OTP engines (spec-exact)

- TOTP: RFC 6238. T0=0, X=period, HMAC-SHA1/256/512, 6/7/8 digits, zero-padded. Go: pquerna/otp.
- HOTP: RFC 4226 dynamic truncation. Counter persisted and incremented on view/copy (CLI flag controls increment).
- Steam Guard: RFC 6238 base (HMAC-SHA1, 30s) but secret is base64, 5-char code, alphabet `23456789BCDFGHJKMNPQRTVWXY` (mod-26 over the 31-bit DT integer). Implemented as a custom Encoder.
- base32: RFC 4648, case-insensitive, padding optional, whitespace stripped; display grouped by 4.
- otpauth:// URI: full Key Uri Format parse/emit (issuer/account label, algorithm, digits, period, counter). Spec'd in otpauth.md.
- otpauth-migration:// (Google export): URL-decode -> base64-decode -> protobuf-decode (MigrationPayload). `secret` is RAW bytes; base32-encode (no pad) when rebuilding otpauth URIs. Handle multi-QR batches (batch_id/index/size). protoc-generated .pb.go vendored.

## 5. CLI (Go, cobra)

Binary: `tess` (fallback `tessera`). Targets Go 1.26, floor `go 1.25`.

Commands (v1):
- `tess add` — add account: from otpauth URI, manual entry, or `--qr <image>` (gozxing decode).
- `tess list [--folder f] [--tag t]` — list accounts (no codes by default).
- `tess code <query>` / `tess` (default) — print current code(s); `--watch` live countdown TUI (bubbletea, behind subcommand).
- `tess import` — `--otpauth`, `--migration <uri|image>` (Google), `--file <encrypted export>`.
- `tess export` — encrypted export (non-proprietary, same vault format) and `--qr` per-account.
- `tess rm`, `tess rename`, `tess move` (folder), `tess tag`.
- `tess vault init|passwd|unlock` — manage passphrase wrap.
Vault path: `$XDG_DATA_HOME/tessera/vault.json` (default `~/.local/share/tessera`), overridable by `--vault`/`$TESSERA_VAULT`; shared with the Mac app when pointed at the same path.
All secret-bearing output goes to stdout only; never logged. Errors via typed error values with context.

## 6. macOS app (SwiftUI)

- `MenuBarExtra(... ).menuBarExtraStyle(.window)` + `LSUIElement` agent (no Dock icon by default; setting to show in Dock).
- Crypto: CryptoKit (HMAC/HKDF/SHA, SE P-256) + swift-sodium (argon2id + XChaCha20-Poly1305).
- Secrets: encrypted vault file; DEK wrapped by Secure Enclave key gated with `.biometryCurrentSet`; `LAContext` Touch ID. Touch ID lock default OFF (do not force app lock — a top user complaint).
- QR screen-scan: ScreenCaptureKit `SCScreenshotManager` -> Vision `VNDetectBarcodesRequest(.qr)` -> otpauth URI. Camera scan optional (entitlement only if used).
- Quick access: menu-bar popover, search, global hotkey, click-to-copy with clear affordance (G2FA complaint: copy-by-click not discoverable).
- Theme switcher: light / dark / system (never default dark).
- Auto-launch: `SMAppService` login item, user-toggleable.
- Sync (Q4): CloudKit `privateCloudDatabase`, **per-account encrypted records** (not one blob — avoids last-writer-wins data loss; allows field-level merge). Client-side encrypted; Apple never sees plaintext -> honest "Data Not Collected". CLI shares the on-disk vault path; cross-platform sync is fast-follow.

## 7. Interop test suite + CI (Q1 condition)

`/spec/testvectors.json` is consumed by both implementations. Required CI cases:
- RFC 6238/4226 official vectors; Steam code vectors; base32 edge cases (padding/whitespace/case).
- otpauth:// round-trip; Google migration payload decode round-trip (raw-bytes secret -> base32).
- Vault cross-decrypt: Go-encrypt -> Swift-decrypt and Swift-encrypt -> Go-decrypt, byte-for-byte payload equality.
- Negative tests: tampered tag rejected; wrong passphrase rejected; truncated/garbage envelope rejected.
- argon2id params (m/t/p) and a known passphrase->DEK vector pinned in testvectors.json.
GitHub Actions: matrix builds Go (test + vet + race) and Swift (build + test); interop job runs both against the shared vectors.

## 8. Licensing / positioning (Q6)

Free and open source, Apache-2.0 (patent grant). "Data Not Collected" privacy label. No analytics/crash SDK, no server.

## 9. App Store path (Q7 + external dependencies)

- Name: Tessera. Avoid implying Google/Microsoft/Steam affiliation; describe as "works with any TOTP service." Trademark check at filing.
- Sandbox entitlement mandatory; `network.client` only for CloudKit; `device.camera` only if camera scan ships; screen capture is TCC-gated (no entitlement). Local Keychain needs no entitlement.
- MAS apps are NOT notarized (Apple re-signs). Need Apple Distribution cert + MAS provisioning profile + App Store Connect record.
- External dependencies that no agent can perform: install **Xcode**; Apple ID signing; create App ID + App Store Connect record; metadata/screenshots/icon/age rating; privacy questionnaire; submit + respond to review.

## 10. Build order

1. `/spec`: vault-format.md, otpauth.md, testvectors.json (seed with RFC vectors).
2. Go core (TDD): base32, otpauth, TOTP/HOTP/Steam, migration import, vault (envelope + argon2id + XChaCha20), QR decode.
3. Go CLI (cobra) over the core.
4. Swift core: mirror crypto + OTP, pass the same vectors; cross-decrypt with Go.
5. SwiftUI app: menu bar, vault unlock, QR scan, theme, sync.
6. Interop CI green; App Store prep + handoff doc.
