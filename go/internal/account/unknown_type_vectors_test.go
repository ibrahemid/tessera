package account

import (
	"testing"

	"github.com/ibrahemid/tessera/go/internal/spectest"
)

type unknownTypeVectors struct {
	UnknownType struct {
		Payload               string `json:"payload"`
		ExpectedCanonicalJSON string `json:"expected_canonical_json"`
		Accounts              []struct {
			ID            string `json:"id"`
			Type          string `json:"type"`
			ValidateError string `json:"validate_error"`
		} `json:"accounts"`
	} `json:"unknown_type"`
}

func TestUnknownTypeVectors(t *testing.T) {
	var v unknownTypeVectors
	spectest.Load(t, &v)
	u := v.UnknownType
	if len(u.Accounts) == 0 {
		t.Fatal("unknown_type is incomplete")
	}
	accts, err := ParseCanonicalJSON([]byte(u.Payload))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(accts) != len(u.Accounts) {
		t.Fatalf("parsed %d accounts, want %d", len(accts), len(u.Accounts))
	}
	out, err := CanonicalJSON(accts)
	if err != nil || string(out) != u.ExpectedCanonicalJSON {
		t.Fatalf("re-encode = %s, %v\nwant %s", out, err, u.ExpectedCanonicalJSON)
	}
	for i, c := range u.Accounts {
		a := accts[i]
		if a.ID != c.ID || string(a.Type) != c.Type {
			t.Fatalf("account %d = %q type %q, want %q type %q", i, a.ID, a.Type, c.ID, c.Type)
		}
		if err := a.Validate(); err == nil || err.Error() != c.ValidateError {
			t.Fatalf("validate %q = %v, want %q", c.ID, err, c.ValidateError)
		}
	}
}
