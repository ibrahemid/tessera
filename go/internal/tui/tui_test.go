package tui

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/clipboard"
)

func sample() []account.Account {
	return []account.Account{
		{ID: "1", Type: account.TOTP, Issuer: "Zeta", Account: "z", Secret: []byte("12345678901234567890"), Algorithm: "SHA1", Digits: 6, Period: 30},
		{ID: "2", Type: account.TOTP, Issuer: "Alpha", Account: "a", Secret: []byte("12345678901234567890"), Algorithm: "SHA1", Digits: 6, Period: 30, Pinned: true},
		{ID: "3", Type: account.HOTP, Issuer: "Bank", Account: "b", Secret: []byte("12345678901234567890"), Algorithm: "SHA1", Digits: 6},
	}
}

func TestFilteredPinnedFirstThenAlpha(t *testing.T) {
	m := New(sample(), nil, nil)
	rows := m.filtered()
	if rows[0].Issuer != "Alpha" { // pinned wins
		t.Errorf("expected pinned Alpha first, got %s", rows[0].Issuer)
	}
}

func TestFilteredByQuery(t *testing.T) {
	m := New(sample(), nil, nil)
	m.query = "bank"
	rows := m.filtered()
	if len(rows) != 1 || rows[0].Issuer != "Bank" {
		t.Errorf("query filter failed: %+v", rows)
	}
}

func TestFilteredByHandle(t *testing.T) {
	accts := sample()
	accts[2].Handle = "bk" // Bank
	m := New(accts, nil, nil)
	m.query = "bk"
	rows := m.filtered()
	if len(rows) != 1 || rows[0].ID != "3" {
		t.Errorf("handle search failed: %+v", rows)
	}
}

func TestViewRendersAccounts(t *testing.T) {
	m := New(sample(), nil, nil)
	v := m.View()
	for _, want := range []string{"Tessera", "Alpha", "Zeta", "Bank", "search"} {
		if !strings.Contains(v, want) {
			t.Errorf("View missing %q", want)
		}
	}
}

func TestAdvancePersistsAndIncrements(t *testing.T) {
	var gotID string
	var gotCounter int64
	m := New(sample(), func(id string, counter int64) error {
		gotID, gotCounter = id, counter
		return nil
	}, nil)
	m.copy = func(string) error { return nil }
	m.advanceHOTP("3")
	var bank account.Account
	for _, a := range m.accounts {
		if a.ID == "3" {
			bank = a
		}
	}
	if bank.Counter != 1 {
		t.Errorf("counter = %d, want 1", bank.Counter)
	}
	// The persist callback names the account and the new value, so the caller
	// can re-read the vault and apply it there instead of re-sealing a stale list.
	if gotID != "3" || gotCounter != 1 {
		t.Errorf("advance callback got (%q, %d), want (\"3\", 1)", gotID, gotCounter)
	}
}

func TestAdvanceFailureIsReportedAndNotApplied(t *testing.T) {
	m := New(sample(), func(string, int64) error { return errors.New("vault is in use") }, nil)
	m.copy = func(string) error { return nil }
	m.advanceHOTP("3")
	for _, a := range m.accounts {
		if a.ID == "3" && a.Counter != 0 {
			t.Errorf("counter advanced despite a failed save: %d", a.Counter)
		}
	}
	if !m.statusFail || !strings.Contains(m.status, "vault is in use") {
		t.Errorf("expected a failure status, got %q (fail=%v)", m.status, m.statusFail)
	}
	if strings.Contains(m.View(), "✓") {
		t.Error("View must not show a success mark after a failed save")
	}
}

// TestCopyFailureIsSurfaced pins that a clipboard error is reported instead of
// the view claiming a copy that never happened.
func TestCopyFailureIsSurfaced(t *testing.T) {
	m := New(sample(), nil, nil)
	m.copy = func(string) error { return errors.New("no clipboard") }
	m2, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	got := m2.(Model)
	if !got.statusFail || !strings.Contains(got.status, "no clipboard") {
		t.Fatalf("status = %q (fail=%v), want a clipboard failure", got.status, got.statusFail)
	}
	if strings.Contains(got.View(), "✓") {
		t.Error("View must not show a success mark after a failed copy")
	}
}

func TestCopySuccessReportsCopied(t *testing.T) {
	var copied string
	m := New(sample(), nil, nil)
	m.copy = func(s string) error { copied = s; return nil }
	m2, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	got := m2.(Model)
	if got.statusFail || !strings.HasPrefix(got.status, "Copied ") {
		t.Fatalf("status = %q (fail=%v), want a Copied status", got.status, got.statusFail)
	}
	if len(copied) != 6 {
		t.Errorf("copied %q, want a 6-digit code", copied)
	}
}

