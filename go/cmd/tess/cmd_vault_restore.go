package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/ibrahemid/tessera/go/internal/store"
	"github.com/ibrahemid/tessera/go/internal/ui"
	"github.com/ibrahemid/tessera/go/internal/vault"
	"github.com/spf13/cobra"
)

var (
	errRestoreSource    = errors.New("name the file to restore, or pass --bak to restore <vault>.bak")
	errRestoreTwoSource = errors.New("name a file or pass --bak, not both")
	errRestoreDeclined  = errors.New("vault not replaced")
)

func newVaultRestoreCmd() *cobra.Command {
	var fromBak, yes bool
	cmd := &cobra.Command{
		Use:   "restore [file | --bak]",
		Short: "Replace the vault with a backup",
		Long: `Replace the vault with a backup: <vault>.bak with --bak, or a file such as
one from $TESSERA_BACKUP_DIR. The file must open with its passphrase, read from
$TESSERA_PASSPHRASE, the system secret store, or a prompt. The vault it replaces
becomes <vault>.bak. tess asks before replacing a vault; --yes skips the question.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			switch {
			case fromBak && len(args) == 1:
				return errRestoreTwoSource
			case !fromBak && len(args) == 0:
				return errRestoreSource
			}
			path, err := store.Resolve(vaultPath)
			if err != nil {
				return err
			}
			src, err := restoreSource(path, args, fromBak)
			if err != nil {
				return err
			}
			env, count, err := openRestoreSource(path, src)
			if err != nil {
				return err
			}
			if store.Exists(path) && !yes {
				if !stdinIsTTY() {
					return fmt.Errorf("replacing the vault at %s needs confirmation; run again with --yes", path)
				}
				if !confirmYN(ui.Sanitize(fmt.Sprintf("Replace the vault at %s with %s (%s)? [y/N] ", path, src, accountCount(count)))) {
					return errRestoreDeclined
				}
			}
			kept, err := replaceVault(cmd, path, env)
			if err != nil {
				return err
			}
			out(cmd, "Restored the vault at %s from %s (%s)", ui.Sanitize(path), ui.Sanitize(src), accountCount(count))
			if kept {
				out(cmd, "The vault it replaced is at %s", ui.Sanitize(store.BackupPath(path)))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&fromBak, "bak", false, "restore <vault>.bak, the vault as it was before the last write")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "replace the vault without asking")
	return cmd
}

func restoreSource(path string, args []string, fromBak bool) (string, error) {
	if fromBak {
		bak := store.BackupPath(path)
		if _, err := os.Lstat(bak); errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("there is no %s; tess writes it before each change to the vault", bak)
		}
		return bak, nil
	}
	src, err := filepath.Abs(args[0])
	if err != nil {
		return "", err
	}
	srcInfo, err := os.Stat(src)
	if errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("no file at %s", src)
	}
	if err != nil {
		return "", err
	}
	if vaultInfo, verr := os.Stat(path); verr == nil && os.SameFile(srcInfo, vaultInfo) {
		return "", fmt.Errorf("%s is the vault itself; name a backup file", src)
	}
	return src, nil
}

func openRestoreSource(path, src string) (*vault.Envelope, int, error) {
	env, err := store.Load(src)
	if err != nil {
		var withBak *store.BackupAvailableError
		if errors.As(err, &withBak) {
			err = withBak.Err
		}
		return nil, 0, fmt.Errorf("cannot read %s: %w", src, err)
	}
	tryOpen := func(pass string) (int, error) {
		dek, err := env.UnwrapDEK(pass)
		if err != nil {
			return 0, err
		}
		accts, err := env.OpenWithDEK(dek)
		return len(accts), err
	}
	if pass := os.Getenv("TESSERA_PASSPHRASE"); pass != "" {
		n, err := tryOpen(pass)
		if errors.Is(err, vault.ErrWrongPassphrase) {
			return nil, 0, &passphraseSourceError{source: "TESSERA_PASSPHRASE", path: src, err: err}
		}
		if err != nil {
			return nil, 0, fmt.Errorf("cannot open %s: %w", src, err)
		}
		return env, n, nil
	}
	if pass, _, hit, err := lookupStored(path, unlockLookupTimeout); err == nil && hit {
		if n, err := tryOpen(pass); err == nil {
			return env, n, nil
		}
	}
	if !stdinIsTTY() {
		return nil, 0, errNoUnlockTerminal
	}
	pass, err := promptPassphrase(ui.Sanitize(fmt.Sprintf("Passphrase for %s: ", src)))
	if err != nil {
		return nil, 0, err
	}
	n, err := tryOpen(pass)
	if err != nil {
		return nil, 0, fmt.Errorf("cannot open %s: %w", src, err)
	}
	return env, n, nil
}

func replaceVault(cmd *cobra.Command, path string, env *vault.Envelope) (bool, error) {
	lock, err := store.Acquire(path)
	if err != nil {
		return false, err
	}
	defer func() {
		if rerr := lock.Release(); rerr != nil {
			warn(cmd.ErrOrStderr(), userMessage(rerr))
		}
	}()
	_, lerr := store.Load(path)
	if _, err := saveVault(cmd.ErrOrStderr(), printNote(cmd.ErrOrStderr()), path, env, false); err != nil {
		return false, err
	}
	return lerr == nil, nil
}

func accountCount(n int) string {
	if n == 1 {
		return "1 account"
	}
	return fmt.Sprintf("%d accounts", n)
}
