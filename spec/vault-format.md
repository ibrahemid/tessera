# Tessera Vault Format (versions 1 and 2)

Source of truth for the encrypted vault. Both the Go core and the Swift core MUST implement this byte-for-byte. Interop CI cross-decrypts and runs negative tests against `testvectors.json`.

## Load-bearing decision: envelope encryption

A random 256-bit **Data Encryption Key (DEK)** encrypts the account payload. The DEK is wrapped independently by each unlock method; every wrap is stored in the envelope. This lets one vault carry a Secure-Enclave (Mac app) wrap and a passphrase wrap side by side — the CLI opens the passphrase wrap, the app opens either — and lets CloudKit round-trip without breaking the single shared format. Adding an unlock method wraps the current DEK and leaves the payload alone. Changing a passphrase rotates the DEK (see *DEK rotation*).

## File

UTF-8 JSON, no BOM. Top-level object:

```jsonc
{
  "version": 1,                 // 1 or 2; see Versions
  "aead": "xchacha20poly1305",
  "wraps": [ <wrap>, ... ],     // 1..16; order not significant
  "payload": { "nonce": "<b64>", "ct": "<b64>" }
}
```

### Versions

- `version` is `1` or `2`. Any other value is rejected (`unsupported version`).
- Version 2 means: account objects may carry the v2 fields (`deleted_at`, `note`, `order`) and extension fields (see *Account object*).
- An implementation of this spec MUST read both versions.
- The writer derives the version from the payload it is about to seal: `2` when any account carries a key outside the v1 key set, otherwise `1`. The v1 key set is `account, algorithm, counter, created_at, digits, folder, handle, id, issuer, period, pinned, secret, tags, type, updated_at`. A vault that uses no v2 field therefore stays readable by 1.1.0 tools, and the version returns to 1 when the last v2 field is cleared.
- 1.1.0 tools reject version 2. That is intended: they drop keys they do not know when they write, so opening a version 2 vault with one would lose data.

### Envelope strictness

The envelope is untrusted input until the payload authenticates. Both cores MUST apply the same checks, for either version:

- Keys are matched exactly (case-sensitive). A duplicate key in any envelope object is rejected.
- `wraps` holds 1 to 16 entries. Zero or more than 16 is rejected.
- Every `b64` field is canonical standard base64 (see *Base64*): the decoded bytes MUST re-encode to the same string. Whitespace and line breaks inside a value are rejected.
- Nonces are exactly 24 bytes.
- Keys the implementation does not recognize, at the top level or inside a wrap, and wraps whose `type` it does not recognize, are kept and written back unchanged. A writer replaces or removes only the wraps it owns.

- `wraps` MUST be non-empty on disk (and at most 16). An implementation may hold a zero-wrap envelope in memory while assembling one (Swift `Envelope.createUnwrapped`), but MUST attach at least one wrap before persisting; a zero-wrap file is permanently unopenable.
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
`wrapKey = argon2id(passphrase_utf8, salt, t, m, p, keyLen=32)`. `ct = XChaCha20-Poly1305(seal(DEK, wrapKey, nonce))`. `params.v` is the param-version of the writer.

Checks before any derivation runs (a wrap that fails one is corrupt and is skipped; if no passphrase wrap survives, the error names the first failed check):

- `kdf` MUST be `argon2id`.
- `salt` is exactly 16 bytes.
- `8 <= m <= 1048576` (KiB, so at most 1 GiB), `1 <= p <= 16`, `m >= 8*p`, `t >= 1`, and `m * t <= 4194304`. The product bounds the work a file can demand to about ten times the default.
- At most the first 4 well-formed passphrase wraps are tried per unlock.

Upgrade: after a successful unlock, a wrap whose `m`, `t` or `p` is below the current default is replaced by a wrap at the default cost under the same passphrase and DEK, whatever its `params.v`. The payload is untouched; the upgrade is persisted with the next write, or immediately when the unlock is writable.

### Secure Enclave wrap (Mac app only)
```jsonc
{
  "type": "secure-enclave",
  "kdf":   "se-keychain-v1",
  "se_key": "<16-byte key id, b64>",
  "nonce": "<24 bytes, b64>",
  "ct":    "<seal(DEK), b64>"
}
```
A non-extractable SE P-256 key, created with `.privateKeyUsage` and, when the user enables "Require Touch ID", `.biometryCurrentSet`. The key's `dataRepresentation` is stored in the data-protection keychain, never in the vault file: generic password, service `com.ibrahemid.tessera`, account `se-key.<lowercase hex of the key id>`, `kSecAttrAccessibleWhenUnlockedThisDeviceOnly`, not synchronizable. `se_key` in the wrap is the random 16-byte id that names that item.

