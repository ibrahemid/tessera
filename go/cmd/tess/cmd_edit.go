package main

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/base32x"
	"github.com/spf13/cobra"
)

// errSecretFlagValue reports a value given to --secret, which reads the secret
// from a prompt or stdin so that it never lands in the shell history.
var errSecretFlagValue = errors.New("--secret takes no value; it reads the secret from a prompt or stdin")

// errEmptySecret reports a secret that is empty once surrounding whitespace is
// removed.
var errEmptySecret = errors.New("the secret is empty")

// secretFlag is the --secret switch. Set never fails, so a value typed after
// --secret= is never echoed back in a flag parsing error.
type secretFlag struct {
	set    bool
	inline bool
}

func (f *secretFlag) String() string { return "false" }

func (f *secretFlag) Type() string { return "bool" }

func (f *secretFlag) IsBoolFlag() bool { return true }

func (f *secretFlag) Set(v string) error {
	f.set = true
	if v != "true" {
		f.inline = true
	}
	return nil
}

// editRequest holds the settings tess edit changes. A nil field is left as it
// is.
type editRequest struct {
	digits    *int
	period    *int
	algorithm *string
	typ       *string
	counter   *int64
	secret    *string
}

func (r editRequest) isEmpty() bool {
	return r.digits == nil && r.period == nil && r.algorithm == nil &&
		r.typ == nil && r.counter == nil && r.secret == nil
}

func newEditCmd() *cobra.Command {
	var (
		digits    int
		period    int
		algorithm string
		typ       string
		counter   int64
		secret    secretFlag
	)
	cmd := &cobra.Command{
		Use:   "edit <query>",
		Short: "Change an account's digits, period, algorithm, type, counter or secret",
		Long: `Change the settings that make an account's codes.

  tess edit github --digits 8
  tess edit bank --type hotp --counter 12
  tess edit github --secret            # asks for the new secret
  pbpaste | tess edit github --secret  # reads it from stdin`,
		Args: func(cmd *cobra.Command, args []string) error {
			if secret.set && len(args) > 1 {
				return errSecretFlagValue
			}
			return cobra.ExactArgs(1)(cmd, args)
		},
		ValidArgsFunction: completeFirstArg,
		RunE: func(cmd *cobra.Command, args []string) error {
			if secret.inline {
				return errSecretFlagValue
			}
			f := cmd.Flags()
			var r editRequest
			if f.Changed("digits") {
				r.digits = &digits
			}
			if f.Changed("period") {
				r.period = &period
			}
			if f.Changed("algorithm") {
				r.algorithm = &algorithm
			}
			if f.Changed("type") {
				r.typ = &typ
			}
			if f.Changed("counter") {
				r.counter = &counter
			}
			if secret.set {
				key, err := readSecretFromStdin()
				if err != nil {
					return err
				}
				if strings.TrimSpace(key) == "" {
					return errEmptySecret
				}
				r.secret = &key
			}
			if r.isEmpty() {
				return fmt.Errorf("nothing to change: pass --digits, --period, --algorithm, --type, --counter or --secret")
			}
			return editAccount(cmd, args[0], r)
		},
	}
	f := cmd.Flags()
	f.IntVar(&digits, "digits", 0, "code digits, 6 to 8")
	f.IntVar(&period, "period", 0, "seconds each code lasts (totp, steam)")
	f.StringVar(&algorithm, "algorithm", "", "SHA1, SHA256 or SHA512")
	f.StringVar(&typ, "type", "", "totp, hotp or steam")
	f.Int64Var(&counter, "counter", 0, "next counter value (hotp)")
	f.VarPF(&secret, "secret", "", "replace the secret, read from a prompt or stdin").NoOptDefVal = "true"
	return cmd
}

