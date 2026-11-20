package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/secretstore"
	"github.com/ibrahemid/tessera/go/internal/store"
	"github.com/ibrahemid/tessera/go/internal/ui"
	"github.com/ibrahemid/tessera/go/internal/vault"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func newVaultCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "vault", Short: "Manage the vault"}
	cmd.AddCommand(
		newVaultInitCmd(),
		newVaultPasswdCmd(),
		newVaultRekeyCmd(),
		newVaultBackupCmd(),
		newVaultRestoreCmd(),
		newVaultResetCmd(),
		newVaultRememberCmd(),
		newVaultForgetCmd(),
		newVaultStatusCmd(),
	)
	return cmd
}

func newVaultInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Create a new empty encrypted vault",
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := store.Resolve(vaultPath)
			if err != nil {
				return err
			}
			if store.Exists(path) {
				return fmt.Errorf("vault already exists at %s", path)
			}
			pass, err := requireNewVaultPassphrase()
			if err != nil {
				return err
			}
			env, err := vault.Seal([]account.Account{}, pass)
			if err != nil {
				return err
			}
			if _, err := saveVault(cmd.ErrOrStderr(), printNote(cmd.ErrOrStderr()), path, env, false); err != nil {
				return err
			}
			out(cmd, "Created vault at %s", path)
			return nil
		},
	}
}

func newVaultResetCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "reset",
		Short: "Delete the vault file so a fresh vault can be created",
		Long: `Delete the vault at its resolved path (respects --vault and $TESSERA_VAULT).
This is irreversible and removes every account. Run "tess vault init" afterward
to create a new empty vault. Use --force to skip the confirmation in scripts.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := store.Resolve(vaultPath)
			if err != nil {
				return err
			}
			if !store.Exists(path) {
				return fmt.Errorf("no vault at %s", path)
			}
			if !force {
				name := filepath.Base(path)
				if !term.IsTerminal(int(os.Stdin.Fd())) {
					return fmt.Errorf("refusing to delete %s without confirmation; re-run with --force", path)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "This deletes the vault at %s and every account in it.\nType the vault filename (%s) to confirm: ", path, name)
				line, err := bufio.NewReader(os.Stdin).ReadString('\n')
				if err != nil {
					return fmt.Errorf("read confirmation: %w", err)
				}
				if strings.TrimSpace(line) != name {
					return fmt.Errorf("confirmation did not match; vault not deleted")
				}
			}
			if err := os.Remove(path); err != nil {
				return fmt.Errorf("delete vault: %w", err)
			}
			out(cmd, "Deleted vault at %s", path)
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "skip the confirmation prompt")
	return cmd
}

// isKeychainWording keeps the macOS remember, forget and status output in the
// login keychain wording it has always used.
var isKeychainWording = runtime.GOOS == "darwin"

// errNegativeTTL rejects a negative --for.
var errNegativeTTL = errors.New("--for takes a duration of zero or more, such as 8h")

func newVaultRememberCmd() *cobra.Command {
	var ttl time.Duration
	cmd := &cobra.Command{
		Use:   "remember",
		Short: "Store the vault passphrase in the system secret store",
		Long: `Store the vault passphrase in the system secret store so tess stops
prompting: the login keychain on macOS, the Secret Service on Linux with the
kernel keyring as a fallback, and Windows Credential Manager on Windows. The
passphrase is verified against the vault before it is stored.

The kernel keyring keeps the entry until reboot; --for sets an earlier expiry.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if ttl < 0 {
				return errNegativeTTL
			}
			b, err := storeBackend()
			if err != nil {
				return err
			}
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
			pass, err := readPassphrase("Vault passphrase: ")
			if err != nil {
				return err
			}
			if err := openWithPassphrase(env, path, pass); err != nil {
				return err
			}
			ctx, stop := interactiveContext(cmd.Context())
			defer stop()
			if err := b.Store(ctx, path, pass, ttl); err != nil {
				return err
			}
			out(cmd, "Stored passphrase for %s in the %s%s", path, b.Name(), storedUntil(b, ttl))
			return nil
		},
	}
	cmd.Flags().DurationVar(&ttl, "for", 0, "expire the stored passphrase after this long, such as 8h (kernel keyring only)")
	return cmd
}

// storeBackend picks the secret store a passphrase is written to.
func storeBackend() (secretstore.Backend, error) {
	ctx, cancel := context.WithTimeout(context.Background(), storeProbeTimeout)
	defer cancel()
	return secretstore.ForStore(ctx)
}

