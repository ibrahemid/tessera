package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/base32x"
	"github.com/ibrahemid/tessera/go/internal/detect"
	"github.com/ibrahemid/tessera/go/internal/migration"
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
		Use:   "add [input]",
		Short: "Add accounts from an otpauth/migration URI, a setup key, a QR image, an export file, or manual flags",
		Args:  cobra.MaximumNArgs(1),
		Long: `Add one or more accounts. The positional argument auto-detects:

  tess add "otpauth://totp/GitHub:me?secret=..."   # otpauth URI
  tess add ZB573K4APD63E6RLD3WAHI3QFZ35RLEP        # bare base32 setup key -> TOTP
  tess add code.png                                # QR image (all codes decoded)
  tess add export.json                             # Aegis/2FAS/Raivo export, or migration/otpauth lines

For a setup key, --issuer/--account/--digits/--period/--algorithm override the
TOTP defaults. --qr and --secret keep working as before.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			var accts []account.Account
			var err error
			switch {
			case len(args) == 1:
				accts, err = addFromArg(args[0], issuer, acct, algorithm, digits, period)
			case qrPath != "":
				accts, err = addFromImage(qrPath)
			case secret != "":
				var a account.Account
				a, err = manualAccount(issuer, acct, secret, typ, algorithm, digits, period)
				if err == nil {
					accts = []account.Account{a}
				}
			default:
				return fmt.Errorf("provide an otpauth URI, a setup key, a file path, --qr <image>, or --secret with manual flags")
			}
			if err != nil {
				return err
			}
			if len(accts) == 0 {
				return fmt.Errorf("no account found in input")
			}

			s, err := openSession()
			if err != nil {
				return err
			}
			ts := now().Unix()
			for i := range accts {
				accts[i].ID = newID()
				accts[i].Folder = folder
				accts[i].CreatedAt = ts
				accts[i].UpdatedAt = ts
				if err := accts[i].Validate(); err != nil {
					return err
				}
				s.accounts = append(s.accounts, accts[i])
			}
			if err := s.save(); err != nil {
				return err
			}
			switch {
			case len(accts) > 1:
				out(cmd, "Added %d accounts", len(accts))
			case label(accts[0]) == "":
				out(cmd, "Added account")
			default:
				out(cmd, "Added %s", label(accts[0]))
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&qrPath, "qr", "", "decode the account(s) from a QR image file (png/jpeg/webp/tiff/bmp)")
	f.StringVar(&issuer, "issuer", "", "issuer (manual/setup key)")
	f.StringVar(&acct, "account", "", "account name (manual/setup key)")
	f.StringVar(&secret, "secret", "", "base32 secret (manual)")
	f.StringVar(&typ, "type", "totp", "totp|hotp|steam (manual)")
	f.StringVar(&algorithm, "algorithm", "SHA1", "SHA1|SHA256|SHA512 (manual/setup key)")
	f.IntVar(&digits, "digits", 6, "code digits (manual/setup key)")
	f.IntVar(&period, "period", 30, "period seconds (manual/setup key)")
	f.StringVar(&folder, "folder", "", "folder to place the account(s) in")
	return cmd
}

// addFromArg routes a single positional argument: an existing file is decoded as
// an image or parsed as text/JSON; otherwise the string is classified and parsed
// directly. A setup key applies the manual override flags.
func addFromArg(arg, issuer, acct, algorithm string, digits, period int) ([]account.Account, error) {
	if _, err := os.Stat(arg); err == nil {
		if isImagePath(arg) {
			return addFromImage(arg)
		}
		data, err := os.ReadFile(arg)
		if err != nil {
			return nil, fmt.Errorf("read %q: %w", arg, err)
		}
		accts, errs := detect.ParseText(string(data))
		if len(accts) == 0 && len(errs) > 0 {
			return nil, fmt.Errorf("%s", errs[0].Err)
		}
		return accts, nil
	}

	switch detect.Classify(arg) {
	case detect.OTPAuth:
		a, err := otpauth.Parse(arg)
		if err != nil {
			return nil, err
		}
		return []account.Account{a}, nil
	case detect.Migration:
		return migration.Parse(arg)
	case detect.SetupKey:
		return []account.Account{setupKeyAccount(arg, issuer, acct, algorithm, digits, period)}, nil
	case detect.ExportJSON:
		accts, errs := detect.ParseText(arg)
		if len(accts) == 0 && len(errs) > 0 {
			return nil, fmt.Errorf("%s", errs[0].Err)
		}
		return accts, nil
	default:
		return nil, fmt.Errorf("unrecognized input (expected an otpauth URI, setup key, or file path)")
	}
}

// addFromImage decodes every QR code in an image and parses each payload.
func addFromImage(path string) ([]account.Account, error) {
	payloads, err := qr.DecodeFileAll(path)
	if err != nil {
		return nil, err
	}
	var out []account.Account
	for _, payload := range payloads {
		accts, errs := detect.ParseText(payload)
		if len(accts) == 0 && len(errs) > 0 {
			return nil, fmt.Errorf("%s", errs[0].Err)
		}
		out = append(out, accts...)
	}
	return out, nil
}

// setupKeyAccount builds a TOTP account from a bare base32 setup key, applying
// the manual override flags on top of the spec defaults (SHA1/6/30). The key is
// already validated by detect.Classify, so the decode cannot fail.
func setupKeyAccount(key, issuer, acct, algorithm string, digits, period int) account.Account {
	secret, _ := base32x.Decode(strings.NewReplacer(" ", "", "-", "").Replace(key))
	return account.Account{
		Type:      account.TOTP,
		Issuer:    issuer,
		Account:   acct,
		Secret:    secret,
		Algorithm: strings.ToUpper(algorithm),
		Digits:    digits,
		Period:    period,
	}
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
