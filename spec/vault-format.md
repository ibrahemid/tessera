# Tessera Vault Format v1

Source of truth for the encrypted vault. Both the Go core and the Swift core MUST implement this byte-for-byte. Interop CI cross-decrypts and runs negative tests against `testvectors.json`.

## Load-bearing decision: envelope encryption

A random 256-bit **Data Encryption Key (DEK)** encrypts the account payload. The DEK is wrapped independently by each unlock method; every wrap is stored in the envelope. This lets one vault carry a Secure-Enclave (Mac app) wrap and a passphrase wrap side by side — the CLI opens the passphrase wrap, the app opens either — and lets CloudKit round-trip without breaking the single shared format. Adding/removing an unlock method re-wraps the DEK; it does NOT re-encrypt the payload.

## File

UTF-8 JSON, no BOM. Top-level object:

```jsonc
{
  "version": 1,
  "aead": "xchacha20poly1305",
  "wraps": [ <wrap>, ... ],     // >= 1; order not significant
  "payload": { "nonce": "<b64>", "ct": "<b64>" }
}
```

- `wraps` MUST be non-empty on disk. An implementation may hold a zero-wrap envelope in memory while assembling one (Swift `Envelope.createUnwrapped`), but MUST attach at least one wrap before persisting; a zero-wrap file is permanently unopenable.
- `aead` is always `xchacha20poly1305`. Nonces are 24 bytes, fresh per encryption (192-bit space removes nonce-reuse risk). `ct` = ciphertext with the 16-byte Poly1305 tag appended (libsodium/`chacha20poly1305` combined-mode layout).
- `payload.ct` = `XChaCha20-Poly1305(seal(canonical_json(accounts), key=DEK, nonce=payload.nonce))`.

### Passphrase wrap
```jsonc
{
  "type": "passphrase",
  "kdf": "argon2id",
  "params": { "v": 1, "m": 131072, "t": 3, "p": 4 },   // m in KiB = 128 MiB
  "salt":  "<16 bytes, b64>",
  "nonce": "<24 bytes, b64>",
  "ct":    "<seal(DEK), b64>"
}
```
`wrapKey = argon2id(passphrase_utf8, salt, t, m, p, keyLen=32)`. `ct = XChaCha20-Poly1305(seal(DEK, wrapKey, nonce))`. `params.v` is the param-version; raising m/t/p later bumps `v` and the vault re-seals this wrap on next successful unlock (payload untouched).

### Secure Enclave wrap (Mac app only)
```jsonc
{
  "type": "secure-enclave",
  "se_key": "<SecureEnclave.P256 key dataRepresentation, b64>",
  "nonce": "<24 bytes, b64>",
  "ct":    "<seal(DEK), b64>"
}
```
A non-extractable SE P-256 key, created with `.privateKeyUsage` and, when the user enables "Require Touch ID", `.biometryCurrentSet`. `wrapKey = HKDF-SHA256(ECDH(se_priv, se_pub)) -> 32 bytes` (self key-agreement; salt `tessera.se.salt.v1`, info `tessera.se.dek.v1`). `ct = XChaCha20-Poly1305(seal(DEK, wrapKey, nonce))`. The plaintext DEK never rests on disk; the biometric flag is an OS access-control attribute and does not change the wire format. This is the Mac app's default daily-unlock wrap (no argon2); the passphrase wrap is the cross-platform/export path and the SE-unavailable fallback.

## Base64

All `b64` fields are **base64 standard alphabet (RFC 4648 §4, `+/`), WITH `=` padding**. NOT base64url. Decoders MUST reject base64url and missing padding.

## Canonical JSON (the sealed-before-encryption payload)

`payload.ct` decrypts to canonical JSON of the account array. "Canonical" is defined as MUST rules so both impls produce identical bytes (cross-decrypt depends on it):

