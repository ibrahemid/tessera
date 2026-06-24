package main

import (
	"fmt"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/store"
	"github.com/ibrahemid/tessera/go/internal/vault"
	"github.com/spf13/cobra"
)

func newVaultCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "vault", Short: "Manage the vault"}
	cmd.AddCommand(newVaultInitCmd(), newVaultPasswdCmd())
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
