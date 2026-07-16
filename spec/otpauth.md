# Tessera OTP & otpauth Spec

Both cores MUST implement these identically. Vectors in `testvectors.json`.

## TOTP (RFC 6238)
`T = floor((unixNow - T0) / period)`, T0 = 0, default period 30s. `code = HOTP(K, T)`. HMAC = SHA1 (default) | SHA256 | SHA512. Digits 6 (default) | 7 | 8, zero-padded. Remaining seconds = `period - (unixNow mod period)`.

## HOTP (RFC 4226)
`HOTP(K, C) = Truncate(HMAC(K, C_8byte_bigendian))`. Dynamic truncation:
```
offset  = HMAC[len-1] & 0x0f
binCode = (HMAC[offset]&0x7f)<<24 | (HMAC[offset+1]&0xff)<<16
        | (HMAC[offset+2]&0xff)<<8 | (HMAC[offset+3]&0xff)
code    = binCode mod 10^digits        // zero-padded
```
Counter persisted; incremented on copy/view (CLI flag controls).

## Steam Guard
RFC 6238 base (HMAC-SHA1, period 30, T0 0). Differences:
- Secret supplied as **base64** (decode to raw bytes; store raw in vault).
- 5-character code. Alphabet `23456789BCDFGHJKMNPQRTVWXY` (26 chars).
- From the 31-bit dynamic-truncation integer `fullcode`: `for i in 0..4: out += ALPHABET[fullcode % 26]; fullcode /= 26`.

## base32 (RFC 4648 §6)
Alphabet `A-Z2-7`, case-insensitive (uppercase-normalize), `=` padding (strip then re-pad on decode), strip whitespace. Used ONLY at otpauth import/export boundaries; vault stores raw bytes.

## otpauth:// URI (Key Uri Format)
`otpauth://TYPE/LABEL?PARAMS`, TYPE = `totp|hotp|steam` (`steam` non-standard, see below). LABEL = `issuer:account` (URL-encoded; `issuer:` prefix optional). Params:

| param | required | default |
|---|---|---|
| `secret` | yes | base32 (RFC4648), padding optional |
| `issuer` | recommended | must match label prefix if both present |
| `algorithm` | no | `SHA1` |
| `digits` | no | `6` |
| `period` | no (totp) | `30` |
| `counter` | yes (hotp) | — |

Steam in otpauth (both directions MUST match):
- Parse: type `steam`, or the heuristic `otpauth://totp/...` with issuer `Steam` (case-insensitive), yields a Steam account. Steam digits default to `5`; a `digits` param other than `5` is rejected. Non-integer `digits`/`period` are rejected for all types.
- Emit: Steam accounts export as `otpauth://steam/...` with `digits=5` (compatible with Aegis and other steam-aware clients).

Emit (export): URL-encode label and issuer; secret base32 no-pad; include issuer param.

## otpauth-migration:// (Google Authenticator export)
`otpauth-migration://offline?data=<URL-encoded base64 of protobuf>`. Decode: URL-decode -> base64-decode -> protobuf `MigrationPayload`.

```proto
message MigrationPayload {
  enum Algorithm  { ALGORITHM_UNSPECIFIED=0; ALGORITHM_SHA1=1; ALGORITHM_SHA256=2; ALGORITHM_SHA512=3; ALGORITHM_MD5=4; }
  enum DigitCount { DIGIT_COUNT_UNSPECIFIED=0; DIGIT_COUNT_SIX=1; DIGIT_COUNT_EIGHT=2; }
  enum OtpType    { OTP_TYPE_UNSPECIFIED=0; OTP_TYPE_HOTP=1; OTP_TYPE_TOTP=2; }
  message OtpParameters {
    bytes secret=1; string name=2; string issuer=3;
    Algorithm algorithm=4; DigitCount digits=5; OtpType type=6; int64 counter=7;
  }
  repeated OtpParameters otp_parameters=1;
  int32 version=2; int32 batch_size=3; int32 batch_index=4; int32 batch_id=5;
}
```
`secret` is RAW bytes (NOT base32). Multi-QR exports share `batch_id`, indexed `batch_index` of `batch_size`; merge before completing import. Map `ALGORITHM_MD5` -> reject with a clear error (not supported by RFC TOTP clients).

## Input detection
Both cores classify a text payload by the first matching rule, in order (Go `internal/detect`, Swift `InputDetect`, byte-identical):

1. Prefix `otpauth-migration://` (case-insensitive scheme) -> Google Authenticator migration parse (multi-account).
2. Prefix `otpauth://` (case-insensitive scheme) -> single-account otpauth parse.
3. App-export JSON: first non-whitespace byte `[` -> Raivo, `{` -> Aegis (`db` key) / 2FAS (`services`/`servicesEncrypted` key) -> existing importers.
4. Bare base32 setup key (guardrail below) -> TOTP, defaults SHA1 / 6 digits / period 30, empty issuer and account.

No rule matches -> `invalid`.

Multiline input: split on line breaks, classify each non-empty line independently by the same precedence. A single line is the one-line case of this rule.

