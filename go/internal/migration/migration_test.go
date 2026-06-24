package migration

import (
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
