package otpauth

import (
	"testing"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/base32x"
	"github.com/ibrahemid/tessera/go/internal/spectest"
)

type vectors struct {
	OTPAuth struct {
		Parse []struct {
			URI       string `json:"uri"`
			Type      string `json:"type"`
			Issuer    string `json:"issuer"`
			Account   string `json:"account"`
			SecretB32 string `json:"secret_b32"`
			Period    int    `json:"period"`
			Digits    int    `json:"digits"`
			Counter   int64  `json:"counter"`
			Algorithm string `json:"algorithm"`
		} `json:"parse"`
	} `json:"otpauth_uri"`
}

func TestParse(t *testing.T) {
	var v vectors
	spectest.Load(t, &v)
	for _, c := range v.OTPAuth.Parse {
		a, err := Parse(c.URI)
		if err != nil {
			t.Fatalf("Parse(%q): %v", c.URI, err)
		}
		if string(a.Type) != c.Type {
			t.Errorf("%s: type=%s want %s", c.URI, a.Type, c.Type)
		}
		if a.Issuer != c.Issuer {
			t.Errorf("%s: issuer=%q want %q", c.URI, a.Issuer, c.Issuer)
		}
		if a.Account != c.Account {
			t.Errorf("%s: account=%q want %q", c.URI, a.Account, c.Account)
		}
		if a.Algorithm != c.Algorithm {
			t.Errorf("%s: algorithm=%q want %q", c.URI, a.Algorithm, c.Algorithm)
		}
		if a.Digits != c.Digits {
			t.Errorf("%s: digits=%d want %d", c.URI, a.Digits, c.Digits)
		}
		if c.Type == "hotp" && a.Counter != c.Counter {
			t.Errorf("%s: counter=%d want %d", c.URI, a.Counter, c.Counter)
		}
		want, _ := base32x.Decode(c.SecretB32)
		if string(a.Secret) != string(want) {
			t.Errorf("%s: secret mismatch", c.URI)
		}
	}
}

func TestFormatRoundTrip(t *testing.T) {
	a := account.Account{
		ID: "x", Type: account.TOTP, Issuer: "ACME Co", Account: "john@example.com",
		Secret: []byte("12345678901234567890"), Algorithm: "SHA1", Digits: 6, Period: 30,
	}
	uri := Format(a)
	back, err := Parse(uri)
	if err != nil {
		t.Fatalf("reparse %q: %v", uri, err)
	}
	if back.Issuer != a.Issuer || back.Account != a.Account ||
		string(back.Secret) != string(a.Secret) || back.Digits != a.Digits {
		t.Errorf("round trip mismatch: %s -> %+v", uri, back)
	}
}

func TestParseRejectsNonOTPAuth(t *testing.T) {
	for _, bad := range []string{"https://example.com", "otpauth://bogus/x?secret=AA", "otpauth://totp/x"} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("expected error for %q", bad)
		}
	}
}
