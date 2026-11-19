# Tessera for Raycast

Searches Tessera accounts and copies or pastes their codes. Every list and code comes from the `tess` CLI at the moment you ask; the extension keeps no codes on disk.

## Requirements

- `tess` in `/opt/homebrew/bin`, `/usr/local/bin`, `~/.local/bin` or `~/go/bin`, or its full path in the extension preferences.
- A remembered passphrase. Run `tess vault remember` once in a terminal. The extension never asks for the passphrase.

## Preferences

| Preference | Effect                                                                                       |
| ---------- | -------------------------------------------------------------------------------------------- |
| tess Path  | Full path to `tess`. `~` expands to the home folder.                                         |
| Vault File | Full path to the vault, passed to `tess` as `TESSERA_VAULT`. `~` expands to the home folder. |

## Actions

| Action         | Shortcut    |
| -------------- | ----------- |
| Copy Code      | Return      |
| Paste Code     | Cmd+Return  |
| Copy Next Code | Cmd+Shift+N |

Accounts are looked up by their `id`.

Copy Code runs `tess code -c --clear 30 --json -- <id>`, and Copy Next Code adds `--next`. `tess` writes the code to the clipboard marked concealed and transient, keeps it off other devices, and clears it after 30 seconds if it is still there. The extension never writes a code to the clipboard itself.

On a TOTP or Steam account, Copy Next Code copies the code of the following time step. On an HOTP account it becomes Advance Counter and Copy Code.

Paste Code reads the code with `tess code --json -- <id>` and pastes it with the Raycast `Clipboard.paste` API. The Raycast documentation gives `Clipboard.paste` one option, `closeMainWindow`; the option that keeps content out of Clipboard History, `concealed`, is documented only for `Clipboard.copy`.

## Build

```sh
npm install
npm run lint
npm test
npm run build
```

`npm run build` writes the bundle to `dist/`. `npm run dev` loads the extension into Raycast.