`wrapKey = HKDF-SHA256(ECDH(se_priv, se_pub)) -> 32 bytes` (self key-agreement; salt `tessera.se.salt.v1`, info `tessera.se.dek.v2`). `ct = XChaCha20-Poly1305(seal(DEK, wrapKey, nonce))`. The plaintext DEK never rests on disk; the biometric flag is an OS access-control attribute and does not change the wire format. This is the Mac app's default daily-unlock wrap (no argon2); the passphrase wrap is the cross-platform/export path and the SE-unavailable fallback.

Why the key lives in the keychain: an SE key blob is bound to the device, not to the app. Any process that holds the blob can ask the Secure Enclave to use it, silently when the key has no biometric flag. The data-protection keychain releases the item only to the app's access group, so a process that reads the vault file gets an id and nothing it can use.

**Legacy form (written by app versions before 1.2.0).** No `kdf` key; `se_key` is the key's `dataRepresentation` itself; info string `tessera.se.dek.v1`. The app still opens it, and on the first successful open it MUST:

1. create a keychain-held key, add the current-form wrap and remove the legacy wrap, in one write;
2. rotate the DEK (see *DEK rotation*), because the legacy blob in any older copy of the file still unwraps the old DEK. A vault with no passphrase wrap rotates immediately. A vault with a passphrase wrap rotates as soon as the passphrase is available (the next passphrase entry in the app, or `tess vault rekey`); until then the app shows that the step is pending.

The CLI never opens either form. It reports `ErrAppOnlyVault` when a `secure-enclave` wrap is present and no passphrase wrap is.

## Base64

All `b64` fields are **base64 standard alphabet (RFC 4648 §4, `+/`), WITH `=` padding**. NOT base64url. Decoders MUST reject base64url, missing padding, embedded whitespace or line breaks, and any value whose decoded bytes do not re-encode to the same string (non-zero trailing bits).

## Canonical JSON (the sealed-before-encryption payload)

`payload.ct` decrypts to canonical JSON of the account array. "Canonical" is defined as MUST rules so both impls produce identical bytes (cross-decrypt depends on it):

1. Object keys sorted ascending by **UTF-8 byte order** (not locale, not language Dictionary order).
2. No insignificant whitespace. No newlines. UTF-8, no BOM.
3. Strings: Go `encoding/json` string escaping with HTML-escaping OFF (`json.Encoder.SetEscapeHTML(false)`), which is exactly:
   - The two-character escapes `\"`, `\\`, `\b`, `\f`, `\n`, `\r`, `\t` (Go 1.22 and later write `\b` and `\f`; earlier releases wrote `\u0008` and `\u000c`, and readers accept both). A control char `< 0x20` with no two-character form is `\u00XX` with lowercase hex (e.g. U+0001 -> `\u0001`). U+007F is written raw.
   - `/`, `<`, `>`, `&` are NOT escaped (that is what HTML-escaping off means).
   - No non-ASCII escaping: emit raw UTF-8. The ONE exception is the line separators U+2028 and U+2029, which Go always escapes as `\u2028` / `\u2029` regardless of the HTML-escaping setting; both cores MUST escape them too. `canonical_edge.json` and `canonical_edge_v2.json` pin all of the above.
4. Numbers: integers only, base-10, no leading zeros, no `+`, no exponent, no decimal point, no `-0`. Every integer fits `int64`. Readers MUST reject any other number form anywhere in the payload (`6.0`, `1e3`), and MUST reject a non-boolean `pinned` or a non-integer where an integer field is defined.
5. Secrets are RAW key bytes, base64-standard with padding (see above). Google-migration secrets arrive as raw bytes; Steam secrets are base64-decoded to raw bytes; otpauth secrets are base32-decoded to raw bytes. The payload normalizes ALL to raw-bytes-then-base64.
6. Parsers MUST reject duplicate keys, compared as byte strings after unescaping.

This canonical form is the SEALED-BEFORE-ENCRYPTION bytes. It is NOT a stable on-disk artifact: AEAD output differs every write (fresh nonce), so two encrypted files of the same accounts are NOT equal. Interop CI diffs the DECRYPTED canonical bytes, never the encrypted file.

