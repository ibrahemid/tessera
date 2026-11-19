# Tessera for Alfred

Searches Tessera accounts with the `2fa` keyword and copies or pastes their codes. Every list and code comes from the `tess` CLI at the moment you ask; the workflow keeps no codes on disk.

## Requirements

- Alfred with the Powerpack.
- `tess` in `/opt/homebrew/bin`, `/usr/local/bin`, `~/.local/bin` or `~/go/bin`, or its full path in the workflow configuration.
- A remembered passphrase. Run `tess vault remember` once in a terminal. The workflow never asks for the passphrase.

## Configuration

| Variable     | Effect                                                                                       |
| ------------ | -------------------------------------------------------------------------------------------- |
| `tess_path`  | Full path to `tess`. `~` expands to the home folder.                                         |
| `vault_file` | Full path to the vault, passed to `tess` as `TESSERA_VAULT`. `~` expands to the home folder. |

## Actions

| Action         | Key           |
| -------------- | ------------- |
| Copy Code      | Return        |
| Copy Next Code | Cmd+Return    |
| Paste Code     | Option+Return |

Accounts are looked up by their `id`.

Copy Code runs `tess code -c --clear 30 --json -- <id>`, and Copy Next Code adds `--next`. `tess` writes the code to the clipboard marked concealed and transient, keeps it off other devices, and clears it after 30 seconds if it is still there. The workflow never writes a code to the clipboard itself.

On a TOTP or Steam account, Cmd+Return copies the code of the following time step. On an HOTP account it advances the counter and copies the new code.

Paste Code reads the code with `tess code --json -- <id>` and pastes it through Alfred's Copy to Clipboard output marked transient, which Alfred's Clipboard History ignores.

A suggested command, such as `tess vault remember`, is copied through Alfred's Copy to Clipboard output.

## Build

```sh
node --test "test/*.test.mjs"
./build.sh
```

`build.sh` writes `.status/dist/Tessera.alfredworkflow` at the repository root.
