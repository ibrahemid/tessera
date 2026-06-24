package main

import (
	"fmt"
	"strings"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/migration"
	"github.com/ibrahemid/tessera/go/internal/otpauth"
	"github.com/ibrahemid/tessera/go/internal/qr"
	"github.com/spf13/cobra"
)

func newImportCmd() *cobra.Command {
	var (
		migrationURI string
		otpauthURI   string
		qrPath       string
	)
	cmd := &cobra.Command{
		Use:   "import",
		Short: "Import accounts from a Google export, an otpauth URI, or a QR image",
		RunE: func(cmd *cobra.Command, args []string) error {
			var imported []account.Account
			var err error
			switch {
			case migrationURI != "":
				imported, err = migration.Parse(migrationURI)
			case otpauthURI != "":
				var a account.Account
				a, err = otpauth.Parse(otpauthURI)
				imported = []account.Account{a}
			case qrPath != "":
				var payload string
				payload, err = qr.DecodeFile(qrPath)
				if err == nil {
					imported, err = importFromPayload(payload)
				}
			default:
				return fmt.Errorf("specify --migration, --otpauth, or --qr")
			}
			if err != nil {
				return err
			}
			if len(imported) == 0 {
				return fmt.Errorf("nothing to import")
			}

			s, err := openSession()
			if err != nil {
				return err
			}
			ts := now().Unix()
			for i := range imported {
				imported[i].ID = newID()
				imported[i].CreatedAt = ts
				imported[i].UpdatedAt = ts
				if err := imported[i].Validate(); err != nil {
					return fmt.Errorf("imported account invalid: %w", err)
				}
				s.accounts = append(s.accounts, imported[i])
			}
			if err := s.save(); err != nil {
				return err
			}
			out(cmd, "Imported %d account(s)", len(imported))
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&migrationURI, "migration", "", "Google Authenticator otpauth-migration:// URI")
	f.StringVar(&otpauthURI, "otpauth", "", "single otpauth:// URI")
	f.StringVar(&qrPath, "qr", "", "QR image file containing an otpauth or migration payload")
	return cmd
}

// importFromPayload dispatches a decoded QR payload to the right parser.
func importFromPayload(payload string) ([]account.Account, error) {
	switch {
	case strings.HasPrefix(payload, "otpauth-migration://"):
		return migration.Parse(payload)
	case strings.HasPrefix(payload, "otpauth://"):
		a, err := otpauth.Parse(payload)
		if err != nil {
			return nil, err
		}
		return []account.Account{a}, nil
	default:
		return nil, fmt.Errorf("unrecognized QR payload")
	}
}