### Account object
```jsonc
{
  "account": "john@example.com",
  "algorithm": "SHA1",            // SHA1 | SHA256 | SHA512
  "counter": 0,                    // HOTP only; 0 for totp/steam
  "created_at": 0,                 // unix seconds
  "deleted_at": 1760000000,        // v2, OPTIONAL; see Trash
  "digits": 6,
  "folder": "",
  "handle": "ac",              // OPTIONAL; see Handles
  "id": "uuid-v4",
  "issuer": "ACME",
  "note": "backup codes in the safe", // v2, OPTIONAL
  "order": 3,                      // v2, OPTIONAL; see Display order
  "period": 30,
  "pinned": false,
  "secret": "<raw bytes, b64-standard>",
  "tags": [],
  "type": "totp",                  // totp | hotp | steam
  "updated_at": 0
}
```
(Keys shown sorted, as they MUST be serialized.)

**Optional fields.** `handle`, `deleted_at`, `note` and `order` are omitted at their default (`""`, `0`, `""`, `0`). A writer MUST NOT emit them at the default, so a vault that uses none of the v2 fields serializes to the same bytes as before they existed.

| Key | Type | Omitted when | Since |
|---|---|---|---|
| `handle` | string | `""` | v1 |
| `deleted_at` | integer, unix seconds, > 0 | `0` | v2 |
| `note` | string | `""` | v2 |
| `order` | integer >= 1 | `0` | v2 |

**Extension fields (forward compatibility).** A reader MUST keep every account key it does not recognize and write it back unchanged, in canonical form, sorted among the known keys. Extension values are canonical JSON values: string, integer (rule 4), boolean, null, array, or object, with duplicate keys rejected at every depth. An account that carries an extension field makes the vault version 2. The payload is AEAD-authenticated, so carrying an unknown field after a successful decrypt is safe. Implementations before 1.2.0 dropped unknown fields on write; that is why any key outside the v1 set forces version 2, which those implementations refuse to open. A change that an older reader could not carry safely (one that alters the meaning of existing fields) still needs a new version number.

A writer never invents keys: it emits the fields this spec defines plus the extension fields it read.

## Trash

A **trashed** account has `deleted_at > 0`.

