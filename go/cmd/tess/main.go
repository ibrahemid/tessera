// Command tess is the Tessera CLI: a scriptable TOTP/HOTP/Steam authenticator
// sharing an encrypted vault with the Tessera macOS app.
package main

import (
	"fmt"
	"os"
	"runtime/debug"

	"github.com/spf13/cobra"
)

// vaultPath holds the value of the global --vault flag.
var vaultPath string

// version is the CLI version, overridden at release time via -ldflags.
var version = "dev"

// resolveVersion returns the release version. goreleaser builds stamp it via
// -ldflags; `go install module@version` builds don't, so fall back to the
// module version Go embeds in the binary ("(devel)" for in-tree builds).
func resolveVersion() string {
	if version != "dev" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return version
}

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
		Version:       resolveVersion(),
		SilenceUsage:  true,
		SilenceErrors: true,
		// With no subcommand, print current codes for all accounts.
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCodeAll(cmd, false)
		},
	}
	root.PersistentFlags().StringVar(&vaultPath, "vault", "", "vault file path (default $TESSERA_VAULT or ~/.local/share/tessera/vault.json)")

	root.AddCommand(
		newVaultCmd(),
		newAddCmd(),
		newListCmd(),
		newCodeCmd(),
		newWatchCmd(),
		newImportCmd(),
		newExportCmd(),
		newMergeCmd(),
		newShowCmd(),
		newRemoveCmd(),
		newRenameCmd(),
		newAliasCmd(),
		newMoveCmd(),
		newTagCmd(),
	)
	return root
}