// editAccount applies r to the account query names and saves it.
func editAccount(cmd *cobra.Command, query string, r editRequest) error {
	s, err := openSession()
	if err != nil {
		return err
	}
	defer s.close()
	idx, err := s.single(cmd, query)
	if err != nil {
		return err
	}
	old := s.accounts[idx]
	edited, err := applyEdit(old, r)
	if err != nil {
		return err
	}
	if reflect.DeepEqual(old, edited) {
		out(cmd, "%s already has those settings", label(old))
		return nil
	}
	edited.UpdatedAt = now().Unix()
	s.accounts[idx] = edited
	if err := s.save(); err != nil {
		return err
	}
	out(cmd, "Updated %s. Its codes change from now on.", label(edited))
	return nil
}

// applyEdit returns a with r applied, or an error naming the first setting the
// result would not accept. a is left unchanged.
func applyEdit(a account.Account, r editRequest) (account.Account, error) {
	wasSteam := a.Type == account.Steam
	if r.typ != nil {
		t := account.Type(strings.ToLower(strings.TrimSpace(*r.typ)))
		switch t {
		case account.TOTP, account.HOTP, account.Steam:
		default:
			return a, fmt.Errorf("invalid --type %q: use totp, hotp or steam", *r.typ)
		}
		a.Type = t
	}
	if r.secret != nil {
		raw, err := decodeEditSecret(*r.secret, a.Type)
		if err != nil {
			return a, err
		}
		a.Secret = raw
	}
	if err := applyEditDigits(&a, r.digits, wasSteam); err != nil {
		return a, err
	}
	if err := applyEditPeriod(&a, r.period); err != nil {
		return a, err
	}
	if err := applyEditCounter(&a, r.counter); err != nil {
		return a, err
	}
	if r.algorithm != nil {
		alg := strings.ToUpper(strings.TrimSpace(*r.algorithm))
		switch alg {
		case "SHA1", "SHA256", "SHA512":
		default:
			return a, fmt.Errorf("invalid --algorithm %q: use SHA1, SHA256 or SHA512", *r.algorithm)
		}
		a.Algorithm = alg
	}
	if err := a.Validate(); err != nil {
		return a, err
	}
	return a, nil
}

func decodeEditSecret(secret string, t account.Type) ([]byte, error) {
	if t == account.Steam {
		return decodeSteamSecret(secret)
	}
	raw, err := base32x.Decode(strings.TrimSpace(secret))
	if err != nil {
		return nil, fmt.Errorf("invalid base32 secret: %w", err)
	}
	return raw, nil
}

func applyEditDigits(a *account.Account, digits *int, wasSteam bool) error {
	if a.Type == account.Steam {
		if digits != nil && *digits != 5 {
			return fmt.Errorf("steam codes have 5 digits; leave out --digits")
		}
		a.Digits = 5
		return nil
	}
	switch {
	case digits != nil:
		a.Digits = *digits
	case wasSteam:
		a.Digits = 6
	}
	if a.Digits < 6 || a.Digits > 8 {
		return fmt.Errorf("--digits must be 6, 7 or 8")
	}
	return nil
}

func applyEditPeriod(a *account.Account, period *int) error {
	if a.Type == account.HOTP {
		if period != nil {
			return fmt.Errorf("--period does not apply to hotp accounts")
		}
		return nil
	}
	if period != nil {
		if *period <= 0 {
			return fmt.Errorf("--period must be a positive number of seconds")
		}
		a.Period = *period
	}
	if a.Period <= 0 {
		a.Period = 30
	}
	return nil
}

func applyEditCounter(a *account.Account, counter *int64) error {
	if a.Type != account.HOTP {
		if counter != nil {
			return fmt.Errorf("--counter applies to hotp accounts only")
		}
		a.Counter = 0
		return nil
	}
	if counter != nil {
		if *counter < 0 || *counter > account.MaxCounter {
			return fmt.Errorf("--counter must be from 0 to %d", account.MaxCounter)
		}
		a.Counter = *counter
	}
	return nil
}
