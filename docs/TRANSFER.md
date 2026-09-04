# Moving accounts in and out

Tessera reads another app's export file and writes one back. Nothing here talks
to a network: an export is a file you produce in the other app, hand to `tess`,
and then delete.

Every export file below holds your secrets in cleartext. Import it, then remove
it (`rm` the file, and empty the trash).

## Into Tessera

`tess import <path>` detects the format from the file's content, not its name,
so the same command reads every source. `tess add <path>` does the same for a
single file, and `cat file | tess import -` reads a pipe.

| From | What to export | Then |
|------|----------------|------|
| Aegis Authenticator | Settings > Import/Export > Export, with encryption unticked | `tess import aegis-export.json` |
| 2FAS Auth | the backup file, with the backup password turned off | `tess import 2fas-backup` |
| Raivo OTP | the JSON export | `tess import raivo-export.json` |
| andOTP | the plain (unencrypted) backup | `tess import andotp-backup.json` |
| FreeOTP+ | the JSON backup | `tess import freeotp-backup.json` |
| Stratum / Authenticator Pro | Settings > Backup > Export unencrypted (`.stratum`) | `tess import backup.stratum` |
| Bitwarden Authenticator | the JSON export (its export offers JSON or CSV; pick JSON) | `tess import bitwarden-authenticator.json` |
| Bitwarden (password manager) | the unencrypted `.json` export, not "Password protected" | `tess import bitwarden_export.json` |
| Proton Authenticator | the JSON export, without a password | `tess import proton-export.json` |
| Ente Auth | Export > plain text (the `.txt` file, not the encrypted `.json`) | `tess import ente-auth-codes.txt` |
| Apple Passwords | the CSV export (Apple's exports are never encrypted) | `tess import passwords.csv` |
| 1Password | the `.1pux` export, or the CSV export | `tess import export.1pux` |
| Google Authenticator | the transfer QR code, saved as a screenshot | `tess import qr.png` |

Rows without a 2FA code are skipped: a password export usually holds mostly
logins, and only the items with a one-time-password field become accounts.

An encrypted export is refused by name rather than half-read — Tessera does not
ask for the other app's password. Export again without encryption and retry.

`tess import` also takes `otpauth://` URIs, `otpauth-migration://` URIs, bare
base32 setup keys, and QR images; `tess add --screen` reads a QR code off the
screen on macOS.

## Out of Tessera

`tess export --format <name>` rewrites the vault in another app's export format.
Without `--out` it writes to stdout; with `--out <file>` it writes one file, and
`--out <directory>` for a format that produces several.

| Format | Reads it | Command |
|--------|----------|---------|
| `aegis` | Aegis Authenticator | `tess export --format aegis --out aegis.json` |
| `2fas` | 2FAS Auth | `tess export --format 2fas --out 2fas.json` |
| `bitwarden` | Bitwarden Authenticator | `tess export --format bitwarden --out bitwarden.json` |
| `proton` | Proton Authenticator | `tess export --format proton --out proton.json` |
| `andotp` | andOTP and the apps that read its backups | `tess export --format andotp --out andotp.json` |
| `apple-csv` | Apple Passwords | `tess export --format apple-csv --out passwords.csv` |
| `google-migration` | Google Authenticator, by scanning | `tess export --format google-migration --out ./qr` |

An account a format cannot store is named on stderr and left out. Two cases:
Proton Authenticator has no HOTP entry, and Google Authenticator's transfer
format carries neither Steam accounts, 7-digit codes, nor a period other than 30
seconds. Those accounts are never rewritten to fit — a changed period or digit
count generates wrong codes.

`google-migration` writes one QR PNG per batch (ten accounts at most, and fewer
when they are large). Scan them one at a time in Google Authenticator's account
import flow.

The other export routes are unchanged: `tess export --uri` prints otpauth URIs,
`tess export --secret` prints base32 secrets, `tess export --qr <dir>` writes a
QR image per account, and `tess export --file <path>` writes an encrypted copy
of the whole vault, which is the only one of these that is safe to keep.

## Between two Tessera vaults

`tess merge <vault-file>` folds another vault (or an encrypted backup written by
`tess export --file`) into this one. An account present in both keeps whichever
copy was edited last; an account already present by content is skipped; the rest
are added.

```sh
tess merge ~/backup.json             # reads the source, writes this vault
tess merge --two-way ~/backup.json   # writes both files
```

`--two-way` applies the same rule in both directions, so two vaults that drifted
apart end up holding the same accounts. Each vault keeps its own handles. When
the same account was edited on both sides in the same second, neither copy wins
and neither file changes.
