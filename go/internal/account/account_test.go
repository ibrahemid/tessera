package account

import (
	"strings"
	"testing"
)

func sample() Account {
	return Account{
		ID: "b", Type: TOTP, Issuer: "ACME", Account: "john@example.com",
		Secret: []byte("12345678901234567890"), Algorithm: "SHA1", Digits: 6, Period: 30,
		Tags: []string{"work"}, CreatedAt: 1700000000, UpdatedAt: 1700000000,
	}
}

func TestCanonicalKeysSortedAndDeterministic(t *testing.T) {
	a := sample()
	b, err := CanonicalJSON([]Account{a})
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	// First key must be "account" (lexicographically smallest).
	if !strings.HasPrefix(s, `[{"account":`) {
		t.Errorf("keys not sorted; got prefix %.40q", s)
	}
	if strings.Contains(s, "\n") {
		t.Error("canonical JSON must not contain newlines")
	}
	// Determinism: same input -> same bytes.
	b2, _ := CanonicalJSON([]Account{a})
	if string(b2) != s {
		t.Error("canonical JSON not deterministic")
	}
}

func TestCanonicalOrdersByID(t *testing.T) {
	a1 := sample()
	a1.ID = "z"
	a2 := sample()
	a2.ID = "a"
	b, _ := CanonicalJSON([]Account{a1, a2})
	out, err := ParseCanonicalJSON(b)
	if err != nil {
		t.Fatal(err)
	}
	if out[0].ID != "a" || out[1].ID != "z" {
		t.Errorf("not ordered by id: %s, %s", out[0].ID, out[1].ID)
	}
}

func TestCanonicalRoundTrip(t *testing.T) {
	a := sample()
	b, _ := CanonicalJSON([]Account{a})
	out, err := ParseCanonicalJSON(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].Issuer != "ACME" || string(out[0].Secret) != "12345678901234567890" {
		t.Errorf("round trip mismatch: %+v", out)
	}
}

func TestNoHTMLEscape(t *testing.T) {
	a := sample()
	a.Issuer = "A&B<C>"
	b, _ := CanonicalJSON([]Account{a})
	s := string(b)
	if !strings.Contains(s, `"A&B<C>"`) {
		t.Errorf("expected raw unescaped issuer: %s", b)
	}
	for _, esc := range []string{"\\u0026", "\\u003c", "\\u003e"} {
		if strings.Contains(s, esc) {
			t.Errorf("HTML escaping not disabled (found %s): %s", esc, b)
		}
	}
}

func TestForwardCompatIgnoresUnknownField(t *testing.T) {
	// A payload written by a newer version carries a field this reader does not
	// know. It MUST decode (AEAD-authenticated payload; tolerance is safe) and,
	// on re-encode, MUST NOT preserve the unknown field (canonical = spec fields).
	payload := []byte(`[{"account":"a@b.co","algorithm":"SHA1","counter":0,` +
		`"created_at":1,"digits":6,"folder":"","future_field":{"x":[1,2]},` +
		`"id":"id1","issuer":"ACME","period":30,"pinned":false,` +
		`"secret":"MTIzNDU2Nzg5MDEyMzQ1Njc4OTA=","tags":[],"type":"totp","updated_at":1}]`)
	got, err := ParseCanonicalJSON(payload)
	if err != nil {
		t.Fatalf("unknown field must not fail decode: %v", err)
	}
	if len(got) != 1 || got[0].ID != "id1" || got[0].Issuer != "ACME" {
		t.Fatalf("known fields not decoded: %+v", got)
	}
	reencoded, err := CanonicalJSON(got)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(reencoded), "future_field") {
		t.Errorf("re-encode must drop unknown field:\n%s", reencoded)
	}
}

func TestRejectDuplicateKeys(t *testing.T) {
	dup := []byte(`[{"id":"a","id":"b","type":"totp"}]`)
	if _, err := ParseCanonicalJSON(dup); err == nil {
		t.Error("expected duplicate-key rejection")
	}
}

func TestValidate(t *testing.T) {
	a := sample()
	if err := a.Validate(); err != nil {
		t.Fatalf("valid account rejected: %v", err)
	}
	bad := sample()
	bad.Secret = nil
	if err := bad.Validate(); err == nil {
		t.Error("empty secret should fail validation")
	}
}
