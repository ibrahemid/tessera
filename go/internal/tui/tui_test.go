package tui

import (
	"strings"
	"testing"

	"github.com/ibrahemid/tessera/go/internal/account"
)

func sample() []account.Account {
	return []account.Account{
		{ID: "1", Type: account.TOTP, Issuer: "Zeta", Account: "z", Secret: []byte("12345678901234567890"), Algorithm: "SHA1", Digits: 6, Period: 30},
		{ID: "2", Type: account.TOTP, Issuer: "Alpha", Account: "a", Secret: []byte("12345678901234567890"), Algorithm: "SHA1", Digits: 6, Period: 30, Pinned: true},
		{ID: "3", Type: account.HOTP, Issuer: "Bank", Account: "b", Secret: []byte("12345678901234567890"), Algorithm: "SHA1", Digits: 6},
	}
}

func TestFilteredPinnedFirstThenAlpha(t *testing.T) {
	m := New(sample(), nil)
	rows := m.filtered()
	if rows[0].Issuer != "Alpha" { // pinned wins
		t.Errorf("expected pinned Alpha first, got %s", rows[0].Issuer)
	}
}

func TestFilteredByQuery(t *testing.T) {
	m := New(sample(), nil)
	m.query = "bank"
	rows := m.filtered()
	if len(rows) != 1 || rows[0].Issuer != "Bank" {
		t.Errorf("query filter failed: %+v", rows)
	}
}

func TestViewRendersAccounts(t *testing.T) {
	m := New(sample(), nil)
	v := m.View()
	for _, want := range []string{"Tessera", "Alpha", "Zeta", "Bank", "search"} {
		if !strings.Contains(v, want) {
			t.Errorf("View missing %q", want)
		}
	}
}

func TestAdvancePersistsAndIncrements(t *testing.T) {
	var saved []account.Account
	m := New(sample(), func(a []account.Account) error { saved = a; return nil })
	m.advance("3")
	var bank account.Account
	for _, a := range m.accounts {
		if a.ID == "3" {
			bank = a
		}
	}
	if bank.Counter != 1 {
		t.Errorf("counter = %d, want 1", bank.Counter)
	}
	if len(saved) != 3 {
		t.Error("save callback not invoked")
	}
}
