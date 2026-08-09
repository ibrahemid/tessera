package otpauth

import (
	"strings"
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

// TestLabelRoundTrip covers spec/otpauth.md § label encoding: every one of
// these issuer/account pairs must survive Format -> Parse unchanged. Colons are
// the interesting case (the label separator is a colon) and '%' is the one that
// used to be corrupted by decoding the label twice.
func TestLabelRoundTrip(t *testing.T) {
	cases := []struct{ issuer, account string }{
		{"ACME Co", "john@example.com"},
		{"Acme: Prod", "ops"},
		{"Acme", "a:b"},
		{"Acme: Prod", "a:b@example.com"},
		{"25% off", "sale%20"},
		{"a%41b", "c%2541d"},
		{"Café Ω", "рабочий@пример.рф"},
		{"slash/issuer", "slash/account"},
		{"", "no-issuer@example.com"},
		{"plus+issuer", "plus+account"},
	}
	for _, tc := range cases {
		a := account.Account{
			ID: "x", Type: account.TOTP, Issuer: tc.issuer, Account: tc.account,
			Secret: []byte("12345678901234567890"), Algorithm: "SHA1", Digits: 6, Period: 30,
		}
		uri := Format(a)
		back, err := Parse(uri)
		if err != nil {
			t.Errorf("Parse(Format(%q/%q)) = %q: %v", tc.issuer, tc.account, uri, err)
			continue
		}
		if back.Issuer != tc.issuer || back.Account != tc.account {
			t.Errorf("round trip %q/%q via %q -> %q/%q", tc.issuer, tc.account, uri, back.Issuer, back.Account)
		}
	}
}

// TestFormatEncodesColonInLabelComponents pins the wire form: the only bare ':'
// in the label is the separator.
func TestFormatEncodesColonInLabelComponents(t *testing.T) {
	a := account.Account{
		ID: "x", Type: account.TOTP, Issuer: "Acme:Prod", Account: "a:b",
		Secret: []byte("12345678901234567890"), Algorithm: "SHA1", Digits: 6, Period: 30,
	}
	uri := Format(a)
	label := strings.TrimPrefix(strings.SplitN(uri, "?", 2)[0], "otpauth://totp/")
	if label != "Acme%3AProd:a%3Ab" {
		t.Errorf("label = %q, want %q", label, "Acme%3AProd:a%3Ab")
	}
	if strings.Count(label, ":") != 1 {
		t.Errorf("label %q must contain exactly one bare colon", label)
	}
}

// TestParseDecodesLabelExactlyOnce pins that a literal '%' in a label is data,
// not the start of an escape to be resolved a second time.
func TestParseDecodesLabelExactlyOnce(t *testing.T) {
	cases := []struct{ uri, issuer, account string }{
		{"otpauth://totp/%25off?secret=JBSWY3DPEHPK3PXP", "", "%off"},
		{"otpauth://totp/a%2541b?secret=JBSWY3DPEHPK3PXP", "", "a%41b"},
		{"otpauth://totp/a%41b?secret=JBSWY3DPEHPK3PXP", "", "aAb"},
		{"otpauth://totp/Acme%3AProd:me?secret=JBSWY3DPEHPK3PXP", "Acme:Prod", "me"},
		{"otpauth://totp/Acme:a%3Ab?secret=JBSWY3DPEHPK3PXP", "Acme", "a:b"},
	}
	for _, tc := range cases {
		got, err := Parse(tc.uri)
		if err != nil {
			t.Errorf("Parse(%q): %v", tc.uri, err)
			continue
		}
		if got.Issuer != tc.issuer || got.Account != tc.account {
			t.Errorf("Parse(%q) = %q/%q, want %q/%q", tc.uri, got.Issuer, got.Account, tc.issuer, tc.account)
		}
	}
}

// TestFormatEncodesSpaceAsPercent20 pins the wire form of a space in a query
// value (spec/otpauth.md § query encoding). The legacy '+' form is read as a
// literal plus by percent-decoding parsers such as the Swift core, which then
// rejects the URI on the issuer/label agreement check.
func TestFormatEncodesSpaceAsPercent20(t *testing.T) {
	a := account.Account{
		ID: "x", Type: account.TOTP, Issuer: "Acme Corp", Account: "john@example.com",
		Secret: []byte("12345678901234567890"), Algorithm: "SHA1", Digits: 6, Period: 30,
	}
	uri := Format(a)
	query := strings.SplitN(uri, "?", 2)[1]
	if !strings.Contains(query, "issuer=Acme%20Corp") {
		t.Errorf("query = %q, want issuer=Acme%%20Corp", query)
	}
	if strings.Contains(query, "+") {
		t.Errorf("query = %q must not contain a bare '+'", query)
	}
	if !strings.Contains(uri, "otpauth://totp/Acme%20Corp:john@example.com?") {
		t.Errorf("uri = %q, want a %%20 in the label too", uri)
	}
}

// TestFormatEncodesLiteralPlus pins that a real '+' is emitted as %2B in a
// query value, so a parser that reads '+' as a space cannot turn it into
// whitespace. In the label (a path component) '+' stays a literal plus.
func TestFormatEncodesLiteralPlus(t *testing.T) {
	a := account.Account{
		ID: "x", Type: account.TOTP, Issuer: "C++ Forum", Account: "dev+x@example.com",
		Secret: []byte("12345678901234567890"), Algorithm: "SHA1", Digits: 6, Period: 30,
	}
	uri := Format(a)
	query := strings.SplitN(uri, "?", 2)[1]
	if !strings.Contains(query, "issuer=C%2B%2B%20Forum") {
		t.Errorf("query = %q, want issuer=C%%2B%%2B%%20Forum", query)
	}
	back, err := Parse(uri)
	if err != nil {
		t.Fatalf("reparse %q: %v", uri, err)
	}
	if back.Issuer != a.Issuer || back.Account != a.Account {
		t.Errorf("round trip %q -> %q/%q", uri, back.Issuer, back.Account)
	}
}

// TestParseAcceptsPlusAsSpace covers the other emitters (and Tessera's own
// older exports) that still write a space as '+': the parser must read it as a
// space, and a plus written as %2B must survive as a plus.
func TestParseAcceptsPlusAsSpace(t *testing.T) {
	cases := []struct{ uri, issuer, account string }{
		{"otpauth://totp/Acme%20Corp:john@example.com?secret=JBSWY3DPEHPK3PXP&issuer=Acme+Corp", "Acme Corp", "john@example.com"},
		{"otpauth://totp/john@example.com?secret=JBSWY3DPEHPK3PXP&issuer=Acme+Corp", "Acme Corp", "john@example.com"},
		{"otpauth://totp/C++%20Forum:dev%2Bx@example.com?secret=JBSWY3DPEHPK3PXP&issuer=C%2B%2B%20Forum", "C++ Forum", "dev+x@example.com"},
	}
	for _, tc := range cases {
		got, err := Parse(tc.uri)
		if err != nil {
			t.Errorf("Parse(%q): %v", tc.uri, err)
			continue
		}
		if got.Issuer != tc.issuer || got.Account != tc.account {
			t.Errorf("Parse(%q) = %q/%q, want %q/%q", tc.uri, got.Issuer, got.Account, tc.issuer, tc.account)
		}
	}
}

func TestSteamRoundTrip(t *testing.T) {
	a := account.Account{
		ID: "s", Type: account.Steam, Issuer: "Steam", Account: "gabe",
		Secret: []byte("12345678901234567890"), Algorithm: "SHA1", Digits: 5, Period: 30,
	}
	uri := Format(a)
	back, err := Parse(uri)
	if err != nil {
		t.Fatalf("reparse %q: %v", uri, err)
	}
	if back.Type != account.Steam || back.Digits != 5 ||
		string(back.Secret) != string(a.Secret) || back.Issuer != a.Issuer {
		t.Errorf("round trip mismatch: %s -> %+v", uri, back)
	}
}

func TestSteamRejectsWrongDigits(t *testing.T) {
	for _, bad := range []string{
		"otpauth://steam/Steam:x?secret=JBSWY3DPEHPK3PXP&digits=6",
		"otpauth://totp/Steam:x?secret=JBSWY3DPEHPK3PXP&issuer=Steam&digits=8",
		"otpauth://totp/x?secret=JBSWY3DPEHPK3PXP&digits=5",
	} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("expected error for %q", bad)
		}
	}
}

func TestParseRejectsNonOTPAuth(t *testing.T) {
	for _, bad := range []string{"https://example.com", "otpauth://bogus/x?secret=AA", "otpauth://totp/x"} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("expected error for %q", bad)
		}
	}
}
