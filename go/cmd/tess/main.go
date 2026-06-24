// Command tess is the Tessera CLI: a scriptable TOTP/HOTP/Steam authenticator
// sharing an encrypted vault with the Tessera macOS app.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// vaultPath holds the value of the global --vault flag.
var vaultPath string

func main() {
	root := newRootCmd()
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "tess",
		Short:         "Tessera: a CLI-first TOTP/2FA authenticator",
		SilenceUsage:  true,
		SilenceErrors: true,
		// With no subcommand, print current codes for all accounts.
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCodeAll(cmd)
		},
	}
	root.PersistentFlags().StringVar(&vaultPath, "vault", "", "vault file path (default $TESSERA_VAULT or ~/.local/share/tessera/vault.json)")

	root.AddCommand(
		newVaultCmd(),
		newAddCmd(),
		newListCmd(),
		newCodeCmd(),
		newImportCmd(),
		newExportCmd(),
		newRemoveCmd(),
		newRenameCmd(),
		newMoveCmd(),
		newTagCmd(),
	)
	return root
}