// TestBackspaceDeletesOneRune pins that the search filter is edited by
// character, not by byte: slicing bytes cuts a multibyte rune in half.
func TestBackspaceDeletesOneRune(t *testing.T) {
	m := New(sample(), nil, nil)
	m.searching = true
	for _, s := range []string{"é", "漢", "🔐"} {
		m.query = "ab" + s
		next, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyBackspace})
		got := next.(Model)
		if got.query != "ab" {
			t.Errorf("backspace on %q left %q, want \"ab\"", "ab"+s, got.query)
		}
		if !utf8.ValidString(got.query) {
			t.Errorf("backspace produced invalid UTF-8: %q", got.query)
		}
	}
	m.query = ""
	next, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyBackspace})
	if q := next.(Model).query; q != "" {
		t.Errorf("backspace on an empty query gave %q", q)
	}
}

// fakeClipboard is an in-memory clipboard backend.
type fakeClipboard struct {
	text      string
	sensitive bool
}

func (f *fakeClipboard) Name() string { return "fake" }

func (f *fakeClipboard) Write(text string, sensitive bool) error {
	f.text, f.sensitive = text, sensitive
	return nil
}

func (f *fakeClipboard) Read() (string, error) { return f.text, nil }

func (f *fakeClipboard) Clear() error { f.text = ""; return nil }

// stubDetect replaces the backend picker for one test.
func stubDetect(t *testing.T, fn func() (clipboard.Clipboard, error)) {
	t.Helper()
	old := detectClipboard
	detectClipboard = fn
	t.Cleanup(func() { detectClipboard = old })
}

func pressCopy(t *testing.T, m Model) Model {
	t.Helper()
	m2, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	return m2.(Model)
}

func TestDefaultCopyWritesTheCodeAsSensitive(t *testing.T) {
	fake := &fakeClipboard{}
	stubDetect(t, func() (clipboard.Clipboard, error) { return fake, nil })

	got := pressCopy(t, New(sample(), nil, nil))
	if got.statusFail {
		t.Fatalf("status = %q, want a copy", got.status)
	}
	if len(fake.text) != 6 || !fake.sensitive {
		t.Errorf("clipboard = %q (sensitive %v), want a 6-digit code marked sensitive", fake.text, fake.sensitive)
	}
}

// TestDefaultCopyShowsWhyTheClipboardIsMissing pins that the status line
// carries the backend's own message, which names the tool to install.
func TestDefaultCopyShowsWhyTheClipboardIsMissing(t *testing.T) {
	missing := &clipboard.NoClipboardError{Session: "wayland", Missing: []string{"wl-copy"}}
	stubDetect(t, func() (clipboard.Clipboard, error) { return nil, missing })

	got := pressCopy(t, New(sample(), nil, nil))
	if !got.statusFail || !strings.Contains(got.status, missing.Error()) {
		t.Errorf("status = %q (fail=%v), want %q", got.status, got.statusFail, missing.Error())
	}
}

func TestCopySchedulesTheClear(t *testing.T) {
	var copied, cleared string
	m := New(sample(), nil, func(c string) error { cleared = c; return nil })
	m.copy = func(s string) error { copied = s; return nil }

	got := pressCopy(t, m)
	if got.statusFail || !strings.HasPrefix(got.status, "Copied ") {
		t.Fatalf("status = %q (fail=%v), want a Copied status", got.status, got.statusFail)
	}
	if cleared == "" || cleared != copied {
		t.Errorf("clear scheduled for %q, want the copied code %q", cleared, copied)
	}
}

// TestClearFailureKeepsTheCopy: the code is on the clipboard, so the status
// says so and adds why it will stay there.
func TestClearFailureKeepsTheCopy(t *testing.T) {
	var copied string
	m := New(sample(), nil, func(string) error { return errors.New("schedule clipboard clear: fork failed") })
	m.copy = func(s string) error { copied = s; return nil }

	got := pressCopy(t, m)
	if copied == "" {
		t.Fatal("the code must still be copied")
	}
	if !got.statusFail || !strings.HasPrefix(got.status, "Copied ") || !strings.Contains(got.status, "fork failed") {
		t.Errorf("status = %q (fail=%v), want the copy and the clear failure", got.status, got.statusFail)
	}
}

func TestFailedCopySchedulesNoClear(t *testing.T) {
	m := New(sample(), nil, func(string) error {
		t.Error("a failed copy must not schedule a clear")
		return nil
	})
	m.copy = func(string) error { return errors.New("no clipboard") }
	pressCopy(t, m)
}

func TestAdvanceSchedulesTheClear(t *testing.T) {
	var copied, cleared string
	m := New(sample(), func(string, int64) error { return nil }, func(c string) error { cleared = c; return nil })
	m.copy = func(s string) error { copied = s; return nil }

	m.advanceHOTP("3")
	if cleared == "" || cleared != copied {
		t.Errorf("clear scheduled for %q, want the copied code %q", cleared, copied)
	}
}
