package main

import (
	"fmt"
	"strings"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/base32x"
	"github.com/ibrahemid/tessera/go/internal/otpauth"
	"github.com/spf13/cobra"
)

// newShowCmd prints one account's full record: the fields the row and the app's
// edit sheet expose, so a user can inspect type, algorithm, digits, period and
// folder from the terminal. Secrets stay hidden unless explicitly requested.
func newShowCmd() *cobra.Command {
	var withSecret, withURI bool
	cmd := &cobra.Command{
		Use:   "show <query>",
		Short: "Show an account's details (type, algorithm, digits, period, folder, tags)",
		Long: `Print one account's record. The secret is never shown unless you ask for it:

  tess show github            # details, no secret
  tess show github --secret   # also print the base32 setup key (cleartext)
  tess show github --uri      # also print the otpauth:// setup link (cleartext)`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openSession()
			if err != nil {
				return err
			}
			idx, err := s.single(cmd, args[0])
			if err != nil {
				return err
			}
			a := s.accounts[idx]
			for _, row := range showRows(a) {
				out(cmd, "%-10s %s", row[0], row[1])
			}
			if withSecret {
				out(cmd, "%-10s %s", "secret", base32x.EncodeNoPad(a.Secret))
			}
			if withURI {
				out(cmd, "%-10s %s", "uri", otpauth.Format(a))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&withSecret, "secret", false, "also print the base32 setup key (cleartext)")
	cmd.Flags().BoolVar(&withURI, "uri", false, "also print the otpauth:// setup link (cleartext)")
	return cmd
}

// showRows returns the label/value pairs for an account, omitting fields that do
// not apply to its type (period for HOTP, counter for everything else).
func showRows(a account.Account) [][2]string {
	rows := [][2]string{
		{"handle", a.Handle},
		{"issuer", a.Issuer},
		{"account", a.Account},
		{"type", string(a.Type)},
		{"algorithm", a.Algorithm},
		{"digits", fmt.Sprintf("%d", a.Digits)},
	}
	if a.Type == account.HOTP {
		rows = append(rows, [2]string{"counter", fmt.Sprintf("%d", a.Counter)})
	} else {
		rows = append(rows, [2]string{"period", fmt.Sprintf("%ds", a.Period)})
	}
	if a.Folder != "" {
		rows = append(rows, [2]string{"folder", a.Folder})
	}
	if len(a.Tags) > 0 {
		rows = append(rows, [2]string{"tags", strings.Join(a.Tags, ", ")})
	}
	if a.Pinned {
		rows = append(rows, [2]string{"pinned", "yes"})
	}
	return rows
}
