package account

import (
	"testing"

	"github.com/ibrahemid/tessera/go/internal/spectest"
)

// TestHandleVectors asserts the Go core reproduces the pinned handle vectors in
// /spec/testvectors.json (assignment and canonical serialization). The Swift
// core must reproduce these same bytes for interop.
func TestHandleVectors(t *testing.T) {
	var v struct {
		Handles struct {
			Canonical struct {
				ExpectedCanonicalJSON string `json:"expected_canonical_json"`
			} `json:"canonical"`
			Assignment []struct {
				Name     string `json:"name"`
				Accounts []struct {
					ID        string `json:"id"`
					Issuer    string `json:"issuer"`
					Account   string `json:"account"`
					CreatedAt int64  `json:"created_at"`
					Handle    string `json:"handle"`
				} `json:"accounts"`
				Expected map[string]string `json:"expected"`
			} `json:"assignment"`
		} `json:"handles"`
	}
	spectest.Load(t, &v)

	if len(v.Handles.Assignment) == 0 {
		t.Fatal("no handle assignment vectors loaded")
	}
	for _, c := range v.Handles.Assignment {
		accts := make([]Account, len(c.Accounts))
		for i, a := range c.Accounts {
			accts[i] = Account{ID: a.ID, Issuer: a.Issuer, Account: a.Account, CreatedAt: a.CreatedAt, Handle: a.Handle}
		}
		AssignHandles(accts)
		for _, a := range accts {
			if a.Handle != c.Expected[a.ID] {
				t.Errorf("case %q: account %s handle %q, want %q", c.Name, a.ID, a.Handle, c.Expected[a.ID])
			}
		}
	}

	a := Account{
		ID: "00000000-0000-4000-8000-000000000010", Type: TOTP,
		Issuer: "ACME", Account: "alice@example.com", Handle: "ac",
		Secret: []byte("12345678901234567890"), Algorithm: "SHA1", Digits: 6, Period: 30,
		CreatedAt: 1700000000, UpdatedAt: 1700000000,
	}
	got, err := CanonicalJSON([]Account{a})
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != v.Handles.Canonical.ExpectedCanonicalJSON {
		t.Errorf("canonical mismatch:\n got %s\nwant %s", got, v.Handles.Canonical.ExpectedCanonicalJSON)
	}
}
