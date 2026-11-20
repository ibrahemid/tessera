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

The two CSV formats are recognized by their exact header line:
`Title,URL,Username,Password,Notes,OTPAuth` for Apple Passwords and
`Title,Url,Username,Password,OTPAuth,Favorite,Archived,Tags,Notes` for
1Password. A CSV with any other header is reported as unrecognized.

Rows without a 2FA code are skipped: a password export usually holds mostly
logins, and only the items with a one-time-password field become accounts.

An encrypted export is refused by name rather than half-read. Tessera does not
ask for the other app's password. Export again without encryption and retry.

`tess import` also takes `otpauth://` URIs (`--otpauth`), `otpauth-migration://`
URIs (`--migration`), text files with one URI or base32 setup key per line, and
QR images (`--qr`, every code in the image). `tess add --screen` reads a QR code
off the screen on macOS and Linux.

### What an import keeps

Every source gives the type, issuer, account name, secret, algorithm, digits,
and period or counter.

| Source | Note | Folder | Tags | Pinned |
|--------|------|--------|------|--------|
| Aegis | kept | first group | other groups | from favorite |
| 2FAS Auth | dropped | its group | dropped | dropped |
| Raivo OTP | dropped | dropped | dropped | kept |
| andOTP | dropped | dropped | kept | dropped |
| Stratum / Authenticator Pro | dropped | first category | other categories | dropped |
| Bitwarden and Bitwarden Authenticator | kept | folder, else first collection | other collections | from favorite |
| Proton Authenticator | kept | dropped | dropped | dropped |
| Apple Passwords CSV | kept | dropped | dropped | dropped |
| 1Password `.1pux` | kept | vault name | kept | from favorite |
| 1Password CSV | kept | dropped | kept, split on `;` | from favorite |
| FreeOTP+, Ente Auth, Google Authenticator | dropped | dropped | dropped | dropped |

Folder and tag names are cleaned like any other text. When an account sits in
several groups, the first in the file's order becomes its folder and the rest
become tags; Bitwarden puts the item's folder before its collections. Blank
names, repeated names and groups the file does not define are dropped.

An account with the same type, issuer, account name (case-insensitive) and
secret as a live account in the vault, or as one earlier in the same import, is
skipped and listed as already in the vault. Each item that fails is listed with its source and the reason, and the
rest of the batch is still imported. `tess import` exits with an error only
when nothing was imported and something failed. `--json` writes the same
report as JSON.

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

An account a format cannot store is named on stderr and left out. Proton
Authenticator has no HOTP entry. Google Authenticator's transfer format has no
Steam entry and stores only 6- and 8-digit codes and, for TOTP, a 30-second
period. Those accounts are never rewritten to fit: a changed period or digit
count generates wrong codes. The transfer codes also leave out an account with
an empty secret.

`google-migration` writes one QR PNG per batch (ten accounts at most, and fewer
when they are large), named `google-migration-1-of-N.png`. Scan them one at a
time in Google Authenticator's account import flow. It needs `--out`, as an
image cannot go to stdout.

### What an export carries

| Format | Note | Folder | Tags | Pinned |
|--------|------|--------|------|--------|
| `aegis` | yes | as a group | no | as favorite |
| `2fas` | no | as a group | no | no |
| `bitwarden` | yes | yes | no | as favorite |
| `proton` | yes | no | no | no |
| `andotp` | no | no | yes | no |
| `apple-csv` | yes | no | no | no |
| `google-migration` | no | no | no | no |

`tess import` of one of these files restores every field marked here.

`apple-csv` leaves out an account whose title or username starts with `=`, `+`,
`-` or `@`, and drops a note that starts with one of them, since a spreadsheet
runs such a cell as a formula. Both are named on stderr.

The other export routes:

- `tess export --uri` prints otpauth URIs, all accounts or the one you name.
- `tess export --secret` prints base32 secrets.
- `tess export --qr <dir>` writes a QR PNG per account, named
  `<issuer>-<account>-<n>.png`.
- `tess export --file <path>` copies the encrypted vault file as it is, without
  unlocking it. It is the only one of these that is safe to keep. It holds every
  account, the Trash included, and every unlock method of the vault.

Every route except `--file` leaves the Trash out.

### Files and overwrites

Every file an export writes is readable and writable by you only (mode 0600),
and a directory it creates for `--qr` or `--out` is mode 0700. Each file is
written to a temporary file in the same directory and renamed into place.

An export never replaces something that already exists unless you pass
`--force`, which covers `--file`, `--qr` and `--out`. Without it the run stops
with `<path> already exists; pass --force to replace it`, and a run that would
write several files checks them all first, so a refused run writes nothing. A
symlink at the destination is never followed; `--force` replaces the link
itself.

## Between two Tessera vaults

`tess merge <vault-file>` folds another vault, or an encrypted copy written by
`tess export --file`, into this one. It carries whole accounts: notes, folders,
tags, pin state and Trash state come along.

```sh
tess merge ~/backup.json             # reads the source, writes this vault
tess merge --two-way ~/backup.json   # writes both files
```

When the source has a different passphrase, tess asks for it, or reads
`$TESSERA_MERGE_PASSPHRASE`.

Each account in the source lands in one of these outcomes:

- Same account in both, source copy edited later: the source copy replaces this
  vault's copy, including a move to the Trash or a restore. This vault keeps its
  own handle for it. Counted as moved to the Trash when it takes a live account
  into the Trash, and as updated otherwise.
- Same account in both, source copy not newer: nothing changes, apart from the
  HOTP counter rule below. Two copies edited in the same second count as the
  same. Counted as unchanged.
- An account this vault does not have, but a live account here has the same
  type, issuer, account name and secret: skipped and counted as already
  present. A trashed account never counts as a match.
- A copy that fails validation, new or newer: left out and counted as invalid.
- Anything else: added.

An HOTP counter never moves back. When both copies are HOTP accounts with the
same secret, the merged account keeps the larger counter: a newer source copy
keeps this vault's counter when that is larger, and a source copy that is not
newer but has a larger counter raises this vault's counter, counted as updated.

The merge prints the counts that are not zero, such as
`Merged backup.json: 1 added, 1 unchanged, 2 already present`. A two-way merge
prints one set for this vault (`here`) and one for the source (`in the source`).

An account you emptied from one vault's Trash is new to that vault, so the next
merge brings it back from the other.

After the merge, a handle that two accounts share stays with the first and the
other gets a new one. A vault is written only when something was added, updated
or moved to the Trash.

`--two-way` applies the same rule in both directions, so two vaults that drifted
apart end up holding the same accounts. It locks both files, writes this vault
first and the source second, so a failed write here leaves the source as it was.
It refuses a source that is the same file as this vault. Without `--two-way`
the source is opened read-only.

The exact rules, and the test vectors that pin them, are in
[`spec/vault-format.md`](../spec/vault-format.md#merging-vaults).
