// Package detect classifies a text payload (an otpauth URI, a Google
// Authenticator migration URI, an app-export JSON blob, or a bare base32 setup
// key) and parses it into the canonical account model. It performs no I/O and
// never logs secrets: failures are reported per item with a redacted display
// form. The classification precedence is the interop contract in
// /spec/otpauth.md ("Input detection"); the Swift InputDetect must match.
package detect

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/base32x"
	"github.com/ibrahemid/tessera/go/internal/importers"
	"github.com/ibrahemid/tessera/go/internal/migration"
	"github.com/ibrahemid/tessera/go/internal/otpauth"
)

// Kind is the classification of a single text payload.
type Kind int

const (
	// Invalid is text no rule recognizes.
	Invalid Kind = iota
	// Migration is an otpauth-migration:// URI (Google Authenticator export).
	Migration
	// OTPAuth is a single otpauth:// URI.
	OTPAuth
	// ExportJSON is an app-export JSON blob (Aegis, 2FAS, or Raivo).
	ExportJSON
	// SetupKey is a bare base32 secret entered as a setup key.
	SetupKey
)

// String reports the spec-canonical name for a Kind.
func (k Kind) String() string {
	switch k {
	case Migration:
		return "migration"
	case OTPAuth:
		return "otpauth"
	case ExportJSON:
		return "export-json"
	case SetupKey:
		return "setup-key"
	default:
		return "invalid"
	}
}

// minSetupKeyLen is the minimum length (after stripping spaces and dashes) for a
// token to be treated as a base32 setup key: 16 base32 chars = 10 secret bytes.
const minSetupKeyLen = 16

// Classify returns the Kind of a single text payload by the spec precedence:
// migration URI, otpauth URI, app-export JSON (leading '[' or '{'), then the
// base32 setup-key guardrail. Anything else is Invalid.
func Classify(text string) Kind {
	t := strings.TrimSpace(text)
	if t == "" {
		return Invalid
	}
	lower := strings.ToLower(t)
	switch {
	case strings.HasPrefix(lower, "otpauth-migration://"):
		return Migration
	case strings.HasPrefix(lower, "otpauth://"):
		return OTPAuth
	}
	switch t[0] {
	case '[', '{':
		return ExportJSON
	}
	if IsLikelyBase32Secret(t) {
		return SetupKey
	}
	return Invalid
}

// IsLikelyBase32Secret reports whether s qualifies as a base32 setup key under
// the spec guardrail: after stripping ASCII spaces and '-', it is a single token
// matching ^[A-Za-z2-7]+$ (case-insensitive), at least 16 chars, that decodes
// cleanly under the lenient base32 rules.
func IsLikelyBase32Secret(s string) bool {
	clean := stripSpacesDashes(s)
	if len(clean) < minSetupKeyLen {
		return false
	}
	for _, r := range clean {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r >= '2' && r <= '7':
		default:
			return false
		}
	}
	_, err := base32x.Decode(clean)
	return err == nil
}

