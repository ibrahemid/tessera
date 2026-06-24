// Package otpauth parses and emits otpauth:// URIs (Key Uri Format) into the
// canonical account model. base32 is decoded to raw secret bytes at this
// boundary; the vault never stores base32.
package otpauth

import (
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
		return a, fmt.Errorf("otpauth: parse uri: %w", err)
	}
	if u.Scheme != "otpauth" {
		return a, fmt.Errorf("otpauth: not an otpauth uri (scheme %q)", u.Scheme)
	}
	switch strings.ToLower(u.Host) {
	case "totp":
		a.Type = account.TOTP
	case "hotp":
		a.Type = account.HOTP
	default:
		return a, fmt.Errorf("otpauth: unsupported type %q", u.Host)
	}

	label := strings.TrimPrefix(u.Path, "/")
	label, err = url.PathUnescape(label)
	if err != nil {
		return a, fmt.Errorf("otpauth: bad label: %w", err)
	}
	if i := strings.Index(label, ":"); i >= 0 {
		a.Issuer = strings.TrimSpace(label[:i])
		a.Account = strings.TrimSpace(label[i+1:])
	} else {
		a.Account = strings.TrimSpace(label)
	}

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
	if d := q.Get("digits"); d != "" {
		n, err := strconv.Atoi(d)
		if err != nil || n < 6 || n > 8 {
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

	// Steam heuristic: issuer "Steam" with a TOTP type is treated as Steam.
	if a.Type == account.TOTP && strings.EqualFold(a.Issuer, "Steam") {
		a.Type = account.Steam
	}
	return a, nil
}

// Format emits an otpauth:// URI for the account (secret base32, no padding).
func Format(a account.Account) string {
	typ := "totp"
	if a.Type == account.HOTP {
		typ = "hotp"
	}
	label := a.Account
	if a.Issuer != "" {
		label = a.Issuer + ":" + a.Account
	}
	v := url.Values{}
	v.Set("secret", base32x.EncodeNoPad(a.Secret))
	if a.Issuer != "" {
		v.Set("issuer", a.Issuer)
	}
	if a.Algorithm != "" && a.Algorithm != "SHA1" {
		v.Set("algorithm", a.Algorithm)
	}
	if a.Digits != 0 && a.Digits != 6 {
		v.Set("digits", strconv.Itoa(a.Digits))
	}
	if a.Type != account.HOTP && a.Period != 0 && a.Period != 30 {
		v.Set("period", strconv.Itoa(a.Period))
	}
	if a.Type == account.HOTP {
		v.Set("counter", strconv.FormatInt(a.Counter, 10))
	}
	return fmt.Sprintf("otpauth://%s/%s?%s", typ, url.PathEscape(label), v.Encode())
}
