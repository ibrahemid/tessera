package main

import (
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"

	"github.com/ibrahemid/tessera/go/internal/base32x"
	"github.com/ibrahemid/tessera/go/internal/store"
	"github.com/ibrahemid/tessera/go/internal/ui"
)

// exitError carries a process exit status other than the default 1.
type exitError struct {
	code int
	err  error
}

func (e *exitError) Error() string { return e.err.Error() }
func (e *exitError) Unwrap() error { return e.err }

// passphraseSourceError reports that a passphrase tess did not prompt for, from
// an environment variable or a secret store, does not open the vault, so the
// user knows which setting to fix. source is the variable name; store, when
// set, names the secret store instead.
type passphraseSourceError struct {
	source string
	store  string
	path   string
	err    error
}

func (e *passphraseSourceError) Error() string {
	if e.store != "" {
		return fmt.Sprintf("the %s passphrase doesn't open %s; run `tess vault forget`", e.store, e.path)
	}
	return fmt.Sprintf("the passphrase in $%s doesn't open %s", e.source, e.path)
}

func (e *passphraseSourceError) Unwrap() error { return e.err }

var (
	packagePrefix  = regexp.MustCompile(`(^|: )(store|vault|otpauth|migration|base32x|secretstore|qr|account|importers|exporters|detect|procenv|code|otp): `)
	issuerMismatch = regexp.MustCompile(`issuer mismatch \(label ("(?:[^"\\]|\\.)*"), param ("(?:[^"\\]|\\.)*")\)`)
)

// userMessage turns an error from any layer into the sentence tess prints: the
// internal package prefixes are dropped, and the errors a user can fix say how.
func userMessage(err error) string {
	var source *passphraseSourceError
	switch {
	case errors.As(err, &source):
		return source.Error()
	case errors.Is(err, store.ErrNotFound):
		path, rerr := store.Resolve(vaultPath)
		if rerr != nil {
			return "no vault found. Create one with `tess vault init`, or pass --vault to use another file."
		}
		return fmt.Sprintf("no vault at %s. Create one with `tess vault init`, or pass --vault to use another file.", path)
	case errors.Is(err, base32x.ErrInvalid):
		return "invalid setup key: base32 uses A-Z and 2-7"
	}
	msg := err.Error()
	if m := issuerMismatch.FindStringSubmatch(msg); m != nil {
		label, lerr := strconv.Unquote(m[1])
		param, perr := strconv.Unquote(m[2])
		if lerr == nil && perr == nil {
			return fmt.Sprintf("issuer differs between the label (%s) and the issuer parameter (%s); add with --issuer to choose", label, param)
		}
	}
	for {
		stripped := packagePrefix.ReplaceAllString(msg, "$1")
		if stripped == msg {
			return msg
		}
		msg = stripped
	}
}

// reportError prints err the way the user sees it and returns the exit status.
func reportError(w io.Writer, err error) int {
	fmt.Fprintln(w, "error:", ui.Sanitize(userMessage(err)))
	var exit *exitError
	if errors.As(err, &exit) {
		return exit.code
	}
	return 1
}
