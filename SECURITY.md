# Security policy

## Reporting a vulnerability

Report privately via [GitHub Security Advisories](../../security/advisories/new)
or email security@ibrahemid.com. Do not open a public issue for anything
exploitable.

You'll get an acknowledgment within 48 hours and a fix or a status update within
7 days. Credit in the release notes unless you prefer otherwise.

## Scope

- The `tess` CLI and Go core (`go/`)
- The macOS app and Swift core (`swift/`)
- The vault format and interop contract (`spec/`)

Out of scope: the marketing site, and attacks requiring root or physical access
to an unlocked machine.

## Design notes for researchers

- Vault: random 256-bit DEK, XChaCha20-Poly1305 payload, DEK wrapped per unlock
  method (argon2id passphrase wrap; Secure Enclave wrap on macOS). See
  `spec/vault-format.md`.
- Crypto primitives come from `golang.org/x/crypto` and CryptoKit; nothing
  hand-rolled. Both implementations must stay byte-identical against
  `spec/testvectors.json`.
- Secrets exist as raw bytes only inside the encrypted vault; base32/otpauth
  appear only at import/export boundaries. Cleartext exports (`tess export
  --uri/--secret/--qr`) are explicit user actions and warn in the help text.