- Trashing sets `deleted_at = now` and `updated_at = now`. Restoring clears `deleted_at` and sets `updated_at = now`. Purging removes the account object from the payload.
- A trashed account is excluded from code generation, lists, search, account resolution, exports, and duplicate detection. It is reachable only through the Trash views (`tess trash`, the app's Trash).
- It keeps its `handle`. Handle uniqueness covers trashed accounts, so a restore never collides.
- Nothing purges automatically. Emptying the Trash is an explicit action on both surfaces.
- Merging two vaults needs no extra rule: where an account exists on both sides the copy with the newer `updated_at` wins, and that copy carries its `deleted_at`. After a purge the other vault's copy returns on the next merge and is reported as added.

## Display order

Both surfaces list live accounts in the same order:

1. accounts with `order > 0`, ascending by `order`;
2. then accounts without one, by `issuer`, then `account`, compared case-insensitively with runs of digits compared as numbers (`Service2` before `Service10`);
3. ties by `id`.

A surface that groups pinned accounts first applies that grouping before this order. A manual reorder assigns `order` 1..n to every live account and leaves `updated_at` alone. Canonical serialization still sorts the array by `id`; position in the array carries no meaning.

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

Canonical shared path: `$XDG_DATA_HOME/tessera/vault.json` (default `~/.local/share/tessera/vault.json`; on Windows `%LOCALAPPDATA%\Tessera\vault.json`), overridable by `--vault` / `$TESSERA_VAULT`. The Mac app keeps its own vault in its sandbox container until the user points it at a shared one. For a shared vault the app obtains a user-granted **security-scoped bookmark** to the folder that holds the vault (for the default path, `~/.local/share/tessera/`) and persists it; it does not relocate the vault. Folder access is what lets the app use the lock and backup files below. A bookmark to the vault file alone (granted by app versions before 1.2.0) still opens the vault, without the lock.

## Writing the vault

Every writer, on every write:

1. Hold the lock (below) from before the read that the write is based on until the rename completes.
2. Read the current file and base the change on it. A writer MUST NOT write wraps or accounts it loaded earlier and kept in memory: it re-reads under the lock, opens the fresh payload with the DEK it holds, applies its change to the fresh account list, and reseals the fresh envelope. If the held DEK no longer opens the payload (the DEK was rotated elsewhere), the writer stops and asks for an unlock.
3. Copy the current file to `<vault>.bak` (mode 0600), replacing the previous backup.
4. Write the new envelope to a temporary file in the same directory (mode 0600, created exclusively), flush it to stable storage (`F_FULLFSYNC` on macOS, `fsync` elsewhere), rename it over the vault, then flush the directory.

**Lock.** An exclusive advisory lock on the sidecar `<vault>.lock` (`flock(LOCK_EX)` on Unix, `LockFileEx` on Windows). The sidecar is created 0600, is never deleted, and is opened without following a symlink. A waiter gives up after a bounded wait with an error that names the lock.

**Without folder access.** The sandboxed app with a file-only bookmark cannot reach the sidecar or the backup. It still follows step 2 immediately before its atomic replace, which narrows the window for a lost update to the time between that read and the rename. The app asks for folder access to close it.

**Backup generation.** `<vault>.bak` is the vault as it was before the last write. An implementation that cannot parse or authenticate the vault names the backup in its error and offers to open it; it never swaps it in silently. A DEK rotation deletes the backup after the new vault is durable, since the backup is sealed under the old DEK.

## DEK rotation

Rotation generates a new random DEK, reseals the payload under it, and rebuilds the wraps.

- A passphrase change rotates (`tess vault passwd`, the app's Change recovery passphrase). Without rotation, an old copy of the file plus the old passphrase would decrypt every later version of the vault.
- `tess vault rekey` rotates and keeps the passphrase.
- The legacy Secure Enclave migration rotates (see *Secure Enclave wrap*).
- The caller re-creates every wrap it can: the passphrase wrap from the passphrase it was given, its own Secure Enclave wrap from a fresh key. A wrap it cannot re-create (the app's Secure Enclave wrap, from the CLI; a wrap type it does not know) is removed, because it would wrap the old DEK. The surface that lost its wrap adds a new one at its next passphrase unlock. The caller reports which wraps were removed.
- Rotation never proceeds if it would leave the vault with no wrap.
- Adding an unlock method wraps the current DEK and does not rotate. Removing one deletes the wrap, and for a keychain-held Secure Enclave key also deletes the key item, which makes any copy of that wrap useless.

## Failure modes (negative tests, MUST reject)

- Tampered ciphertext or tag (AEAD auth failure).
- Wrong passphrase (argon2id -> wrong wrapKey -> DEK unseal fails).
- Truncated / malformed envelope, bad base64, base64url input, missing padding, base64 with embedded line breaks or non-zero trailing bits.
- Duplicate JSON keys in the envelope or in the decrypted payload, at any depth.
- Envelope keys in the wrong case (`"WRAPS"`).
- Zero wraps, or more than 16.
- Passphrase wrap with `kdf` other than `argon2id`, a salt that is not 16 bytes, or cost parameters outside the bounds (`t = 0`, `p = 0`, `p = 17`, `m < 8p`, `m > 1048576`, `m * t > 4194304`).
- A non-integer number, or a non-boolean `pinned`, in the decrypted payload.
- Unknown `version` or `aead`.

## Handle test vectors

`testvectors.json` and `canonical_edge.json` MUST cover handles so both cores stay byte-identical. The Go implementer regenerates these via `go/internal/vectorgen`; the coverage below is the contract.

- Accounts serialized WITH a `handle` field (canonical output includes it in sorted position between `folder` and `id`).
- Multi-word issuer → base from first char of each of the first two words (e.g. `Google Cloud` → `gc`).
- Digit-leading issuer → `x`-prefixed base (e.g. `1Password` → `x1p`).
- Empty issuer with an email account → base from the local part (e.g. issuer `""`, account `alice@example.com` → `al`).
- Collision chain producing `base`, `base2`, `base3` from three accounts with the same base.
- A user-edited handle occupying `base2` so the next auto-assignment skips to `base3` (proves auto-assign avoids taken handles and never renumbers).

## Version 2 test vectors

`testvectors.json` and `canonical_edge_v2.json` (two version 2 accounts in canonical form; `canonical_edge.json` stays the version 1 file) MUST also cover:

- Canonical output for an account with each of `deleted_at`, `note`, `order` alone and all three together (sorted positions: `deleted_at` between `created_at` and `digits`; `note` and `order` between `issuer` and `period`).
- An account whose v2 fields are all at their defaults serializes to the v1 bytes.
- The version rule: a payload with any v2 or extension field yields `version: 2`; clearing them yields `version: 1`.
- An extension field with a nested object, an array, a string with characters from the escaping rules, and a negative integer: parsed, re-emitted byte-identical, sorted among the known keys.
- Negative: a float in an extension field; a duplicate key inside an extension object; `"pinned": 1`; `"digits": 6.0`.
- A version 2 envelope that both cores cross-decrypt, and the envelope negatives listed under *Failure modes*.
- An envelope with an unknown top-level key and a wrap of unknown type: both survive a load and save.
- An envelope as Foundation's `JSONEncoder` writes it (indented, ` : ` after keys, `/` escaped as `\/`): every Tessera app release writes this form, and both cores MUST open it.
- `\b` and `\f` in a string value, written as two-character escapes.