1. Object keys sorted ascending by **UTF-8 byte order** (not locale, not language Dictionary order).
2. No insignificant whitespace. No newlines. UTF-8, no BOM.
3. Strings: minimal JSON escaping (`"`, `\`, control chars `< 0x20` as `\uXXXX` lowercase hex; `/` not escaped). No non-ASCII escaping (emit raw UTF-8).
4. Numbers: integers only, base-10, no leading zeros, no `+`, no exponent, no decimal point. Counters fit `int64`.
5. Secrets are RAW key bytes, base64-standard with padding (see above). Google-migration secrets arrive as raw bytes; Steam secrets are base64-decoded to raw bytes; otpauth secrets are base32-decoded to raw bytes. The payload normalizes ALL to raw-bytes-then-base64.
6. Parsers MUST reject duplicate keys.

This canonical form is the SEALED-BEFORE-ENCRYPTION bytes. It is NOT a stable on-disk artifact: AEAD output differs every write (fresh nonce), so two encrypted files of the same accounts are NOT equal. Interop CI diffs the DECRYPTED canonical bytes, never the encrypted file.

### Account object
```jsonc
{
  "account": "john@example.com",
  "algorithm": "SHA1",            // SHA1 | SHA256 | SHA512
  "counter": 0,                    // HOTP only; 0 for totp/steam
  "created_at": 0,                 // unix seconds
  "digits": 6,
  "folder": "",
  "handle": "ac",              // OPTIONAL; see Handles
  "id": "uuid-v4",
  "issuer": "ACME",
  "period": 30,
  "pinned": false,
  "secret": "<raw bytes, b64-standard>",
  "tags": [],
  "type": "totp",                  // totp | hotp | steam
  "updated_at": 0
}
```
(Keys shown sorted, as they MUST be serialized.)

**Forward compatibility.** Readers MUST ignore fields they do not recognize in a decrypted account object rather than rejecting the payload. The payload is AEAD-authenticated, so tolerating unknown fields after a successful decrypt is safe, and it lets an older implementation open a vault written by a newer one that added a field. Writers MUST NOT emit any field not defined by this spec; canonical encoding remains exactly the spec fields above (an unknown field read from disk is dropped, not re-serialized). Duplicate-key rejection (rule 6) and envelope/header strictness are unaffected.

## Handles

A **handle** is a short, unique, user-typeable identifier for an account (e.g. `gi`, `go2`, `aw3`), shown alongside the account in both the CLI and the app and accepted wherever an account is looked up. It disambiguates accounts that share (or lack) an issuer.

- `handle` is an OPTIONAL string account field. When present it MUST match `^[a-z][a-z0-9]{0,11}$` (lowercase, leading letter, 1–12 chars) and MUST be unique across all accounts in the vault.
- It sorts between `folder` and `id` in the canonical key order (UTF-8 byte order), as shown above.

**Compatibility.** Vaults written before handles existed have no `handle` on their accounts. Handles are NOT required on disk: an account MAY omit the field. Implementations MUST auto-assign a handle to every account that lacks one at unlock (see *Handle assignment*). Assignment happens in memory at unlock; the implementation then persists the newly assigned handles **immediately on unlock, in a single atomic write** (re-wrap-free: the payload is re-sealed with the assigned handles; wraps untouched). A read-only open that cannot write (e.g. `--vault` pointing at a read-only file, or an export-only path) keeps the assigned handles in memory only and does not persist them; the same deterministic algorithm reproduces identical handles on the next writable unlock.

**User-editable.** Handles are user-editable. An edited handle MUST satisfy the charset and the uniqueness rules above; the interface rejects an edit that violates either. Auto-assignment NEVER overwrites a handle that is already present (whether original or user-edited).

### Handle assignment

Deterministic; both cores MUST produce identical handles for identical input.

**Assignment order.** When multiple accounts lack a handle, assign them in ascending `created_at`, then ascending `id` (lexicographic). This makes migration order-independent of vault storage order.

For each account needing a handle:

1. **Base source string.** Normalize the `issuer`: lowercase, keep only `[a-z0-9 ]` (drop everything else), collapse runs of whitespace to a single space, trim. If the result is empty, normalize the local part of `account` (the substring before the first `@`) the same way. If that is also empty, the base is the literal `acct` used **verbatim** (skip step 2 — do NOT re-derive it to first-two-chars; the base is `acct`, not `ac`), and proceed to step 3.
2. **Base.** Split the base source on spaces into words. A single word yields its first two characters (or its one character, if the word is a single char). Two or more words yield the first character of each of the first two words. If the resulting base begins with a digit, prefix it with `x`.
3. **Uniqueness.** If the bare base is not already taken by any existing handle in the vault, assign it. Otherwise assign `base` + `N`, where `N` is the smallest integer ≥ 2 such that `base`+`N` is not taken by ANY existing handle (including original and user-edited ones). Each newly assigned handle immediately counts as taken for the accounts assigned after it. Existing handles are NEVER renumbered when accounts are added or removed.

Examples: issuer `ACME` → `ac`; issuer `GitHub` (single word, first two chars) → `gi`; issuer `Google Cloud` → `gc`; issuer `1Password` (base `1p`, digit-leading) → `x1p`; empty issuer, account `alice@example.com` → local part `alice` → `al`; empty issuer and empty account → `acct`. A second `GitHub` when `gi` is taken → `gi2`; a third → `gi3`; if a user already edited some handle to `gi2`, the auto-assigned third `GitHub` skips to `gi3`.

## Account resolution (CLI/app lookup)

When the user references an account by a text query, both the CLI and the app resolve it by this precedence:

1. **Exact handle** match (handles are always lowercase; compare against the lowercased query).
2. **Exact `issuer` or `account`** match, case-insensitive.
3. **Unique substring** match against `handle`, `issuer`, or `account`, case-insensitive.

The first stage that yields exactly one account resolves. If a stage yields more than one match, the interface MUST present the matching accounts (each shown with its handle) so the user can pick, rather than failing with a bare "not found" / "ambiguous" error. Only when no stage matches anything is it an unresolved-account error.

**Editing handles.** Editing a handle is a first-class action, not a buried setting. The CLI MUST provide a dedicated `tess alias <account> <new-handle>` command (in addition to any `--handle` flag on the modify path), and the app MUST expose the handle as an editable field on the account. Both surfaces apply the same charset and uniqueness validation as auto-assignment. Changing a handle frees the old value, which then becomes available to future auto-assignment.

## Vault path

Canonical shared path: `$XDG_DATA_HOME/tessera/vault.json` (default `~/.local/share/tessera/vault.json`), overridable by `--vault` / `$TESSERA_VAULT`. The Mac app defaults to this SAME path (not its sandbox container) so the CLI and app share one vault. Under the App Store sandbox the Mac app obtains a one-time user-granted **security-scoped bookmark** to `~/.local/share/tessera/` and persists it; it does not relocate the vault. This is the correct sandbox pattern.

## Failure modes (negative tests, MUST reject)

- Tampered ciphertext or tag (AEAD auth failure).
- Wrong passphrase (argon2id -> wrong wrapKey -> DEK unseal fails).
- Truncated / malformed envelope, bad base64, base64url input, missing padding.
- Duplicate JSON keys in the decrypted payload.
- Unknown `version` or `aead`.

## Handle test vectors

`testvectors.json` and `canonical_edge.json` MUST cover handles so both cores stay byte-identical. The Go implementer regenerates these via `go/internal/vectorgen`; the coverage below is the contract.

- Accounts serialized WITH a `handle` field (canonical output includes it in sorted position between `folder` and `id`).
- Multi-word issuer → base from first char of each of the first two words (e.g. `Google Cloud` → `gc`).
- Digit-leading issuer → `x`-prefixed base (e.g. `1Password` → `x1p`).
- Empty issuer with an email account → base from the local part (e.g. issuer `""`, account `alice@example.com` → `al`).
- Collision chain producing `base`, `base2`, `base3` from three accounts with the same base.
- A user-edited handle occupying `base2` so the next auto-assignment skips to `base3` (proves auto-assign avoids taken handles and never renumbers).
