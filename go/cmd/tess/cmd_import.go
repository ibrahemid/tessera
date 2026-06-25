package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/base32x"
	"github.com/ibrahemid/tessera/go/internal/migration"
	"github.com/ibrahemid/tessera/go/internal/otpauth"
	"github.com/ibrahemid/tessera/go/internal/qr"
	"github.com/spf13/cobra"
)

func newImportCmd() *cobra.Command {
	var (
		migrationURIs []string
		otpauthURIs   []string
		qrPaths       []string
		filePath      string
	)
	cmd := &cobra.Command{
		Use:   "import",
		Short: "Bulk-import accounts from a file, Google exports, otpauth URIs, or QR images",
		Long: `Import accounts in bulk. Sources combine and can be repeated:

  tess import --file accounts.txt              # one otpauth:// or otpauth-migration:// per line
  tess import --qr a.png --qr b.png            # multiple QR images (e.g. a multi-screen Google export)
  tess import --migration "otpauth-migration://offline?data=..."
  tess import --otpauth "otpauth://totp/...."

Duplicate accounts (same type, issuer, account and secret) are skipped.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			var imported []account.Account

			if filePath != "" {
				accts, err := importFromFile(filePath)
				if err != nil {
					return err
				}
				imported = append(imported, accts...)
			}
			for _, uri := range migrationURIs {
				accts, err := migration.Parse(uri)
				if err != nil {
					return fmt.Errorf("migration import: %w", err)
				}
				imported = append(imported, accts...)
			}
			for _, uri := range otpauthURIs {
				a, err := otpauth.Parse(uri)
				if err != nil {
					return fmt.Errorf("otpauth import: %w", err)
				}
				imported = append(imported, a)
			}
			for _, path := range qrPaths {
				payload, err := qr.DecodeFile(path)
				if err != nil {
					return fmt.Errorf("qr %s: %w", path, err)
				}
				accts, err := importFromPayload(payload)
				if err != nil {
					return fmt.Errorf("qr %s: %w", path, err)
				}
				imported = append(imported, accts...)
			}

			if len(imported) == 0 {
				return fmt.Errorf("specify --file, --qr, --migration, or --otpauth")
			}

			s, err := openSession()
			if err != nil {
				return err
			}
			added, skipped := mergeAccounts(s, imported)
			if added == 0 {
				out(cmd, "No new accounts (%d duplicate(s) skipped).", skipped)
				return nil
			}
			if err := s.save(); err != nil {
				return err
			}
			if skipped > 0 {
				out(cmd, "Imported %d account(s); skipped %d duplicate(s).", added, skipped)
			} else {
				out(cmd, "Imported %d account(s).", added)
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&filePath, "file", "", "text file with one otpauth:// or otpauth-migration:// URI per line")
	f.StringArrayVar(&migrationURIs, "migration", nil, "Google Authenticator otpauth-migration:// URI (repeatable)")
	f.StringArrayVar(&otpauthURIs, "otpauth", nil, "single otpauth:// URI (repeatable)")
	f.StringArrayVar(&qrPaths, "qr", nil, "QR image file (repeatable; png/jpeg)")
	return cmd
}

// importFromFile parses a text file of otpauth/otpauth-migration URIs, one per
// line. Blank lines and lines starting with '#' are ignored.
func importFromFile(path string) ([]account.Account, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read import file: %w", err)
	}
	var out []account.Account
	for n, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		accts, err := importFromPayload(line)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", n+1, err)
		}
		out = append(out, accts...)
	}
	return out, nil
}

// importFromPayload dispatches a single URI/payload to the right parser.
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
		return nil, fmt.Errorf("unrecognized payload (expected otpauth:// or otpauth-migration://)")
	}
}

// mergeAccounts stamps and appends imported accounts to the session, skipping
// duplicates (same type/issuer/account/secret) already present or within the
// batch. Returns counts of added and skipped.
func mergeAccounts(s *session, imported []account.Account) (added, skipped int) {
	seen := map[string]bool{}
	for _, a := range s.accounts {
		seen[dedupeKey(a)] = true
	}
	ts := now().Unix()
	for _, a := range imported {
		key := dedupeKey(a)
		if seen[key] {
			skipped++
			continue
		}
		seen[key] = true
		a.ID = newID()
		a.CreatedAt = ts
		a.UpdatedAt = ts
		if err := a.Validate(); err != nil {
			skipped++
			continue
		}
		s.accounts = append(s.accounts, a)
		added++
	}
	return added, skipped
}

func dedupeKey(a account.Account) string {
	return strings.Join([]string{
		string(a.Type), strings.ToLower(a.Issuer), strings.ToLower(a.Account),
		base32x.EncodeNoPad(a.Secret),
	}, "|")
}
