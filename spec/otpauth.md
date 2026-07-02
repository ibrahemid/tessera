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
