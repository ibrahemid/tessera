// Command tess is the Tessera CLI: a scriptable TOTP/HOTP/Steam authenticator
// sharing an encrypted vault with the Tessera macOS app.
package main

import (
	"os"
	"runtime/debug"

	"github.com/ibrahemid/tessera/go/internal/store"
	"github.com/ibrahemid/tessera/go/internal/ui"
	"github.com/spf13/cobra"
)

// vaultPath holds the value of the global --vault flag.
var vaultPath string

// extraCommands builds commands that only exist under a build tag; each is
// added to the root command.
var extraCommands []func() *cobra.Command

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
	restoreConsole, _ := ui.EnableConsole()
	code := 0
	if err := newRootCmd().Execute(); err != nil {
		code = reportError(os.Stderr, err)
	}
	restoreConsole()
	os.Exit(code)
}

func newRootCmd() *cobra.Command {
	opts := &codeOptions{}
	root := &cobra.Command{
		Use:   "tess [query]",
		Short: "Tessera: a CLI-first TOTP/2FA authenticator",
		Long: `Print and copy TOTP/HOTP/Steam codes from an encrypted local vault.

  tess                     current codes for every account
  tess acme                one account's code, copied to the clipboard
  tess watch               live view with countdown bars
  tess add <uri|key|file>  add accounts
  tess vault init          create a vault`,
		Version:       resolveVersion(),
		Args:          cobra.MaximumNArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		// A bare argument is an account query: `tess acme` is `tess code acme`.
		// Cobra matches subcommands by exact name, so a command name always wins.
		ValidArgsFunction: completeFirstArg,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCode(cmd, args, opts)
		},
	}
	root.PersistentFlags().StringVar(&vaultPath, "vault", "", "vault file path (default $TESSERA_VAULT or "+store.DefaultPathHint()+")")
	bindCodeFlags(root, opts)

	root.AddGroup(
		&cobra.Group{ID: "codes", Title: "Codes:"},
		&cobra.Group{ID: "accounts", Title: "Accounts:"},
		&cobra.Group{ID: "transfer", Title: "Transfer:"},
		&cobra.Group{ID: "vault", Title: "Vault:"},
	)
	groups := []struct {
		id   string
		cmds []*cobra.Command
	}{
		{"codes", []*cobra.Command{newCodeCmd(), newVerifyCmd(), newWatchCmd()}},
		{"accounts", []*cobra.Command{
			newAddCmd(), newListCmd(), newShowCmd(), newRenameCmd(),
			newAliasCmd(), newMoveCmd(), newTagCmd(), newNoteCmd(),
			newPinCmd(), newUnpinCmd(), newOrderCmd(), newEditCmd(),
			newDedupeCmd(), newRemoveCmd(), newTrashCmd(), newRestoreCmd(),
		}},
		{"transfer", []*cobra.Command{newImportCmd(), newExportCmd(), newMergeCmd()}},
		{"vault", []*cobra.Command{newVaultCmd()}},
	}
	for _, g := range groups {
		for _, c := range g.cmds {
			c.GroupID = g.id
			root.AddCommand(c)
		}
	}
	root.AddCommand(newClipClearCmd())
	for _, newCmd := range extraCommands {
		root.AddCommand(newCmd())
	}
	return root
}
