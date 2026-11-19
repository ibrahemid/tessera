package otpauth

import (
	"testing"

	"github.com/ibrahemid/tessera/go/internal/base32x"
	"github.com/ibrahemid/tessera/go/internal/spectest"
)

type v2Vectors struct {
	OTPAuthStrict struct {
		Valid []struct {
			Name      string `json:"name"`
			URI       string `json:"uri"`
			Type      string `json:"type"`
			Issuer    string `json:"issuer"`
			Account   string `json:"account"`
			SecretB32 string `json:"secret_b32"`
			Algorithm string `json:"algorithm"`
			Digits    int    `json:"digits"`
			Period    int    `json:"period"`
			Counter   int64  `json:"counter"`
		} `json:"valid"`
		Invalid []struct {
			Name string `json:"name"`
			URI  string `json:"uri"`
		} `json:"invalid"`
	} `json:"otpauth_strict"`
}

func TestV2VectorsOTPAuthStrict(t *testing.T) {
	var v v2Vectors
	spectest.Load(t, &v)
	if len(v.OTPAuthStrict.Valid) == 0 || len(v.OTPAuthStrict.Invalid) == 0 {
		t.Fatal("otpauth_strict is empty")
	}
	for _, c := range v.OTPAuthStrict.Valid {
		t.Run(c.Name, func(t *testing.T) {
			a, err := Parse(c.URI)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if string(a.Type) != c.Type || a.Issuer != c.Issuer || a.Account != c.Account {
				t.Fatalf("got %s %q/%q, want %s %q/%q", a.Type, a.Issuer, a.Account, c.Type, c.Issuer, c.Account)
			}
			if a.Algorithm != c.Algorithm || a.Digits != c.Digits || a.Period != c.Period || a.Counter != c.Counter {
				t.Fatalf("got %s/%d/%d/%d, want %s/%d/%d/%d", a.Algorithm, a.Digits, a.Period, a.Counter, c.Algorithm, c.Digits, c.Period, c.Counter)
			}
			if b32 := base32x.EncodeNoPad(a.Secret); b32 != c.SecretB32 {
				t.Fatalf("secret mismatch")
			}
		})
	}
	for _, c := range v.OTPAuthStrict.Invalid {
		t.Run(c.Name, func(t *testing.T) {
			if _, err := Parse(c.URI); err == nil {
				t.Fatal("Parse accepted an invalid URI")
			}
		})
	}
}