func stripAllWhitespace(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if unicode.IsSpace(r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func stripSpacesDashes(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r == ' ' || r == '-' {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// ItemError records one item in a batch that failed to parse. Input is a
// redacted display form (kind plus a short prefix), never a full secret.
type ItemError struct {
	Line  int
	Input string
	Err   error
}

func (e ItemError) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("line %d: %v", e.Line, e.Err)
	}
	return e.Err.Error()
}

// redact returns a safe display form for a payload: its kind and the first four
// characters, so an error can name the item without exposing the secret.
func redact(kind Kind, raw string) string {
	t := strings.TrimSpace(raw)
	r := []rune(t)
	prefix := string(r)
	if len(r) > 4 {
		prefix = string(r[:4]) + "…"
	}
	if prefix == "" {
		return kind.String()
	}
	return kind.String() + " " + prefix
}

// ParseText classifies and parses a text payload into accounts, collecting a
// per-item error for every line or blob that fails instead of aborting. A blob
// whose first non-whitespace byte is '[' or '{' is parsed as a single JSON
// export (app exports are legitimately multiline); everything else is split on
// line breaks and each non-empty line is classified independently.
func ParseText(text string) ([]account.Account, []ItemError) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil, []ItemError{{Line: 1, Input: "", Err: fmt.Errorf("no input")}}
	}
	if trimmed[0] == '[' || trimmed[0] == '{' {
		accts, source, ok, err := importers.Parse([]byte(text))
		if ok {
			if err != nil {
				return nil, []ItemError{{Line: 1, Input: redact(ExportJSON, source), Err: err}}
			}
			return accts, nil
		}
		return nil, []ItemError{{Line: 1, Input: redact(Invalid, trimmed), Err: fmt.Errorf("unrecognized JSON export")}}
	}

	// Wrapped-URI repair (spec § input detection): textareas and mail clients
	// hard-wrap long URIs, so a single URI with embedded line breaks is one
	// URI, not a batch. Applies only when the whole input holds exactly one
	// scheme and the first line alone is a true fragment (fails to parse);
	// a complete first line means the input is a batch.
	if strings.ContainsAny(trimmed, "\n\r") {
		if k := Classify(trimmed); (k == OTPAuth || k == Migration) &&
			strings.Count(strings.ToLower(trimmed), "otpauth") == 1 {
			firstLine := strings.TrimSpace(strings.FieldsFunc(trimmed, func(r rune) bool {
				return r == '\n' || r == '\r'
			})[0])
			joined := stripAllWhitespace(trimmed)
			switch k {
			case Migration:
				if _, err := migration.Parse(firstLine); err != nil {
					if parsed, err := migration.Parse(joined); err == nil {
						return parsed, nil
					}
				}
			case OTPAuth:
				if _, err := otpauth.Parse(firstLine); err != nil {
					if a, err := otpauth.Parse(joined); err == nil {
						return []account.Account{a}, nil
					}
				}
			}
		}
	}

	var accts []account.Account
	var errs []ItemError
	for i, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		lineNo := i + 1
		kind := Classify(line)
		switch kind {
		case Migration:
			parsed, err := migration.Parse(line)
			if err != nil {
				errs = append(errs, ItemError{Line: lineNo, Input: redact(kind, line), Err: err})
				continue
			}
			accts = append(accts, parsed...)
		case OTPAuth:
			a, err := otpauth.Parse(line)
			if err != nil {
				errs = append(errs, ItemError{Line: lineNo, Input: redact(kind, line), Err: err})
				continue
			}
			accts = append(accts, a)
		case ExportJSON:
			parsed, source, ok, err := importers.Parse([]byte(line))
			if !ok {
				errs = append(errs, ItemError{Line: lineNo, Input: redact(Invalid, line), Err: fmt.Errorf("unrecognized JSON export")})
				continue
			}
			if err != nil {
				errs = append(errs, ItemError{Line: lineNo, Input: redact(kind, source), Err: err})
				continue
			}
			accts = append(accts, parsed...)
		case SetupKey:
			accts = append(accts, setupKeyAccount(line))
		default:
			errs = append(errs, ItemError{Line: lineNo, Input: redact(Invalid, line), Err: fmt.Errorf("unrecognized input")})
		}
	}
	return accts, errs
}

// setupKeyAccount builds a TOTP account from a bare base32 setup key with the
// spec defaults (SHA1, 6 digits, 30s period, empty issuer and account). The key
// is already validated by IsLikelyBase32Secret via Classify, so the decode
// cannot fail here.
func setupKeyAccount(key string) account.Account {
	secret, _ := base32x.Decode(stripSpacesDashes(key))
	return account.Account{
		Type:      account.TOTP,
		Secret:    secret,
		Algorithm: "SHA1",
		Digits:    6,
		Period:    30,
	}
}
