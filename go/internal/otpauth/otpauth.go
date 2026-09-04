// Package otpauth parses and emits otpauth:// URIs (Key Uri Format) into the
// canonical account model. base32 is decoded to raw secret bytes at this
// boundary; the vault never stores base32.
package otpauth

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/base32x"
)

// Parse decodes an otpauth:// URI into an Account.
func Parse(uri string) (account.Account, error) {
	var a account.Account
	u, err := url.Parse(strings.TrimSpace(uri))
	if err != nil {
		// url.Error prints the whole URL it failed on, and this one carries a
		// secret. The reason is dropped with it: nothing in it identifies the
		// account, and the caller reports a redacted display form of the input.
		return a, errors.New("otpauth: invalid URI")
	}
	if u.Scheme != "otpauth" {
		return a, fmt.Errorf("otpauth: not an otpauth uri (scheme %q)", u.Scheme)
	}
	switch strings.ToLower(u.Host) {
	case "totp":
		a.Type = account.TOTP
	case "hotp":
		a.Type = account.HOTP
	case "steam":
		a.Type = account.Steam
	default:
		return a, fmt.Errorf("otpauth: unsupported type %q", u.Host)
	}

	issuer, acct, err := splitLabel(u.EscapedPath())
	if err != nil {
		return a, err
	}
	a.Issuer, a.Account = issuer, acct

	q := u.Query()
	secretParam := q.Get("secret")
	if secretParam == "" {
		return a, fmt.Errorf("otpauth: missing secret")
	}
	secret, err := base32x.Decode(secretParam)
	if err != nil {
		return a, fmt.Errorf("otpauth: bad secret: %w", err)
	}
	a.Secret = secret

	if iss := q.Get("issuer"); iss != "" {
		if a.Issuer != "" && a.Issuer != iss {
			return a, fmt.Errorf("otpauth: issuer mismatch (label %q, param %q)", a.Issuer, iss)
		}
		a.Issuer = iss
	}

	// Steam heuristic: issuer "Steam" with a TOTP type is treated as Steam.
	if a.Type == account.TOTP && strings.EqualFold(a.Issuer, "Steam") {
		a.Type = account.Steam
	}

	a.Algorithm = "SHA1"
	if alg := q.Get("algorithm"); alg != "" {
		switch strings.ToUpper(alg) {
		case "SHA1", "SHA256", "SHA512":
			a.Algorithm = strings.ToUpper(alg)
		default:
			return a, fmt.Errorf("otpauth: unsupported algorithm %q", alg)
		}
	}

	a.Digits = 6
	if a.Type == account.Steam {
		a.Digits = 5
	}
	if d := q.Get("digits"); d != "" {
		n, err := strconv.Atoi(d)
		bad := err != nil || n < 6 || n > 8
		if a.Type == account.Steam {
			bad = err != nil || n != 5
		}
		if bad {
			return a, fmt.Errorf("otpauth: invalid digits %q", d)
		}
		a.Digits = n
	}

	a.Period = 30
	if p := q.Get("period"); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil || n <= 0 {
			return a, fmt.Errorf("otpauth: invalid period %q", p)
		}
		a.Period = n
	}

	if a.Type == account.HOTP {
		c := q.Get("counter")
		if c == "" {
			return a, fmt.Errorf("otpauth: hotp requires counter")
		}
		n, err := strconv.ParseInt(c, 10, 64)
		if err != nil || n < 0 {
			return a, fmt.Errorf("otpauth: invalid counter %q", c)
		}
		a.Counter = n
	}

	return a, nil
}

// splitLabel applies the spec label rules (spec/otpauth.md § label encoding) to
// the still-encoded path: split on the first literal ':', then percent-decode
// each side exactly once. url.URL.Path is already decoded, so splitting there
// would break a label whose issuer or account legitimately contains a colon, and
// decoding it a second time corrupts any label containing a literal '%'.
func splitLabel(escapedPath string) (issuer, acct string, err error) {
	label := strings.TrimPrefix(escapedPath, "/")
	rawIssuer, rawAcct, hasIssuer := strings.Cut(label, ":")
	if !hasIssuer {
		rawIssuer, rawAcct = "", label
	}
	issuer, err = url.PathUnescape(rawIssuer)
	if err != nil {
		return "", "", fmt.Errorf("otpauth: bad label issuer: %w", err)
	}
	acct, err = url.PathUnescape(rawAcct)
	if err != nil {
		return "", "", fmt.Errorf("otpauth: bad label account: %w", err)
	}
	return strings.TrimSpace(issuer), strings.TrimSpace(acct), nil
}

// escapeLabelPart percent-encodes one label component. url.PathEscape leaves
// ':' alone, which would be indistinguishable from the issuer/account separator
// on the way back in, so it is encoded explicitly.
func escapeLabelPart(s string) string {
	return strings.ReplaceAll(url.PathEscape(s), ":", "%3A")
}

// Format emits an otpauth:// URI for the account (secret base32, no padding).
func Format(a account.Account) string {
	typ := "totp"
	switch a.Type {
	case account.HOTP:
		typ = "hotp"
	case account.Steam:
		typ = "steam"
	}
	label := escapeLabelPart(a.Account)
	if a.Issuer != "" {
		label = escapeLabelPart(a.Issuer) + ":" + label
	}
	v := url.Values{}
	v.Set("secret", base32x.EncodeNoPad(a.Secret))
	if a.Issuer != "" {
		v.Set("issuer", a.Issuer)
	}
	if a.Algorithm != "" && a.Algorithm != "SHA1" {
		v.Set("algorithm", a.Algorithm)
	}
	if a.Type == account.Steam {
		v.Set("digits", "5")
	} else if a.Digits != 0 && a.Digits != 6 {
		v.Set("digits", strconv.Itoa(a.Digits))
	}
	if a.Type != account.HOTP && a.Period != 0 && a.Period != 30 {
		v.Set("period", strconv.Itoa(a.Period))
	}
	if a.Type == account.HOTP {
		v.Set("counter", strconv.FormatInt(a.Counter, 10))
	}
	return fmt.Sprintf("otpauth://%s/%s?%s", typ, label, encodeQuery(v))
}

// encodeQuery emits the query in the form spec/otpauth.md § query encoding
// requires: a space is %20, never '+'. url.Values.Encode writes the legacy form
// form '+' for a space, which a parser built on URLComponents (the Swift core)
// reads as a literal plus. Every other byte is already percent-escaped by
// Encode — a literal '+' in a value comes out as %2B — so '+' in its output can
// only be a space.
func encodeQuery(v url.Values) string {
	return strings.ReplaceAll(v.Encode(), "+", "%20")
}
