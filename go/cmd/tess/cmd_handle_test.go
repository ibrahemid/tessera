package main

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/store"
	"github.com/ibrahemid/tessera/go/internal/vault"
	"github.com/spf13/cobra"
)

func totp(id, issuer, acct string, created int64) account.Account {
	return account.Account{
		ID: id, Type: account.TOTP, Issuer: issuer, Account: acct,
		Secret: []byte("12345678901234567890"), Algorithm: "SHA1", Digits: 6, Period: 30,
		CreatedAt: created, UpdatedAt: created,
	}
}

// sealVault writes an old-format vault (accounts without handles) at path.
func sealVault(t *testing.T, path string, accts []account.Account) {
	t.Helper()
	env, err := vault.Seal(accts, "testpass123")
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if err := store.Save(path, env); err != nil {
		t.Fatalf("save: %v", err)
	}
}

func TestUnlockAssignsHandlesOnceAndPersists(t *testing.T) {
	path := withVault(t)
	sealVault(t, path, []account.Account{
		totp("a", "ACME", "x", 1),
		totp("b", "ACME", "y", 2),
	})

	s, err := openSession()
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	got := map[string]string{}
	for _, a := range s.accounts {
		got[a.ID] = a.Handle
	}
	if got["a"] != "ac" || got["b"] != "ac2" {
		t.Fatalf("in-memory handles wrong: a=%q b=%q", got["a"], got["b"])
	}

	// The assignment was persisted: reloading and decrypting shows handles.
	env, err := store.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	reloaded, err := env.Open("testpass123")
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range reloaded {
		if a.Handle == "" {
			t.Fatalf("account %s not persisted with a handle", a.ID)
		}
	}

	// A second unlock must NOT rewrite the vault (handles already present).
	raw1, _ := os.ReadFile(path)
	if _, err := openSession(); err != nil {
		t.Fatal(err)
	}
	raw2, _ := os.ReadFile(path)
	if !bytes.Equal(raw1, raw2) {
		t.Error("second unlock rewrote the vault; handles must be assigned exactly once")
	}
}

func TestDuplicateIssuerGithub(t *testing.T) {
	// GitHub is a single word with internal caps: lowercased then first-two-chars
	// (no camelCase split), so two GitHubs yield gi/gi2.
	path := withVault(t)
	sealVault(t, path, []account.Account{totp("a", "GitHub", "dev", 1), totp("b", "GitHub", "ops", 2)})
	s, err := openSession()
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, a := range s.accounts {
		got[a.ID] = a.Handle
	}
	if got["a"] != "gi" || got["b"] != "gi2" {
		t.Fatalf("GitHub handles: a=%q b=%q, want gi/gi2", got["a"], got["b"])
	}
}

func sess(accts ...account.Account) *session { return &session{accounts: accts} }

func TestResolvePrecedence(t *testing.T) {
	// Handle "aw" on one account; another account's issuer "AWS" contains "aw"
	// as a substring. Exact-handle wins over substring.
	s := sess(
		account.Account{ID: "1", Handle: "aw", Issuer: "Amazon", Account: "root"},
		account.Account{ID: "2", Handle: "aw2", Issuer: "AWS", Account: "dev"},
	)
	idxs, err := s.resolveMatches("aw")
	if err != nil || len(idxs) != 1 || s.accounts[idxs[0]].ID != "1" {
		t.Fatalf("exact handle should win: idxs=%v err=%v", idxs, err)
	}

	// Exact issuer (case-insensitive) beats a broader substring match: "git"
	// equals account 1's issuer, so it resolves even though "GitHub" also
	// contains "git" as a substring.
	s2 := sess(
		account.Account{ID: "1", Handle: "g1", Issuer: "Git", Account: "a"},
		account.Account{ID: "2", Handle: "g2", Issuer: "GitHub", Account: "b"},
	)
	idxs, err = s2.resolveMatches("git")
	if err != nil || len(idxs) != 1 || s2.accounts[idxs[0]].ID != "1" {
		t.Fatalf("exact issuer should win over substring: idxs=%v err=%v", idxs, err)
	}

	// Unique substring resolves when no exact match exists.
	idxs, err = s2.resolveMatches("hub")
	if err != nil || len(idxs) != 1 || s2.accounts[idxs[0]].ID != "2" {
		t.Fatalf("unique substring should resolve: idxs=%v err=%v", idxs, err)
	}

	// Ambiguous substring (no exact handle/issuer/account) returns all matches.
	sAmb := sess(
		account.Account{ID: "1", Handle: "a1", Issuer: "GitHub", Account: "a"},
		account.Account{ID: "2", Handle: "a2", Issuer: "GitLab", Account: "b"},
	)
	idxs, err = sAmb.resolveMatches("git")
	if err != nil || len(idxs) != 2 {
		t.Fatalf("ambiguous substring should return 2: idxs=%v err=%v", idxs, err)
	}

	// No match is an error.
	if _, err := s2.resolveMatches("nope"); err == nil {
		t.Error("expected no-match error")
	}
}