// storedUntil is the expiry note after a stored passphrase: the expiry time
// for a ttl, or the reboot that clears the kernel keyring.
func storedUntil(b secretstore.Backend, ttl time.Duration) string {
	switch {
	case ttl > 0:
		return " until " + now().Add(ttl).Format("2006-01-02 15:04")
	case b.ID() == kernelKeyringID:
		return " until reboot"
	}
	return ""
}

func newVaultForgetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "forget",
		Short: "Remove the vault passphrase from the system secret store",
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := store.Resolve(vaultPath)
			if err != nil {
				return err
			}
			ctx, stop := interactiveContext(cmd.Context())
			defer stop()
			removed, err := secretstore.Forget(ctx, path)
			if isKeychainWording {
				if err != nil {
					return err
				}
				out(cmd, "Removed any keychain entry for %s", path)
				return nil
			}
			for _, name := range removed {
				out(cmd, "Removed the stored passphrase for %s from the %s", path, name)
			}
			if len(removed) == 0 && err == nil {
				out(cmd, "No stored passphrase for %s", path)
			}
			return err
		},
	}
}

func newVaultStatusCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show the vault path, file details, and unlock methods",
		Long: `Report the resolved vault path, whether the file exists, its size and
modification time, the format version, the unlock methods in the envelope
header and the argon2id cost of each passphrase unlock, whether <vault>.bak
exists, the $TESSERA_BACKUP_DIR folder and the date of its newest backup, and
which system secret store holds the passphrase. Nothing is decrypted and the
passphrase is never shown.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := store.Resolve(vaultPath)
			if err != nil {
				return err
			}
			st := readVaultStatus(path)
			ctx, cancel := context.WithTimeout(cmd.Context(), storeProbeTimeout)
			defer cancel()
			if asJSON {
				st.RememberedIn, st.LockedIn = []string{}, []string{}
				for _, p := range secretstore.Locate(ctx, path) {
					switch {
					case p.Present:
						st.RememberedIn = append(st.RememberedIn, p.Backend.Name())
					case p.Locked:
						st.LockedIn = append(st.LockedIn, p.Backend.Name())
					}
				}
				out(cmd, "%s", mustJSON(st))
				return nil
			}
			for _, line := range st.lines() {
				out(cmd, "%s", line)
			}
			out(cmd, "%s", secretStatusLine(ctx, path, isKeychainWording))
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "output JSON")
	return cmd
}

// secretStatusLine reports what the secret stores hold for path. The keychain
// wording is the macOS form; the other form names each store.
func secretStatusLine(ctx context.Context, path string, keychainWording bool) string {
	var present, locked []string
	var lookupErr error
	found := secretstore.Locate(ctx, path)
	for _, p := range found {
		switch {
		case p.Present:
			present = append(present, p.Backend.Name())
		case p.Locked:
			locked = append(locked, p.Backend.Name())
		case p.Err != nil && lookupErr == nil:
			lookupErr = p.Err
		}
	}
	if keychainWording {
		switch {
		case len(present) > 0:
			return "Keychain: entry present"
		case len(locked) > 0:
			return "Keychain: unknown (" + ui.Sanitize(userMessage(secretstore.ErrLocked)) + ")"
		case lookupErr != nil:
			return "Keychain: unknown (" + ui.Sanitize(userMessage(lookupErr)) + ")"
		case len(found) > 0:
			return "Keychain: no entry"
		}
		if _, err := secretstore.ForStore(ctx); err != nil {
			return "Keychain: unknown (" + ui.Sanitize(userMessage(err)) + ")"
		}
		return "Keychain: no entry"
	}
	switch {
	case len(present) > 0:
		return "Secrets:  entry present (" + ui.Sanitize(strings.Join(present, ", ")) + ")"
	case len(locked) > 0:
		return "Secrets:  locked (" + ui.Sanitize(strings.Join(locked, ", ")) + ")"
	case lookupErr != nil:
		return "Secrets:  unknown (" + ui.Sanitize(userMessage(lookupErr)) + ")"
	}
	b, err := secretstore.ForStore(ctx)
	if err == nil {
		return "Secrets:  no entry (stores in " + ui.Sanitize(b.Name()) + ")"
	}
	return "Secrets:  unavailable: " + ui.Sanitize(unavailableReasons(err))
}

// unavailableReasons says why no secret store can hold the passphrase.
func unavailableReasons(err error) string {
	var none *secretstore.NoBackendError
	if !errors.As(err, &none) {
		return userMessage(err)
	}
	var reasons []string
	for _, u := range none.Tried {
		if u != nil {
			reasons = append(reasons, u.Backend+": "+u.Reason)
		}
	}
	if len(reasons) == 0 {
		reasons = []string{"no secret store on this system"}
	}
	return strings.Join(reasons, "; ") + "; set TESSERA_PASSPHRASE for scripts"
}
