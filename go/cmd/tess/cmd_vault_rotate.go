package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ibrahemid/tessera/go/internal/secretstore"
	"github.com/ibrahemid/tessera/go/internal/store"
	"github.com/ibrahemid/tessera/go/internal/ui"
	"github.com/spf13/cobra"
)

const rotationEffects = `The vault key changes, so <vault>.bak and the files in $TESSERA_BACKUP_DIR,
which open with the old key, are deleted and today's backup is written under the
new one. An unlock method tess cannot re-create, such as the Touch ID unlock of
the Tessera app, is removed; the app adds it back the next time it is unlocked
with the passphrase.`

func newVaultPasswdCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "passwd",
		Short: "Change the vault passphrase",
		Long: `Change the vault passphrase. The current passphrase is read from
$TESSERA_PASSPHRASE or prompted for; the replacement is read from
$TESSERA_NEW_PASSPHRASE or entered twice on a terminal.

` + rotationEffects + `

A passphrase remembered in a system secret store is replaced with the new one.
The kernel keyring entry is removed instead; run "tess vault remember" to store
the new passphrase there.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return rotateVault(cmd, true)
		},
	}
}

func newVaultRekeyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rekey",
		Short: "Replace the vault key and keep the passphrase",
		Long: `Replace the key that encrypts the vault and keep the passphrase. The
passphrase is read from $TESSERA_PASSPHRASE or prompted for.

` + rotationEffects,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return rotateVault(cmd, false)
		},
	}
}

type vaultChangedError struct{ path string }

func (e *vaultChangedError) Error() string {
	return fmt.Sprintf("the vault at %s got a new key while this command ran; run the command again", e.path)
}

func rotateVault(cmd *cobra.Command, changePassphrase bool) error {
	path, err := store.Resolve(vaultPath)
	if err != nil {
		return err
	}
	env, err := store.Load(path)
	if err != nil {
		return err
	}
	if os.Getenv("TESSERA_PASSPHRASE") == "" && !stdinIsTTY() {
		return errNoUnlockTerminal
	}
	old, err := readPassphrase("Current passphrase: ")
	if err != nil {
		return err
	}
	dek, err := env.UnwrapDEK(old)
	if err == nil {
		_, err = env.OpenWithDEK(dek)
	}
	if err != nil {
		return unlockFailure(path, old, err)
	}
	newPass := old
	if changePassphrase {
		if newPass, err = requireChangedVaultPassphrase(); err != nil {
			return err
		}
	}

	lock, err := store.Acquire(path)
	if err != nil {
		return err
	}
	removed, res, err := rotateLocked(cmd, path, dek, old, newPass)
	if rerr := lock.Release(); rerr != nil {
		warn(cmd.ErrOrStderr(), userMessage(rerr))
	}
	if err != nil {
		return err
	}

	if changePassphrase {
		out(cmd, "Passphrase changed")
	} else {
		out(cmd, "Vault key replaced")
	}
	for _, typ := range removed {
		out(cmd, "%s", removedWrapLine(typ, changePassphrase))
	}
	if res != nil && len(res.Removed) > 0 {
		out(cmd, "Deleted %s in %s that opened with the old key", backupCount(len(res.Removed)), ui.Sanitize(filepath.Dir(res.Path)))
	}
	if changePassphrase {
		replaceRememberedPassphrase(cmd, path, newPass)
		if os.Getenv("TESSERA_PASSPHRASE") != "" {
			warn(cmd.ErrOrStderr(), "$TESSERA_PASSPHRASE still holds the old passphrase; set it to the new one")
		}
	}
	return nil
}

func rotateLocked(cmd *cobra.Command, path string, dek []byte, old, newPass string) ([]string, *store.BackupResult, error) {
	fresh, err := store.Load(path)
	if err != nil {
		return nil, nil, err
	}
	if _, err := fresh.OpenWithDEK(dek); err != nil {
		return nil, nil, &vaultChangedError{path: path}
	}
	removed, err := fresh.RotatePassphrase(old, newPass)
	if err != nil {
		return nil, nil, unlockFailure(path, old, err)
	}
	res, err := saveVault(cmd.ErrOrStderr(), path, fresh, true)
	if err != nil {
		return nil, nil, err
	}
	return removed, res, nil
}

func removedWrapLine(typ string, changePassphrase bool) string {
	switch typ {
	case "secure-enclave":
		return "Removed the Touch ID unlock of the Tessera app; the app adds it back the next time you unlock it with the passphrase"
	case "passphrase":
		if changePassphrase {
			return "Removed every other passphrase; only the new one opens the vault"
		}
		return "Removed every other passphrase; only the one you entered opens the vault"
	}
	return fmt.Sprintf("Removed the %s unlock; add it again from the app that created it", ui.Sanitize(typ))
}

func backupCount(n int) string {
	if n == 1 {
		return "1 older backup"
	}
	return fmt.Sprintf("%d older backups", n)
}

func replaceRememberedPassphrase(cmd *cobra.Command, path, newPass string) {
	probe, cancel := context.WithTimeout(cmd.Context(), storeProbeTimeout)
	found := secretstore.Locate(probe, path)
	cancel()
	w := cmd.ErrOrStderr()
	for _, p := range found {
		name := p.Backend.Name()
		switch {
		case p.Present && p.Backend.ID() == kernelKeyringID:
			ctx, stop := interactiveContext(cmd.Context())
			err := p.Backend.Delete(ctx, path)
			stop()
			if err != nil {
				warn(w, fmt.Sprintf("the %s still holds the old passphrase and could not be cleared (%s); run `tess vault forget`", name, userMessage(err)))
				continue
			}
			out(cmd, "Removed the old passphrase from the %s; run `tess vault remember` to store the new one", ui.Sanitize(name))
		case p.Present:
			ctx, stop := interactiveContext(cmd.Context())
			err := p.Backend.Store(ctx, path, newPass, 0)
			stop()
			if err != nil {
				warn(w, fmt.Sprintf("the %s still holds the old passphrase and could not be updated (%s); run `tess vault remember` to replace it", name, userMessage(err)))
				continue
			}
			out(cmd, "Replaced the passphrase stored in the %s", ui.Sanitize(name))
		case p.Locked:
			warn(w, fmt.Sprintf("the %s is locked, so tess could not check it for the old passphrase; if it holds one, unlock it and run `tess vault remember`", name))
		case p.Err != nil:
			warn(w, fmt.Sprintf("could not check the %s for the old passphrase (%s)", name, userMessage(p.Err)))
		}
	}
}
