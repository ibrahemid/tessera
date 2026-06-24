package main

import (
	"fmt"
	"strings"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/base32x"
	"github.com/ibrahemid/tessera/go/internal/otpauth"
	"github.com/ibrahemid/tessera/go/internal/qr"
	"github.com/spf13/cobra"
)

func newAddCmd() *cobra.Command {
	var (
		qrPath    string
		issuer    string
		acct      string
		secret    string
		typ       string
		algorithm string
		digits    int
		period    int
		folder    string
	)
	cmd := &cobra.Command{
		Use:   "add [otpauth-uri]",
		Short: "Add an account from an otpauth URI, a QR image, or manual flags",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var a account.Account
			var err error
			switch {
			case len(args) == 1:
				a, err = otpauth.Parse(args[0])
			case qrPath != "":
				var uri string
				uri, err = qr.DecodeFile(qrPath)
				if err == nil {
					a, err = otpauth.Parse(uri)
				}
			case secret != "":
				a, err = manualAccount(issuer, acct, secret, typ, algorithm, digits, period)
			default:
				return fmt.Errorf("provide an otpauth URI, --qr <image>, or --secret with manual flags")
			}
			if err != nil {
				return err
			}
			a.ID = newID()
			a.Folder = folder
			a.CreatedAt = now().Unix()
			a.UpdatedAt = a.CreatedAt
			if err := a.Validate(); err != nil {
				return err
			}

			s, err := openSession()
			if err != nil {
				return err
			}
			s.accounts = append(s.accounts, a)
			if err := s.save(); err != nil {
				return err
			}
			out(cmd, "Added %s", label(a))
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&qrPath, "qr", "", "decode the account from a QR image file (png/jpeg)")
	f.StringVar(&issuer, "issuer", "", "issuer (manual)")
	f.StringVar(&acct, "account", "", "account name (manual)")
	f.StringVar(&secret, "secret", "", "base32 secret (manual)")
	f.StringVar(&typ, "type", "totp", "totp|hotp|steam (manual)")
	f.StringVar(&algorithm, "algorithm", "SHA1", "SHA1|SHA256|SHA512 (manual)")
	f.IntVar(&digits, "digits", 6, "code digits (manual)")
	f.IntVar(&period, "period", 30, "period seconds (manual)")
	f.StringVar(&folder, "folder", "", "folder to place the account in")
	return cmd
}

func manualAccount(issuer, acct, secret, typ, algorithm string, digits, period int) (account.Account, error) {
	t := account.Type(strings.ToLower(typ))
	var raw []byte
	var err error
	if t == account.Steam {
		// Steam secrets are base64; otp.DecodeSteamSecret handles that at the boundary.
		raw, err = base32x.Decode(secret) // accept base32 too; fall through below
		if err != nil {
			return account.Account{}, fmt.Errorf("steam secret must be base32 or use import: %w", err)
		}
		digits = 5
	} else {
		raw, err = base32x.Decode(secret)
		if err != nil {
			return account.Account{}, fmt.Errorf("invalid base32 secret: %w", err)
		}
	}
	return account.Account{
		Type: t, Issuer: issuer, Account: acct, Secret: raw,
		Algorithm: strings.ToUpper(algorithm), Digits: digits, Period: period,
	}, nil
}