func TestMatchTableOutput(t *testing.T) {
	s := sess(totp("1", "ACME", "alice", 1), totp("2", "ACME", "bob", 2))
	account.AssignHandles(s.accounts)
	cmd := &cobra.Command{}
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	printMatchTable(cmd, s.accounts, []int{0, 1}, false)
	got := buf.String()
	for _, want := range []string{"handle", "issuer", "account", "ac", "ac2", "alice", "bob"} {
		if !strings.Contains(got, want) {
			t.Fatalf("table missing %q:\n%s", want, got)
		}
	}
}

func TestSetHandleValidation(t *testing.T) {
	s := sess(
		account.Account{ID: "1", Handle: "ac", Issuer: "ACME"},
		account.Account{ID: "2", Handle: "gh", Issuer: "Git"},
	)
	if err := setHandle(s, 0, "1bad"); err == nil {
		t.Error("digit-leading handle must be rejected")
	}
	if err := setHandle(s, 0, "TooManyChars!"); err == nil {
		t.Error("invalid charset must be rejected")
	}
	if err := setHandle(s, 0, "gh"); err == nil {
		t.Error("duplicate handle must be rejected")
	}
	if err := setHandle(s, 0, "work"); err != nil {
		t.Fatalf("valid handle rejected: %v", err)
	}
	if s.accounts[0].Handle != "work" {
		t.Errorf("handle not set: %q", s.accounts[0].Handle)
	}
	// The freed handle "ac" is now assignable to another missing account.
	s.accounts = append(s.accounts, account.Account{ID: "3", Issuer: "ACME", CreatedAt: 3})
	account.AssignHandles(s.accounts)
	if s.accounts[2].Handle != "ac" {
		t.Errorf("freed handle should be reused: got %q", s.accounts[2].Handle)
	}
}

func TestAliasCommand(t *testing.T) {
	path := withVault(t)
	sealVault(t, path, []account.Account{totp("a", "Google", "me", 1)})
	// Migration assigns "go".
	c := newAliasCmd()
	c.SetOut(io.Discard)
	c.SetArgs([]string{"go", "gwork"})
	if err := c.Execute(); err != nil {
		t.Fatalf("alias: %v", err)
	}
	env, _ := store.Load(path)
	accts, _ := env.Open("testpass123")
	if accts[0].Handle != "gwork" {
		t.Fatalf("alias did not persist: %q", accts[0].Handle)
	}
}

func TestCodeMultiMatch(t *testing.T) {
	path := withVault(t)
	sealVault(t, path, []account.Account{totp("a", "ACME", "one", 1), totp("b", "ACME", "two", 2)})

	// Without --copy: prints a table with codes and exits 0.
	c := newCodeCmd()
	var buf bytes.Buffer
	c.SetOut(&buf)
	c.SetArgs([]string{"acme"})
	if err := c.Execute(); err != nil {
		t.Fatalf("code without -c on multi-match should not error: %v", err)
	}
	if !strings.Contains(buf.String(), "code") {
		t.Fatalf("expected a code column:\n%s", buf.String())
	}

	// With --copy: no single target, prints the table and exits non-zero.
	c2 := newCodeCmd()
	c2.SetOut(io.Discard)
	c2.SetArgs([]string{"acme", "-c"})
	if err := c2.Execute(); err == nil {
		t.Fatal("code -c on multi-match must error")
	}
}
