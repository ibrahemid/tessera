package main

import (
	"crypto/subtle"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/spf13/cobra"
)

// maxVerifyWindow bounds --window: a wider window accepts codes from so far
// away that the check stops meaning anything.
const maxVerifyWindow = 100

var errVerifyWindow = fmt.Errorf("--window takes 0 to %d steps", maxVerifyWindow)

// codeFormatError reports a code that cannot be one of the account's codes.
type codeFormatError struct {
	label string
	want  string
}

func (e *codeFormatError) Error() string {
	return fmt.Sprintf("%s codes are %s", e.label, e.want)
}

// codeMismatchError reports a well-formed code that matches no step or counter
// in the window.
type codeMismatchError struct {
	label  string
	window int
	hotp   bool
}

func (e *codeMismatchError) Error() string {
	if e.hotp {
		return fmt.Sprintf("the code does not match %s at its counter or the %d after it", e.label, e.window)
	}
	return fmt.Sprintf("the code does not match %s within %d %s of now", e.label, e.window, plural(e.window, "step", "steps"))
}

func newVerifyCmd() *cobra.Command {
	var window int
	cmd := &cobra.Command{
		Use:   "verify <query> <code>",
		Short: "Check a code against an account",
		Long: `Check whether a code belongs to an account. tess verify exits 0 when the
code is valid and 1 when it is not, and never copies.

For a time-based account the code may come from up to --window steps before
or after now. For an HOTP account it may come from the stored counter or the
--window counters after it; the counter is never changed.`,
		Args:              cobra.ExactArgs(2),
		ValidArgsFunction: completeFirstArg,
		RunE: func(cmd *cobra.Command, args []string) error {
			if window < 0 || window > maxVerifyWindow {
				return errVerifyWindow
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
			return verifyCode(cmd, s.accounts[idx], args[1], window)
		},
	}
	cmd.Flags().IntVar(&window, "window", 1, "steps (or HOTP counters) away from now that still count as valid")
	return cmd
}

// verifyCode reports whether input is one of a's codes within window, as the
// exit status and one line on stdout.
func verifyCode(cmd *cobra.Command, a account.Account, input string, window int) error {
	got, err := normalizeCode(a, input)
	if err != nil {
		return err
	}
	if a.Type == account.HOTP {
		counter, ok, err := matchCounter(a, got, window)
		if err != nil {
			return err
		}
		if !ok {
			return &exitError{code: 1, err: &codeMismatchError{label: label(a), window: window, hotp: true}}
		}
		out(cmd, "The code matches %s at counter %d.", label(a), counter)
		return nil
	}
	offset, ok, err := matchStep(a, got, now(), window)
	if err != nil {
		return err
	}
	if !ok {
		return &exitError{code: 1, err: &codeMismatchError{label: label(a), window: window}}
	}
	switch {
	case offset < 0:
		out(cmd, "The code matches %s, from %d %s before now.", label(a), -offset, plural(-offset, "step", "steps"))
	case offset > 0:
		out(cmd, "The code matches %s, from %d %s after now.", label(a), offset, plural(offset, "step", "steps"))
	default:
		out(cmd, "The code matches %s.", label(a))
	}
	return nil
}

// normalizeCode drops the spaces tess groups a code with and checks the
// result has the account's length and alphabet.
func normalizeCode(a account.Account, input string) (string, error) {
	c := strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, input)
	if a.Type == account.Steam {
		c = strings.ToUpper(c)
		if len(c) != 5 || strings.Trim(c, "23456789BCDFGHJKMNPQRTVWXY") != "" {
			return "", &codeFormatError{label: label(a), want: "5 letters and digits"}
		}
		return c, nil
	}
	if len(c) != a.Digits || strings.Trim(c, "0123456789") != "" {
		return "", &codeFormatError{label: label(a), want: fmt.Sprintf("%d digits", a.Digits)}
	}
	return c, nil
}

// matchStep returns the step offset from t, in -window..window and nearest
// first, whose code is c.
func matchStep(a account.Account, c string, t time.Time, window int) (int, bool, error) {
	for d := 0; d <= window; d++ {
		for _, offset := range []int{-d, d} {
			at, ok := stepStart(t, a.Period, int64(offset))
			if !ok {
				if d == 0 {
					break
				}
				continue
			}
			want, err := genCode(a, at)
			if err != nil {
				return 0, false, err
			}
			if subtle.ConstantTimeCompare([]byte(want), []byte(c)) == 1 {
				return offset, true, nil
			}
			if d == 0 {
				break
			}
		}
	}
	return 0, false, nil
}

// matchCounter returns the counter, from a's stored counter through window
// past it, whose code is c. a is a copy, so the stored counter is untouched.
func matchCounter(a account.Account, c string, window int) (int64, bool, error) {
	start := a.Counter
	for k := int64(0); k <= int64(window) && start <= account.MaxCounter-k; k++ {
		a.Counter = start + k
		want, err := genCode(a, time.Time{})
		if err != nil {
			return 0, false, err
		}
		if subtle.ConstantTimeCompare([]byte(want), []byte(c)) == 1 {
			return a.Counter, true, nil
		}
	}
	return 0, false, nil
}

// plural picks the word for n of something.
func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
