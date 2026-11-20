package main

import (
	"errors"
	"fmt"
	"io"
	"regexp"
	"runtime"
	"strings"

	"github.com/ibrahemid/tessera/go/internal/base32x"
	"github.com/ibrahemid/tessera/go/internal/otpauth"
	"github.com/ibrahemid/tessera/go/internal/store"
	"github.com/ibrahemid/tessera/go/internal/ui"
	"github.com/ibrahemid/tessera/go/internal/vault"
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

type restorableError struct {
	path string
	err  error
}

func (e *restorableError) Error() string { return e.err.Error() }
func (e *restorableError) Unwrap() error { return e.err }

func withRestoreHint(path string, err error) error {
	if err == nil || !(errors.Is(err, vault.ErrWrongPassphrase) || errors.Is(err, vault.ErrCorrupt)) || !store.HasBackup(path) {
		return err
	}
	return &restorableError{path: path, err: err}
}

func restoreCommand(path string) string {
	cmd := "tess vault restore --bak"
	if def, err := store.Resolve(""); err == nil && def == path {
		return cmd
	}
	return cmd + " --vault " + quoteArg(path)
}

var (
	posixSafe   = regexp.MustCompile(`^[A-Za-z0-9_./:@%+=,~-]+$`)
	windowsSafe = regexp.MustCompile(`^[A-Za-z0-9_.:\\/~+=-]+$`)
)

// quoteArg quotes s for the shell people use on this platform.
func quoteArg(s string) string {
	if runtime.GOOS == "windows" {
		return quotePowerShell(s)
	}
	return quotePOSIX(s)
}

// quotePOSIX quotes s for sh, bash and zsh.
func quotePOSIX(s string) string {
	if posixSafe.MatchString(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// quotePowerShell quotes s for PowerShell. Unless s holds a character that
// expands or ends a PowerShell double-quoted string, the result is a
// double-quoted string that cmd.exe reads the same way.
func quotePowerShell(s string) string {
	switch {
	case windowsSafe.MatchString(s):
		return s
	case !strings.ContainsAny(s, "\"$`“”„"):
		return `"` + s + `"`
	}
	var b strings.Builder
	b.WriteByte('\'')
	for _, r := range s {
		if strings.ContainsRune("'‘’‚‛", r) {
			b.WriteRune(r)
		}
		b.WriteRune(r)
	}
	b.WriteByte('\'')
	return b.String()
}

var (
	packagePrefix = regexp.MustCompile(`(^|: )(store|vault|otpauth|migration|base32x|secretstore|qr|account|importers|exporters|detect|procenv|code|otp|screenshot|clipboard|dbusx): `)
)

// userMessage turns an error from any layer into the sentence tess prints: the
// internal package prefixes are dropped, and the errors a user can fix say how.
func userMessage(err error) string {
	var restorable *restorableError
	var withBak *store.BackupAvailableError
	var source *passphraseSourceError
	switch {
	case errors.As(err, &restorable):
		lead := "The vault as it was before the last write"
		if errors.Is(restorable.err, vault.ErrWrongPassphrase) {
			lead = "If the passphrase is right, the vault file is damaged. The vault as it was before the last write"
		}
		return fmt.Sprintf("%s. %s is at %s; restore it with `%s`.", userMessage(restorable.err), lead, store.BackupPath(restorable.path), restoreCommand(restorable.path))
	case errors.As(err, &withBak):
		return fmt.Sprintf("cannot read the vault at %s (%s). The vault as it was before the last write is at %s; restore it with `%s`.", withBak.Path, userMessage(withBak.Err), withBak.Backup, restoreCommand(withBak.Path))
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
	var mismatch *otpauth.IssuerMismatchError
	if errors.As(err, &mismatch) {
		return fmt.Sprintf("issuer differs between the label (%s) and the issuer parameter (%s); add with --issuer to choose", mismatch.Label, mismatch.Param)
	}
	msg := err.Error()
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