Wrapped-URI repair (runs before the per-line split). Textareas and mail clients hard-wrap long URIs, so a single URI arriving with embedded line breaks is one URI, not a batch. Repair applies only when ALL of: the trimmed input classifies as `otpauth` or `migration` by prefix; it contains at least one line break; the substring `otpauth` occurs exactly once case-insensitively; and the first non-empty line ALONE fails to parse as that kind (i.e. it is a true fragment — a complete first line means the input is a batch and per-line semantics stand). Then strip ALL whitespace from the whole input and parse the result as that single URI. On success the repaired URI is the entire result; on failure, fall back to the per-line rule unchanged.

Base32 setup-key guardrail (rule 4). Prevents prose (e.g. `hello world`) from being read as a key. After stripping ASCII spaces and `-`, the input qualifies as a setup key only if it is a single token matching `^[A-Za-z2-7]+$` case-insensitively, length >= 16 chars (>= 10 secret bytes), and it decodes cleanly under the lenient base32 rules above (§ base32). Otherwise it is not a setup key and falls through to `invalid`.

Partial-failure semantics. Batch inputs (multiline, multi-file, multi-QR) import every item that parses. Each failure is recorded per item (source, line/file index, reason) and never aborts the batch.

## Supported image formats
The app decodes images via ImageIO: PNG, JPEG, HEIC, WebP, TIFF, GIF, BMP. The CLI decodes via Go `x/image`: PNG, JPEG, WebP, TIFF, BMP. HEIC is app-only (no cgo in the security-audited Go module). Multiple QR codes in one image are all decoded; each decoded payload is classified via the text precedence above.

## Detection test table
Canonical classification cases. Both suites port these; `kind` is one of `migration | otpauth | export-json | setup-key | invalid`.

| input | kind | notable |
|---|---|---|
| `ZB573K4APD63E6RLD3WAHI3QFZ35RLEP` | setup-key | 32 chars; SHA1/6/30 defaults; empty issuer+account |
| `zb573k4a pd63e6rl d3wahi3q fz35rlep` | setup-key | spaces stripped; same key as above |
| `zb573k4a-pd63e6rl-d3wahi3q-fz35rlep` | setup-key | dashes stripped; same key |
| `GEZDGNBV` | invalid | 8 chars < 16 min |
| `hello world` | invalid | two tokens; not `^[A-Za-z2-7]+$` |
| `otpauth://totp/Example:alice@google.com?secret=JBSWY3DPEHPK3PXP&issuer=Example` | otpauth | TOTP; secret base32 |
| `otpauth://hotp/Bank:ops?secret=JBSWY3DPEHPK3PXP&counter=5&digits=8` | otpauth | HOTP; counter=5 |
| `otpauth://steam/Steam:me?secret=ONSWG4TFOQ&digits=5` | otpauth | steam; digits=5 |
| `otpauth-migration://offline?data=CjEKCkhlbGxvId6tvu8SGEV4YW1wbGU6YWxpY2VAZ29vZ2xlLmNvbRoHRXhhbXBsZSABKAEwAhABGAEgACjr4JKkBg%3D%3D` | migration | 1 account (Example:alice) |
| `[{ "issuer": "GitHub", "account": "john@example.com", "secret": "JBSWY3DPEHPK3PXP", "algorithm": "SHA1", "digits": "6", "kind": "TOTP", "timer": "30", "counter": "0" }]` | export-json | Raivo (`[` lead) |
| `{ "schemaVersion": 4, "services": [{ "name": "GitHub", "secret": "JBSWY3DPEHPK3PXP", "otp": { "tokenType": "TOTP" } }] }` | export-json | 2FAS (`services` key) |
| `{ "version": 1, "db": { "version": 3, "entries": [] } }` | export-json | Aegis (`db` key) |
| `otpauth://totp/A?secret=JBSWY3DPEHPK3PXP`<br>`hello world`<br>`ZB573K4APD63E6RLD3WAHI3QFZ35RLEP` | otpauth, invalid, setup-key | per-line: line1 otpauth, line2 invalid, line3 setup-key |
| `otpauth://totp/Demo:reviewer@example.com?`<br>`secret=JBSWY3DPEHPK3PXP&issuer=Demo` | otpauth | wrapped-URI repair: whitespace stripped, parses as one account |
| `otpauth://totp/A?`<br>`hello world` | otpauth + invalid | repair join fails (`hello world` is not query), falls back per-line: both lines error |
| `otpauth://totp/A?secret=JBSWY3DPEHPK3PXP`<br>`ZB573K4APD63E6RLD3WAHI3QFZ35RLEP` | otpauth, setup-key | no repair: first line parses alone, so per-line batch (2 accounts) |
| `` (empty) | invalid | no non-whitespace |
| `   \t  ` (whitespace only) | invalid | no non-whitespace |
| `SGVsbG8gd29ybGQhISE=` | invalid | base64 not base32 (`=` mid/tail, non-`A-Z2-7`) |
