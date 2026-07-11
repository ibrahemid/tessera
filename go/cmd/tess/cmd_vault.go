package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/store"
	"github.com/ibrahemid/tessera/go/internal/vault"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func newVaultCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "vault", Short: "Manage the vault"}
	cmd.AddCommand(newVaultInitCmd(), newVaultPasswdCmd(), newVaultResetCmd())
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
			if err := store.Save(path, env); err != nil {
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

func newVaultPasswdCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "passwd",
		Short: "Change the vault passphrase",
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := store.Resolve(vaultPath)
			if err != nil {
				return err
			}
			env, err := store.Load(path)
			if err != nil {
				return err
			}
			old, err := readPassphrase("Current passphrase: ")
			if err != nil {
				return err
			}
			if _, err := env.Open(old); err != nil {
				return err
			}
			newPass, err := requireNewVaultPassphrase()
			if err != nil {
				return err
			}
			if err := env.ChangePassphrase(old, newPass); err != nil {
				return err
			}
			if err := store.Save(path, env); err != nil {
				return err
			}
			out(cmd, "Passphrase changed")
			return nil
		},
	}
}
