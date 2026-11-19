package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/base32x"
	"github.com/ibrahemid/tessera/go/internal/otpauth"
	"github.com/ibrahemid/tessera/go/internal/qr"
	"github.com/ibrahemid/tessera/go/internal/ui"
	"github.com/spf13/cobra"
)

var (
	errShowJSONSecret = errors.New("--json never includes the secret; use --secret or --uri without --json")
	errShowQRAlone    = errors.New("--qr prints only the QR code; drop --secret, --uri and --json")
	errShowQRPiped    = errors.New("stdout is not a terminal and the QR code holds the secret; pass --force to write it anyway")
)

// newShowCmd prints one account's full record: the fields the row and the app's
// edit sheet expose, so a user can inspect type, algorithm, digits, period and
// folder from the terminal. Secrets stay hidden unless explicitly requested.
func newShowCmd() *cobra.Command {
	var withSecret, withURI, withQR, force, asJSON bool
	cmd := &cobra.Command{
		Use:   "show <query>",
		Short: "Show an account's details (type, algorithm, digits, period, folder, tags)",
		Long: `Print one account's record. The secret is never shown unless you ask for it:

  tess show github            # details, no secret
  tess show github --secret   # also print the base32 setup key (cleartext)
  tess show github --uri      # also print the otpauth:// setup link (cleartext)
  tess show github --qr       # draw the setup link as a QR code to scan
  tess show github --json     # details as JSON, no secret

--qr refuses to write anywhere but a terminal unless you pass --force.`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeFirstArg,
		RunE: func(cmd *cobra.Command, args []string) error {
			switch {
			case withQR && (withSecret || withURI || asJSON):
				return errShowQRAlone
			case asJSON && (withSecret || withURI):
				return errShowJSONSecret
			case withQR && !force && !stdoutIsTTY():
				return errShowQRPiped
			}
			s, err := openSession()
			if err != nil {
				return err
			}
			defer s.close()
			idx, err := s.single(cmd, args[0])
			if err != nil {
				return err
			}
			a := s.accounts[idx]
			if asJSON {
				out(cmd, "%s", mustJSON(newShowRecord(a)))
				return nil
			}
			if withQR {
				errOut(cmd, "This QR code holds the secret for %s.", label(a))
				return qr.WriteTerminal(cmd.OutOrStdout(), otpauth.Format(a))
			}
			for _, row := range showRows(a) {
				out(cmd, "%-10s %s", row[0], ui.Sanitize(row[1]))
			}
			for n, line := range noteLines(a.Note) {
				key := ""
				if n == 0 {
					key = "note"
				}
				out(cmd, "%-10s %s", key, ui.Sanitize(line))
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
	cmd.Flags().BoolVar(&withQR, "qr", false, "draw the otpauth:// setup link as a QR code (holds the secret)")
	cmd.Flags().BoolVar(&force, "force", false, "with --qr, write the QR code even when stdout is not a terminal")
	cmd.Flags().BoolVar(&asJSON, "json", false, "output JSON (never includes the secret)")
	return cmd
}

// showRecord is the JSON shape of `tess show --json`; docs/CLI-JSON.md is its
// contract. It never carries the secret. Period applies to time-based
// accounts and Counter to HOTP.
type showRecord struct {
	ID        string   `json:"id"`
	Handle    string   `json:"handle"`
	Issuer    string   `json:"issuer"`
	Account   string   `json:"account"`
	Type      string   `json:"type"`
	Algorithm string   `json:"algorithm"`
	Digits    int      `json:"digits"`
	Period    *int     `json:"period,omitempty"`
	Counter   *int64   `json:"counter,omitempty"`
	Folder    string   `json:"folder,omitempty"`
	Tags      []string `json:"tags,omitempty"`
	Pinned    bool     `json:"pinned"`
	Note      string   `json:"note,omitempty"`
	Order     int      `json:"order,omitempty"`
	CreatedAt int64    `json:"created_at"`
	UpdatedAt int64    `json:"updated_at"`
}

func newShowRecord(a account.Account) showRecord {
	r := showRecord{
		ID: a.ID, Handle: a.Handle, Issuer: a.Issuer, Account: a.Account, Type: string(a.Type),
		Algorithm: a.Algorithm, Digits: a.Digits, Folder: a.Folder, Tags: a.Tags, Pinned: a.Pinned,
		Note: a.Note, Order: a.Order, CreatedAt: a.CreatedAt, UpdatedAt: a.UpdatedAt,
	}
	if a.Type == account.HOTP {
		counter := a.Counter
		r.Counter = &counter
	} else {
		period := a.Period
		r.Period = &period
	}
	return r
}

// showRows returns the label/value pairs for an account, omitting fields that do
// not apply to its type (period for HOTP, counter for everything else).
func showRows(a account.Account) [][2]string {
	rows := [][2]string{
		{"alias", a.Handle},
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

// noteLines splits a note into the lines tess show prints, with each tab
// widened to spaces so it is not escaped.
func noteLines(note string) []string {
	if note == "" {
		return nil
	}
	return strings.Split(strings.ReplaceAll(note, "\t", "    "), "\n")
}
