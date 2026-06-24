# Tessera Vault Format v1

Source of truth for the encrypted vault. Both the Go core and the Swift core MUST implement this byte-for-byte. Interop CI cross-decrypts and runs negative tests against `testvectors.json`.

## Load-bearing decision: envelope encryption

A random 256-bit **Data Encryption Key (DEK)** encrypts the account payload. The DEK is wrapped independently by each unlock method; every wrap is stored in the envelope. This lets a Secure-Enclave (Mac) vault be opened by the passphrase CLI and lets CloudKit round-trip without breaking the single shared format. Adding/removing an unlock method re-wraps the DEK; it does NOT re-encrypt the payload.

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
A biometric-gated (`.biometryCurrentSet`), non-extractable SE P-256 key. `wrapKey = HKDF-SHA256(ECDH(se_priv, se_pub_ephemeral)) -> 32 bytes`, exact KDF info/salt pinned when the Swift core lands. Plaintext DEK never rests on disk.

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

## Vault path

Canonical shared path: `$XDG_DATA_HOME/tessera/vault.json` (default `~/.local/share/tessera/vault.json`), overridable by `--vault` / `$TESSERA_VAULT`. The Mac app defaults to this SAME path (not its sandbox container) so the CLI and app share one vault. Under the App Store sandbox the Mac app obtains a one-time user-granted **security-scoped bookmark** to `~/.local/share/tessera/` and persists it; it does not relocate the vault. This is the correct sandbox pattern.

## Failure modes (negative tests, MUST reject)

- Tampered ciphertext or tag (AEAD auth failure).
- Wrong passphrase (argon2id -> wrong wrapKey -> DEK unseal fails).
- Truncated / malformed envelope, bad base64, base64url input, missing padding.
- Duplicate JSON keys in the decrypted payload.
- Unknown `version` or `aead`.
