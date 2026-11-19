package migration

import (
	"testing"

	"github.com/ibrahemid/tessera/go/internal/base32x"
	"github.com/ibrahemid/tessera/go/internal/spectest"
)

type v2Vectors struct {
	MigrationStrict struct {
		Valid []struct {
			Name     string `json:"name"`
			URI      string `json:"uri"`
			Accounts []struct {
				Type      string `json:"type"`
				Issuer    string `json:"issuer"`
				Account   string `json:"account"`
				SecretB32 string `json:"secret_b32"`
				Algorithm string `json:"algorithm"`
				Digits    int    `json:"digits"`
				Counter   int64  `json:"counter"`
			} `json:"accounts"`
		} `json:"valid"`
		Invalid []struct {
			Name string `json:"name"`
			URI  string `json:"uri"`
		} `json:"invalid"`
	} `json:"migration_strict"`
}

func TestV2VectorsMigrationStrict(t *testing.T) {
	var v v2Vectors
	spectest.Load(t, &v)
	if len(v.MigrationStrict.Valid) == 0 || len(v.MigrationStrict.Invalid) == 0 {
		t.Fatal("migration_strict is empty")
	}
	for _, c := range v.MigrationStrict.Valid {
		t.Run(c.Name, func(t *testing.T) {
			got, err := Parse(c.URI)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if len(got) != len(c.Accounts) {
				t.Fatalf("got %d accounts, want %d", len(got), len(c.Accounts))
			}
			for i, want := range c.Accounts {
				a := got[i]
				if string(a.Type) != want.Type || a.Issuer != want.Issuer || a.Account != want.Account {
					t.Errorf("account %d: got %s %q/%q, want %s %q/%q", i, a.Type, a.Issuer, a.Account, want.Type, want.Issuer, want.Account)
				}
				if a.Algorithm != want.Algorithm || a.Digits != want.Digits || a.Counter != want.Counter {
					t.Errorf("account %d: got %s/%d/%d, want %s/%d/%d", i, a.Algorithm, a.Digits, a.Counter, want.Algorithm, want.Digits, want.Counter)
				}
				if b32 := base32x.EncodeNoPad(a.Secret); b32 != want.SecretB32 {
					t.Errorf("account %d: secret mismatch", i)
				}
				if err := a.Validate(); err != nil {
					t.Errorf("account %d: Validate: %v", i, err)
				}
			}
		})
	}
	for _, c := range v.MigrationStrict.Invalid {
		t.Run(c.Name, func(t *testing.T) {
			if _, err := Parse(c.URI); err == nil {
				t.Fatal("Parse accepted an invalid URI")
			}
		})
	}
}
