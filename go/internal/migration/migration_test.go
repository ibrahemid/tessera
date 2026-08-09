package migration

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/ibrahemid/tessera/go/internal/base32x"
	"github.com/ibrahemid/tessera/go/internal/spectest"
)

type migrationVectors struct {
	Migration struct {
		Cases []struct {
			URI      string `json:"uri"`
			Accounts []struct {
				Type      string `json:"type"`
				Issuer    string `json:"issuer"`
				Account   string `json:"account"`
				SecretB32 string `json:"secret_b32"`
				Algorithm string `json:"algorithm"`
				Digits    int    `json:"digits"`
			} `json:"accounts"`
		} `json:"cases"`
	} `json:"migration"`
}

func TestParseSpecVectors(t *testing.T) {
	var v migrationVectors
	spectest.Load(t, &v)
	if len(v.Migration.Cases) == 0 {
		t.Fatal("no migration cases in test vectors")
	}

	for ci, tc := range v.Migration.Cases {
		got, err := Parse(tc.URI)
		if err != nil {
			t.Fatalf("case %d: Parse error: %v", ci, err)
		}
		if len(got) != len(tc.Accounts) {
			t.Fatalf("case %d: got %d accounts, want %d", ci, len(got), len(tc.Accounts))
		}
		for i, want := range tc.Accounts {
			a := got[i]
			if string(a.Type) != want.Type {
				t.Errorf("case %d acct %d: type=%q want %q", ci, i, a.Type, want.Type)
			}
			if a.Issuer != want.Issuer {
				t.Errorf("case %d acct %d: issuer=%q want %q", ci, i, a.Issuer, want.Issuer)
			}
			if a.Account != want.Account {
				t.Errorf("case %d acct %d: account=%q want %q", ci, i, a.Account, want.Account)
			}
			if a.Algorithm != want.Algorithm {
				t.Errorf("case %d acct %d: algorithm=%q want %q", ci, i, a.Algorithm, want.Algorithm)
			}
			if a.Digits != want.Digits {
				t.Errorf("case %d acct %d: digits=%d want %d", ci, i, a.Digits, want.Digits)
			}
			if b32 := base32x.EncodeNoPad(a.Secret); b32 != want.SecretB32 {
				t.Errorf("case %d acct %d: secret b32=%q want %q", ci, i, b32, want.SecretB32)
			}
			if a.ID == "" {
				t.Errorf("case %d acct %d: empty ID", ci, i)
			}
			if err := a.Validate(); err != nil {
				t.Errorf("case %d acct %d: Validate: %v", ci, i, err)
			}
		}
	}
}

func TestParseDefaults(t *testing.T) {
	const uri = "otpauth-migration://offline?data=CjEKCkhlbGxvId6tvu8SGEV4YW1wbGU6YWxpY2VAZ29vZ2xlLmNvbRoHRXhhbXBsZSABKAEwAhABGAEgACjr4JKkBg%3D%3D"
	got, err := Parse(uri)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	a := got[0]
	if a.Period != 30 {
		t.Errorf("period=%d want 30", a.Period)
	}
	if a.CreatedAt != 0 || a.UpdatedAt != 0 {
		t.Errorf("timestamps want 0, got created=%d updated=%d", a.CreatedAt, a.UpdatedAt)
	}
}

func TestParseRejectsNonMigration(t *testing.T) {
	cases := []string{
		"otpauth://totp/Example:alice@google.com?secret=JBSWY3DPEHPK3PXP&issuer=Example",
		"https://offline?data=CjEK",
		"otpauth-migration://wronghost?data=CjEK",
		"otpauth-migration://offline",
	}
	for _, uri := range cases {
		if _, err := Parse(uri); err == nil {
			t.Errorf("expected error for %q, got nil", uri)
		}
	}
}

// TestParsePreservesPlusInData pins that the base64 data parameter is not
// form-decoded. url.Query turns a literal '+' into a space, which corrupts the
// payload; Swift's URLComponents preserves it, so form-decoding here would make
// the two cores read different bytes from the same QR code.
func TestParsePreservesPlusInData(t *testing.T) {
	// A payload whose standard-base64 encoding contains a literal '+'.
	const b64 = "CicKCvsA+AMEBQYHCAkSDVBsdXM6dXNlckBhLmIaBFBsdXMgASgBMAIQARgBIAA="
	if !strings.Contains(b64, "+") {
		t.Fatalf("fixture must contain a literal '+': %s", b64)
	}
	got, err := Parse("otpauth-migration://offline?data=" + b64)
	if err != nil {
		t.Fatalf("Parse with a literal '+' in data: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d accounts, want 1", len(got))
	}
	wantSecret := []byte{0xfb, 0x00, 0xf8, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09}
	if !bytes.Equal(got[0].Secret, wantSecret) {
		t.Errorf("secret = % x, want % x", got[0].Secret, wantSecret)
	}
	if got[0].Issuer != "Plus" || got[0].Account != "user@a.b" {
		t.Errorf("issuer/account = %q/%q, want Plus/user@a.b", got[0].Issuer, got[0].Account)
	}
	// The percent-encoded form of the same data must decode identically.
	esc, err := Parse("otpauth-migration://offline?data=" + strings.ReplaceAll(b64, "+", "%2B"))
	if err != nil {
		t.Fatalf("Parse with %%2B: %v", err)
	}
	if !bytes.Equal(esc[0].Secret, got[0].Secret) {
		t.Error("'+' and '%2B' must decode to the same secret")
	}
}

// TestParseRejectsOutOfRangeCounter pins that an unsigned wire counter above
// int64 range is rejected instead of wrapping to a negative counter that would
// then be sealed into the vault.
func TestParseRejectsOutOfRangeCounter(t *testing.T) {
	// OtpParameters{ secret: "0123456789", name: "x", counter: 2^64-1 }
	param := []byte{0x0a, 0x0a}
	param = append(param, []byte("0123456789")...)
	param = append(param, 0x12, 0x01, 'x')
	param = append(param, 0x38) // field 7, varint
	param = append(param, []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x01}...)
	payload := append([]byte{0x0a, byte(len(param))}, param...)
	uri := "otpauth-migration://offline?data=" + base64.StdEncoding.EncodeToString(payload)
	if _, err := Parse(uri); err == nil {
		t.Fatal("expected an out-of-range counter to be rejected")
	}

	// The same payload with a counter inside int64 range parses, and the value
	// survives Validate.
	ok := append([]byte(nil), param[:len(param)-11]...)
	ok = append(ok, 0x38, 0x05)
	okPayload := append([]byte{0x0a, byte(len(ok))}, ok...)
	got, err := Parse("otpauth-migration://offline?data=" + base64.StdEncoding.EncodeToString(okPayload))
	if err != nil {
		t.Fatalf("in-range counter: %v", err)
	}
	if got[0].Counter != 5 {
		t.Errorf("counter = %d, want 5", got[0].Counter)
	}
}

func TestParseRejectsGarbageBase64(t *testing.T) {
	cases := []string{
		"otpauth-migration://offline?data=!!!notbase64!!!",
		"otpauth-migration://offline?data=" + "////////", // valid base64 but garbage protobuf
		"otpauth-migration://offline?data=",
	}
	for _, uri := range cases {
		if _, err := Parse(uri); err == nil {
			t.Errorf("expected error for %q, got nil", uri)
		}
	}
}
