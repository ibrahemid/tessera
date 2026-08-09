package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/ibrahemid/tessera/go/internal/account"
)

// stubClipboard replaces the clipboard writer for one test.
func stubClipboard(t *testing.T, fn func(string) error) {
	t.Helper()
	old := copyToClipboard
	copyToClipboard = fn
	t.Cleanup(func() { copyToClipboard = old })
}

func runCode(t *testing.T, args ...string) (string, error) {
	t.Helper()
	c := newCodeCmd()
	var buf bytes.Buffer
	c.SetOut(&buf)
	c.SetArgs(args)
	err := c.Execute()
	return buf.String(), err
}

// TestCodeCopyFailureExitsNonZero pins that a clipboard error reaches the user.
// A silently discarded failure means the user pastes a stale code.
func TestCodeCopyFailureExitsNonZero(t *testing.T) {
	path := withVault(t)
	sealVault(t, path, []account.Account{totp("a", "ACME", "x", 1)})
	stubClipboard(t, func(string) error { return errors.New("no clipboard on this display") })

	_, err := runCode(t, "acme", "--copy")
	if err == nil {
		t.Fatal("a failed clipboard write must not exit zero")
	}
	if !strings.Contains(err.Error(), "clipboard") {
		t.Errorf("error should name the clipboard, got %v", err)
	}
}

func TestCodeCopySuccess(t *testing.T) {
	path := withVault(t)
	sealVault(t, path, []account.Account{totp("a", "ACME", "x", 1)})
	var copied string
	stubClipboard(t, func(s string) error { copied = s; return nil })

	out, err := runCode(t, "acme", "--copy")
	if err != nil {
		t.Fatalf("code --copy: %v", err)
	}
	if len(copied) != 6 {
		t.Errorf("copied %q, want a 6-digit code", copied)
	}
	if !strings.Contains(out, copied) {
		t.Errorf("printed code and copied code differ: %q vs %q", out, copied)
	}
}

// TestCodeNextPersistsCounterEvenIfCopyFails pins the ordering: the advanced
// HOTP counter is written before the copy is attempted, so a clipboard failure
// never leaves the user's counter behind the code they were shown.
func TestCodeNextPersistsCounterEvenIfCopyFails(t *testing.T) {
	path := withVault(t)
	sealVault(t, path, []account.Account{hotpAcct("h", "Bank", "ops")})
	stubClipboard(t, func(string) error { return errors.New("no clipboard") })

	if _, err := runCode(t, "bank", "--next", "--copy"); err == nil {
		t.Fatal("expected the clipboard failure to surface")
	}
	s, err := openSession()
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	i := indexByID(s.accounts, "h")
	if i < 0 || s.accounts[i].Counter != 1 {
		t.Fatalf("counter not persisted: %+v", s.accounts)
	}
}
