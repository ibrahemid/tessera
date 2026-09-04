package main

import (
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/base32x"
	"github.com/ibrahemid/tessera/go/internal/detect"
	"github.com/ibrahemid/tessera/go/internal/migration"
	"github.com/ibrahemid/tessera/go/internal/otp"
	"github.com/ibrahemid/tessera/go/internal/otpauth"
	"github.com/ibrahemid/tessera/go/internal/qr"
	"github.com/spf13/cobra"
)

func newAddCmd() *cobra.Command {
	var (
		qrPath     string
		issuer     string
		acct       string
		secret     string
		typ        string
		algorithm  string
		digits     int
		period     int
		folder     string
		fromScreen bool
	)
	cmd := &cobra.Command{
		Use:   "add [input]",
		Short: "Add accounts from an otpauth/migration URI, a setup key, a QR image, an export file, the screen, or manual flags",
		Args:  cobra.MaximumNArgs(1),
		Long: `Add one or more accounts. The positional argument auto-detects:

  tess add "otpauth://totp/GitHub:me?secret=..."   # otpauth URI
  tess add ZB573K4APD63E6RLD3WAHI3QFZ35RLEP        # bare base32 setup key -> TOTP
  tess add code.png                                # QR image (all codes decoded)
  tess add export.json                             # Aegis/2FAS/Raivo export, or migration/otpauth lines
  tess add --screen                                # select a QR code on screen (macOS)
  cat export.json | tess add -                     # read the input from stdin

For a setup key, --issuer/--account/--digits/--period/--algorithm override the
TOTP defaults. --qr and --secret keep working as before.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			var accts []account.Account
			var problems []importProblem
			var err error
			switch {
			case fromScreen:
				accts, problems, err = addFromScreen()
			case len(args) == 1:
				accts, problems, err = addFromArg(args[0], issuer, acct, algorithm, digits, period)
			case qrPath != "":
				accts, problems, err = addFromImage(qrPath)
			case secret != "":
				var a account.Account
				a, err = manualAccount(issuer, acct, secret, typ, algorithm, digits, period)
				if err == nil {
					accts = []account.Account{a}
				}
			default:
				return fmt.Errorf("provide an otpauth URI, a setup key, a file path, -, --qr <image>, --screen, or --secret with manual flags")
			}
			if err != nil {
				return err
			}
			if len(accts) == 0 {
				printProblems(cmd, problems)
				return fmt.Errorf("no account found in input")
			}

			s, err := openSession()
			if err != nil {
				return err
			}
			defer s.close()
			ts := now().Unix()
			added := 0
			for i := range accts {
				accts[i].ID = newID()
				accts[i].Folder = folder
				accts[i].CreatedAt = ts
				accts[i].UpdatedAt = ts
				if verr := accts[i].Validate(); verr != nil {
					problems = append(problems, importProblem{label(accts[i]), verr.Error()})
					continue
				}
				s.accounts = append(s.accounts, accts[i])
				added++
			}
			if added == 0 {
				printProblems(cmd, problems)
				return fmt.Errorf("no account found in input")
			}
			if err := s.save(); err != nil {
				return err
			}
			switch {
			case added > 1:
				out(cmd, "Added %d accounts", added)
			case label(accts[0]) == "":
				out(cmd, "Added account")
			default:
				out(cmd, "Added %s", label(accts[0]))
			}
			// spec/otpauth.md § partial-failure semantics: a batch imports what
			// parses and lists each item that did not.
			printProblems(cmd, problems)
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
	f.BoolVar(&fromScreen, "screen", false, "select a QR code on screen and add it (macOS)")
	return cmd
}

// addFromArg routes a single positional argument: an existing file is decoded as
// an image or parsed as text/JSON; otherwise the string is classified and parsed
// directly. A setup key applies the manual override flags. Items that fail come
// back as problems alongside the ones that parsed, never as a silent drop
// (spec/otpauth.md § partial-failure semantics).
func addFromArg(arg, issuer, acct, algorithm string, digits, period int) ([]account.Account, []importProblem, error) {
	if arg == "-" {
		return addFromStdin()
	}
	if _, err := os.Stat(arg); err == nil {
		if isImagePath(arg) {
			return addFromImage(arg)
		}
		data, err := os.ReadFile(arg)
		if err != nil {
			return nil, nil, fmt.Errorf("read %q: %w", arg, err)
		}
		accts, errs := detect.ParseText(string(data))
		return accts, itemProblems(arg, errs), nil
	}

	switch detect.Classify(arg) {
	case detect.OTPAuth:
		a, err := otpauth.Parse(arg)
		if err != nil {
			return nil, nil, err
		}
		return []account.Account{a}, nil, nil
	case detect.Migration:
		accts, err := migration.Parse(arg)
		return accts, nil, err
	case detect.SetupKey:
		return []account.Account{setupKeyAccount(arg, issuer, acct, algorithm, digits, period)}, nil, nil
	case detect.ExportJSON:
		accts, errs := detect.ParseText(arg)
		return accts, itemProblems("input", errs), nil
	default:
		return nil, nil, fmt.Errorf("unrecognized input (expected an otpauth URI, setup key, or file path)")
	}
}

// stdinReader is the standard input source, replaceable in tests.
var stdinReader io.Reader = os.Stdin

// addFromStdin parses everything on stdin, so tess can sit at the end of a
// pipeline that produces otpauth URIs or an export file.
func addFromStdin() ([]account.Account, []importProblem, error) {
	data, err := io.ReadAll(stdinReader)
	if err != nil {
		return nil, nil, fmt.Errorf("read stdin: %w", err)
	}
	accts, errs := detect.ParseText(string(data))
	return accts, itemProblems("stdin", errs), nil
}

// addFromImage decodes every QR code in an image and parses each payload.
func addFromImage(path string) ([]account.Account, []importProblem, error) {
	payloads, err := qr.DecodeFileAll(path)
	if err != nil {
		return nil, nil, err
	}
	var out []account.Account
	var problems []importProblem
	for i, payload := range payloads {
		src := path
		if len(payloads) > 1 {
			src = fmt.Sprintf("%s (QR %d)", path, i+1)
		}
		accts, errs := detect.ParseText(payload)
		out = append(out, accts...)
		problems = append(problems, itemProblems(src, errs)...)
	}
	return out, problems, nil
}

// itemProblems converts per-item parse failures into the reporting shape shared
// with `tess import`, tagging each with its source and line.
func itemProblems(source string, errs []detect.ItemError) []importProblem {
	if len(errs) == 0 {
		return nil
	}
	out := make([]importProblem, 0, len(errs))
	for _, e := range errs {
		src := source
		if e.Line > 0 {
			src = fmt.Sprintf("%s line %d", source, e.Line)
		}
		out = append(out, importProblem{src, e.Err.Error()})
	}
	return out
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

// decodeSteamSecret resolves a Steam shared_secret to raw key bytes. The spec
// says Steam secrets arrive as base64 (spec/otpauth.md § Steam Guard), so
// base64 is tried first, padded then unpadded, and base32 only as a fallback
// for a key that was handed over in otpauth form. The orders matter: a string
// like ABCDEFGHIJKLMNOPQRST is valid in both alphabets and means different
// bytes in each, and base64 is the documented Steam encoding.
func decodeSteamSecret(secret string) ([]byte, error) {
	s := strings.TrimSpace(secret)
	if s == "" {
		return nil, fmt.Errorf("steam secret is empty")
	}
	if b, err := otp.DecodeSteamSecret(s); err == nil && len(b) > 0 {
		return b, nil
	}
	if b, err := base64.RawStdEncoding.DecodeString(s); err == nil && len(b) > 0 {
		return b, nil
	}
	b, err := base32x.Decode(s)
	if err != nil {
		return nil, fmt.Errorf("steam secret must be base64 (Steam's shared_secret) or base32: %w", err)
	}
	return b, nil
}

func manualAccount(issuer, acct, secret, typ, algorithm string, digits, period int) (account.Account, error) {
	t := account.Type(strings.ToLower(typ))
	var raw []byte
	var err error
	if t == account.Steam {
		raw, err = decodeSteamSecret(secret)
		if err != nil {
			return account.Account{}, err
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
