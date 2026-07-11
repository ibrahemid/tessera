package account

import "testing"

func TestHandleBaseExamples(t *testing.T) {
	cases := []struct {
		issuer, account, want string
	}{
		{"ACME", "", "ac"},
		{"GitHub", "", "gi"}, // single word with internal caps: lowercase then first two chars, no camelCase split
		{"Google Cloud", "", "gc"},
		{"1Password", "", "x1p"},
		{"", "alice@example.com", "al"},
		{"", "", "acct"},
		{"X", "", "x"},                  // single-char word
		{"  Foo   Bar  Baz ", "", "fb"}, // multi-word, collapses and trims
		{"99designs", "", "x99"},        // digit-leading, first two chars
		{"7 Eleven", "", "x7e"},         // digit-leading two-word
		{"café", "", "ca"},              // non-[a-z0-9 ] dropped
		{"", "bob", "bo"},               // account with no @
	}
	for _, c := range cases {
		if got := handleBase(c.issuer, c.account); got != c.want {
			t.Errorf("handleBase(%q,%q) = %q, want %q", c.issuer, c.account, got, c.want)
		}
	}
}

func TestAssignHandlesCollisionChain(t *testing.T) {
	accts := []Account{
		{ID: "a", Issuer: "ACME", CreatedAt: 1},
		{ID: "b", Issuer: "ACME", CreatedAt: 2},
		{ID: "c", Issuer: "ACME", CreatedAt: 3},
	}
	if !AssignHandles(accts) {
		t.Fatal("expected assignment to report change")
	}
	want := map[string]string{"a": "ac", "b": "ac2", "c": "ac3"}
	for _, a := range accts {
		if a.Handle != want[a.ID] {
			t.Errorf("account %s: handle %q, want %q", a.ID, a.Handle, want[a.ID])
		}
	}
}

func TestAssignHandlesSkipsUserEdited(t *testing.T) {
	// A user already edited an account's handle to ac2; the auto-assigned third
	// account with the same base must skip to ac3 and no handle is renumbered.
	accts := []Account{
		{ID: "a", Issuer: "ACME", CreatedAt: 1, Handle: "ac"},
		{ID: "b", Issuer: "ACME", CreatedAt: 2, Handle: "ac2"},
		{ID: "c", Issuer: "ACME", CreatedAt: 3},
	}
	AssignHandles(accts)
	if accts[0].Handle != "ac" || accts[1].Handle != "ac2" || accts[2].Handle != "ac3" {
		t.Errorf("got %q/%q/%q, want ac/ac2/ac3", accts[0].Handle, accts[1].Handle, accts[2].Handle)
	}
}

func TestAssignHandlesOrderByCreatedThenID(t *testing.T) {
	// Same base, assignment order is created_at asc then id asc; storage order
	// is deliberately reversed to prove it does not affect the result.
	accts := []Account{
		{ID: "z", Issuer: "ACME", CreatedAt: 10},
		{ID: "a", Issuer: "ACME", CreatedAt: 10},
		{ID: "m", Issuer: "ACME", CreatedAt: 5},
	}
	AssignHandles(accts)
	byID := map[string]string{}
	for _, a := range accts {
		byID[a.ID] = a.Handle
	}
	// created_at 5 first (m -> ac), then created_at 10 by id: a -> ac2, z -> ac3.
	if byID["m"] != "ac" || byID["a"] != "ac2" || byID["z"] != "ac3" {
		t.Errorf("order wrong: m=%q a=%q z=%q", byID["m"], byID["a"], byID["z"])
	}
}

func TestAssignHandlesIdempotent(t *testing.T) {
	accts := []Account{{ID: "a", Issuer: "ACME", CreatedAt: 1}}
	if !AssignHandles(accts) {
		t.Fatal("first pass should assign")
	}
	if AssignHandles(accts) {
		t.Error("second pass must not change already-assigned handles")
	}
}

func TestValidHandle(t *testing.T) {
	valid := []string{"a", "gh", "gh2", "x1p", "abcdefghijkl", "a1b2c3"}
	invalid := []string{"", "1p", "Gh", "gh_", "abcdefghijklm", "gh 2", "é"}
	for _, h := range valid {
		if !ValidHandle(h) {
			t.Errorf("expected %q valid", h)
		}
	}
	for _, h := range invalid {
		if ValidHandle(h) {
			t.Errorf("expected %q invalid", h)
		}
	}
}

func TestCheckHandleUniqueness(t *testing.T) {
	dup := []Account{{ID: "a", Handle: "gh"}, {ID: "b", Handle: "gh"}}
	if err := CheckHandleUniqueness(dup); err == nil {
		t.Error("expected duplicate-handle error")
	}
	ok := []Account{{ID: "a", Handle: "gh"}, {ID: "b", Handle: "gh2"}, {ID: "c"}}
	if err := CheckHandleUniqueness(ok); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCanonicalIncludesHandleWhenSet(t *testing.T) {
	a := Account{
		ID: "id1", Type: TOTP, Issuer: "ACME", Account: "a@b.co", Handle: "ac",
		Secret: []byte("12345678901234567890"), Algorithm: "SHA1", Digits: 6, Period: 30,
		CreatedAt: 1, UpdatedAt: 1,
	}
	b, err := CanonicalJSON([]Account{a})
	if err != nil {
		t.Fatal(err)
	}
	// Handle sorts between folder and id.
	if want := `"folder":"","handle":"ac","id":"id1"`; !contains(string(b), want) {
		t.Errorf("handle not in sorted position:\n%s", b)
	}
	out, err := ParseCanonicalJSON(b)
	if err != nil {
		t.Fatal(err)
	}
	if out[0].Handle != "ac" {
		t.Errorf("round trip lost handle: %q", out[0].Handle)
	}
}

func TestCanonicalOmitsHandleWhenEmpty(t *testing.T) {
	a := Account{
		ID: "id1", Type: TOTP, Issuer: "ACME", Account: "a@b.co",
		Secret: []byte("12345678901234567890"), Algorithm: "SHA1", Digits: 6, Period: 30,
		CreatedAt: 1, UpdatedAt: 1,
	}
	b, _ := CanonicalJSON([]Account{a})
	if contains(string(b), `"handle"`) {
		t.Errorf("empty handle must be omitted:\n%s", b)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
