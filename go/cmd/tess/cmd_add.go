package main

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/url"
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
  tess add --screen                                # select a QR code on screen (macOS, Linux)
  cat export.json | tess add -                     # read the input from stdin
  tess add                                         # type the URI or setup key without echo
  tess add --secret - --issuer GitHub              # read the setup key from stdin

For a setup key, --issuer/--account/--digits/--period/--algorithm override the
TOTP defaults; --issuer also replaces the issuer of an otpauth URI. --qr and
--secret keep working as before.

` + argumentNote,
		RunE: func(cmd *cobra.Command, args []string) error {
			issuer, acct, folder = account.CleanText(issuer), account.CleanText(acct), account.CleanText(folder)
			var accts []account.Account
			var problems []importProblem
			var err error
			fromArgument := false
			switch {
			case fromScreen:
				accts, problems, err = addFromScreen(cmd.Context())
			case len(args) == 1:
				fromArgument = isSecretArgument(args[0])
				accts, problems, err = addFromArg(args[0], issuer, acct, algorithm, digits, period)
			case qrPath != "":
				accts, problems, err = addFromImage(qrPath)
			case secret != "":
				fromArgument = secret != "-"
				var a account.Account
				a, err = addFromSecretFlag(secret, issuer, acct, typ, algorithm, digits, period)
				if err == nil {
					accts = []account.Account{a}
				}
			case stdinIsTTY():
				var text string
				text, err = promptHidden("otpauth URI or setup key: ")
				if err == nil {
					accts, problems, err = addFromText(text, "input", issuer, acct, algorithm, digits, period)
				}
			default:
				return fmt.Errorf("provide an otpauth URI, a setup key, a file path, -, --qr <image>, --screen, or --secret with manual flags")
			}
			if fromArgument && stdinIsTTY() {
				defer errOut(cmd, "%s", historyWarning)
			}
			if err != nil {
				return err
			}
			if len(accts) == 0 {
				printProblems(cmd, problems)
				return fmt.Errorf("no account found in input")
			}
			if len(accts) == 1 && isUnnamed(accts[0]) && stdinIsTTY() {
				if name, perr := promptLine("Issuer: "); perr == nil {
					accts[0].Issuer = account.CleanText(name)
				}
			}

			s, err := openSession()
			if err != nil {
				return err
			}
			defer s.close()
			for i := range accts {
				accts[i].Folder = folder
			}
			merged := mergeAccountsDetailed(s, accts)
			problems = append(problems, merged.failed...)
			if len(merged.added) == 0 {
				printSkipped(cmd, merged.skipped)
				if len(merged.skipped) > 0 && len(problems) == 0 {
					return nil
				}
				printProblems(cmd, problems)
				return fmt.Errorf("no account found in input")
			}
			if err := s.save(); err != nil {
				return err
			}
			printAdded(cmd, s, merged.added)
			printSkipped(cmd, merged.skipped)
			// spec/otpauth.md § partial-failure semantics: a batch imports what
			// parses and lists each item that did not.
			printProblems(cmd, problems)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&qrPath, "qr", "", "decode the account(s) from a QR image file (png/jpeg/webp/tiff/bmp)")
	f.StringVar(&issuer, "issuer", "", "issuer (manual, setup key, or otpauth URI)")
	f.StringVar(&acct, "account", "", "account name (manual/setup key)")
	f.StringVar(&secret, "secret", "", "base32 secret, or - to read it from stdin (manual)")
	f.StringVar(&typ, "type", "totp", "totp|hotp|steam (manual)")
	f.StringVar(&algorithm, "algorithm", "SHA1", "SHA1|SHA256|SHA512 (manual/setup key)")
	f.IntVar(&digits, "digits", 6, "code digits (manual/setup key)")
	f.IntVar(&period, "period", 30, "period seconds (manual/setup key)")
	f.StringVar(&folder, "folder", "", "folder to place the account(s) in")
	f.BoolVar(&fromScreen, "screen", false, "select a QR code on screen and add it (macOS, Linux)")
	return cmd
}

// isUnnamed reports an account with neither an issuer nor an account name.
func isUnnamed(a account.Account) bool {
	return strings.TrimSpace(a.Issuer) == "" && strings.TrimSpace(a.Account) == ""
}

// isSecretArgument reports whether a positional argument carries a secret
// itself, rather than naming stdin or a file to read.
func isSecretArgument(arg string) bool {
	if arg == "-" {
		return false
	}
	if _, err := os.Stat(arg); err == nil {
		return false
	}
	return detect.Classify(strings.TrimSpace(arg)) != detect.Invalid
}

// addFromSecretFlag builds the manual account for --secret, reading the key
// from stdin when the flag is -.
func addFromSecretFlag(secret, issuer, acct, typ, algorithm string, digits, period int) (account.Account, error) {
	if secret == "-" {
		key, err := readSecretFromStdin()
		if err != nil {
			return account.Account{}, err
		}
		if key == "" {
			return account.Account{}, fmt.Errorf("no secret on stdin")
		}
		secret = key
	}
	return manualAccount(issuer, acct, secret, typ, algorithm, digits, period)
}

// readSecretFromStdin prompts without echo on a terminal and otherwise reads
// the whole of stdin.
func readSecretFromStdin() (string, error) {
	if stdinIsTTY() {
		return promptHidden("Secret: ")
	}
	data, err := readCapped(stdinReader, "stdin")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// printAdded reports the accounts just saved, naming the alias of a single one.
func printAdded(cmd *cobra.Command, s *session, ids []string) {
	if len(ids) > 1 {
		out(cmd, "Added %d accounts", len(ids))
		return
	}
	i := indexByID(s.accounts, ids[0])
	if i < 0 {
		out(cmd, "Added 1 account")
		return
	}
	a := s.accounts[i]
	if isUnnamed(a) {
		h := displayHandle(a)
		out(cmd, "Added an account with no name as %s. Name it with `tess rename %s --issuer NAME`.", h, h)
		return
	}
	out(cmd, "Added %s as %s", label(a), displayHandle(a))
}

// printSkipped lists accounts left out because the vault already holds them.
func printSkipped(cmd *cobra.Command, labels []string) {
	for _, l := range labels {
		out(cmd, "Skipped %s: already in the vault", l)
	}
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
		data, err := readFileCapped(arg)
		if err != nil {
			return nil, nil, err
		}
		accts, errs := detect.ParseText(string(data))
		return accts, itemProblems(arg, errs), nil
	}
	return addFromText(arg, "input", issuer, acct, algorithm, digits, period)
}

// addFromText classifies and parses text that is not a file: an otpauth or
// migration URI, a setup key, or a pasted export.
func addFromText(text, source, issuer, acct, algorithm string, digits, period int) ([]account.Account, []importProblem, error) {
	switch detect.Classify(text) {
	case detect.OTPAuth:
		a, err := parseOTPAuth(text, issuer)
		if err != nil {
			return nil, nil, err
		}
		return []account.Account{a}, nil, nil
	case detect.Migration:
		accts, err := migration.Parse(text)
		return accts, nil, err
	case detect.SetupKey:
		return []account.Account{setupKeyAccount(text, issuer, acct, algorithm, digits, period)}, nil, nil
	case detect.ExportJSON, detect.ExportCSV, detect.ExportBinary:
		// An export pasted as an argument rather than handed over as a file: a
		// CSV export starts with its header line, a .1pux or an encrypted
		// Stratum backup with its magic bytes.
		accts, errs := detect.ParseText(text)
		return accts, itemProblems(source, errs), nil
	default:
		return nil, nil, fmt.Errorf("unrecognized input (expected an otpauth URI, setup key, or file path)")
	}
}

// parseOTPAuth parses an otpauth URI. A non-empty issuer replaces both the
// label's issuer and the issuer parameter, which settles a URI whose two
// issuers disagree.
func parseOTPAuth(uri, issuer string) (account.Account, error) {
	if issuer == "" {
		return otpauth.Parse(uri)
	}
	u, err := url.Parse(strings.TrimSpace(uri))
	if err != nil {
		return account.Account{}, errors.New("invalid otpauth URI")
	}
	label := strings.TrimPrefix(u.EscapedPath(), "/")
	if _, acctPart, ok := strings.Cut(label, ":"); ok {
		label = acctPart
	}
	q := u.Query()
	q.Set("issuer", issuer)
	return otpauth.Parse(u.Scheme + "://" + u.Host + "/" + label + "?" + q.Encode())
}

// stdinReader is the standard input source, replaceable in tests.
var stdinReader io.Reader = os.Stdin

// addFromStdin parses everything on stdin, so tess can sit at the end of a
// pipeline that produces otpauth URIs or an export file.
func addFromStdin() ([]account.Account, []importProblem, error) {
	data, err := readCapped(stdinReader, "stdin")
	if err != nil {
		return nil, nil, err
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
		out = append(out, importProblem{src, userMessage(e.Err)})
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
