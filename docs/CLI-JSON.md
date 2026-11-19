# tess JSON output

Commands that take `--json` print one JSON value on stdout, indented with two spaces and followed by a newline. Errors go to stderr as text and the exit status is non-zero.

## Compatibility

- A field listed here keeps its name, type and presence rule.
- New fields can appear in any object. Consumers must ignore fields they do not know.
- Key order is stable but not part of the contract. Read fields by name.
- Times are unix seconds (integers) unless the field says otherwise.
- `--json` never prints a secret, a setup key or an `otpauth://` link. `tess code --json` copies only with `-c`.

Presence:

- **always**: the field is in every object.
- **when set**: the field is left out when it is empty, zero or false.
- Any other rule is spelled out in the table.

## Commands

| Command | Prints |
|---|---|
| `tess code <query> --json` | a code row; an array of code rows when the query matches several accounts |
| `tess code --all --json`, `tess code --json` | an array of code rows, one per live account, in display order |
| `tess code <query> --at <time> --json` | the code row at that time |
| `tess list --json` | an array of list rows in display order |
| `tess show <query> --json` | an account record |
| `tess trash --json` | an array of trash rows, most recently trashed first; `[]` when the Trash is empty |
| `tess import <source> --json` | an import report |
| `tess vault status --json` | a vault status object |

Display order is the order `tess list` uses: pinned accounts first, then accounts with a manual order, then issuer and account name. Trashed accounts appear only in `tess trash --json`.

A `<query>` equal to an account's full `id` (case-sensitive) selects that account before any handle, issuer or account match, so `tess code --json -- <id>` names one account.

## Code row

| Field | Type | Presence | Meaning |
|---|---|---|---|
| `id` | string | always | the account's stable ID |
| `handle` | string | always | the short alias a query matches |
| `issuer` | string | always | |
| `account` | string | always | |
| `type` | string | always | `totp`, `hotp` or `steam` |
| `code` | string | always | the code, without grouping spaces |
| `next_code` | string | `totp` and `steam` | the code for the following time step |
| `expires_in` | integer | `totp` and `steam` | seconds until `code` stops being current |

For `hotp`, `code` is the code at the stored counter. `tess code <query> --next --json` advances and saves the counter first; `--all` and `--at` never advance it. `--at` takes RFC 3339 (`2026-03-01T09:00:00Z`) or unix seconds, and refuses `hotp` accounts.

`tess code <query> --json -c` copies `code`. For `totp` and `steam`, `--next` leaves the row unchanged and `-c --next` copies `next_code`.

## List row

| Field | Type | Presence | Meaning |
|---|---|---|---|
| `id` | string | always | |
| `handle` | string | always | |
| `issuer` | string | always | |
| `account` | string | always | |
| `type` | string | always | `totp`, `hotp` or `steam` |
| `folder` | string | when set | |
| `tags` | array of strings | when set | |
| `pinned` | boolean | always | |
| `has_note` | boolean | always | whether the account has a note; the note text is in `tess show --json` |
| `order` | integer | when set | the manual position, from 1 |

`--folder` and `--tag` filter the rows.

## Account record

| Field | Type | Presence | Meaning |
|---|---|---|---|
| `id` | string | always | |
| `handle` | string | always | |
| `issuer` | string | always | |
| `account` | string | always | |
| `type` | string | always | `totp`, `hotp` or `steam` |
| `algorithm` | string | always | `SHA1`, `SHA256` or `SHA512` |
| `digits` | integer | always | code length |
| `period` | integer | `totp` and `steam` | step length in seconds |
| `counter` | integer | `hotp`, including 0 | the stored counter |
| `folder` | string | when set | |
| `tags` | array of strings | when set | |
| `pinned` | boolean | always | |
| `note` | string | when set | |
| `order` | integer | when set | the manual position, from 1 |
| `created_at` | integer | always | |
| `updated_at` | integer | always | |

`--json` cannot be combined with `--secret`, `--uri` or `--qr`.

## Trash row

| Field | Type | Presence | Meaning |
|---|---|---|---|
| `id` | string | always | |
| `handle` | string | always | the alias `tess restore` takes |
| `issuer` | string | always | |
| `account` | string | always | |
| `type` | string | always | `totp`, `hotp` or `steam` |
| `deleted_at` | integer | always | when the account was moved to the Trash |

## Import report

| Field | Type | Presence | Meaning |
|---|---|---|---|
| `items` | array of import items | always | one per account read, then one per item that could not be read |
| `added` | integer | always | |
| `skipped` | integer | always | |
| `failed` | integer | always | |

Import item:

| Field | Type | Presence | Meaning |
|---|---|---|---|
| `status` | string | always | `added`, `skipped` or `failed` |
| `id` | string | `added` | the new account's ID |
| `handle` | string | `added` | the new account's alias |
| `issuer` | string | an account was read | |
| `account` | string | an account was read | |
| `type` | string | an account was read | |
| `source` | string | the input could not be read as an account | the file, line or QR code it came from |
| `reason` | string | `skipped` and `failed` | why |

The exit status is non-zero when nothing was added and an item failed. A run where every account was already in the vault exits 0.

## Vault status

| Field | Type | Presence | Meaning |
|---|---|---|---|
| `path` | string | always | the vault file |
| `exists` | boolean | always | |
| `size` | integer | when set | bytes |
| `modified` | string | when the file exists | RFC 3339, UTC |
| `unreadable` | string | when the file exists but cannot be read | why |
| `version` | integer | when the file was read | vault format version |
| `wraps` | array of strings | always | unlock methods, such as `passphrase` |
| `argon2` | array of objects | always | one per passphrase unlock: `m`, `t`, `p` (integers), `below_default` (boolean) |
| `previous` | string | when set | the previous-version file kept beside the vault |
| `backup_dir` | string | when set | `TESSERA_BACKUP_DIR` |
| `backup_dir_error` | string | when set | why backups are not written there |
| `newest_backup` | string | when set | date of the newest backup, `YYYY-MM-DD` |
| `remembered_in` | array of strings | always | secret stores holding the passphrase |
| `locked_in` | array of strings | always | secret stores that hold an entry but are locked |
