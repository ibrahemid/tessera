package main

import (
	"bytes"
	"fmt"
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
	defer s.close()
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
	s.close()
	raw1, _ := os.ReadFile(path)
	s2, err := openSession()
	if err != nil {
		t.Fatal(err)
	}
	s2.close()
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
	defer s.close()
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

// TestResolveExactID pins the id stage on every resolver entry point the
// commands call: a full, case-exact id picks its account before a handle equal
// to the same text, within the scope the entry point resolves in.
func TestResolveExactID(t *testing.T) {
	const (
		liveID    = "49abdd291b36981883aafdd2e47ced39"
		trashedID = "d8db694d88f80749163b421a5489f884"
	)
	accounts := func() *session {
		return sess(
			account.Account{ID: liveID, Handle: "gi", Issuer: "GitHub", Account: "dev"},
			account.Account{ID: "b", Handle: liveID, Issuer: "Other", Account: "ops"},
			account.Account{ID: trashedID, Handle: "st", Issuer: "Steam", Account: "me", DeletedAt: 5},
			account.Account{ID: "d", Handle: trashedID, Issuer: "Old", Account: "x", DeletedAt: 6},
		)
	}
	type resolver func(s *session, cmd *cobra.Command, q string) (int, error)
	one := func(idxs []int, err error) (int, error) {
		if err != nil {
			return -1, err
		}
		if len(idxs) != 1 {
			return -1, fmt.Errorf("%d matches", len(idxs))
		}
		return idxs[0], nil
	}
	live := map[string]resolver{
		"resolveMatches": func(s *session, _ *cobra.Command, q string) (int, error) { return one(s.resolveMatches(q)) },
		"resolveIn live": func(s *session, _ *cobra.Command, q string) (int, error) { return one(s.resolveIn(q, false)) },
		"single":         func(s *session, c *cobra.Command, q string) (int, error) { return s.single(c, q) },
		"singleIn live":  func(s *session, c *cobra.Command, q string) (int, error) { return s.singleIn(c, q, false) },
		"singleScoped live": func(s *session, c *cobra.Command, q string) (int, error) {
			return s.singleScoped(c, q, liveScope)
		},
		"singleScoped liveThenTrash": func(s *session, c *cobra.Command, q string) (int, error) {
			return s.singleScoped(c, q, liveThenTrashScope)
		},
		"resolveEach live": func(s *session, c *cobra.Command, q string) (int, error) {
			return one(s.resolveEach(c, []string{q}, liveScope))
		},
		"resolveEach liveThenTrash": func(s *session, c *cobra.Command, q string) (int, error) {
			return one(s.resolveEach(c, []string{q}, liveThenTrashScope))
		},
	}
	trash := map[string]resolver{
		"resolveIn trash": func(s *session, _ *cobra.Command, q string) (int, error) { return one(s.resolveIn(q, true)) },
		"singleIn trash":  func(s *session, c *cobra.Command, q string) (int, error) { return s.singleIn(c, q, true) },
		"singleScoped trash": func(s *session, c *cobra.Command, q string) (int, error) {
			return s.singleScoped(c, q, trashScope)
		},
		"resolveEach trash": func(s *session, c *cobra.Command, q string) (int, error) {
			return one(s.resolveEach(c, []string{q}, trashScope))
		},
	}
	cases := []struct {
		name      string
		resolvers map[string]resolver
		query     string
		wantID    string
		wantErr   string
	}{
		{"live id beats an equal handle", live, liveID, liveID, ""},
		{"live id is trimmed", live, "  " + liveID + "\t", liveID, ""},
		{"partial id is not an id", live, liveID[:12], "b", ""},
		{"id compares case-sensitively", live, strings.ToUpper(liveID), "b", ""},
		{"trashed id beats an equal trashed handle", trash, trashedID, trashedID, ""},
		{"trashed partial id is not an id", trash, trashedID[:12], "d", ""},
		{"live scope ignores a trashed id", map[string]resolver{
			"resolveMatches": live["resolveMatches"],
			"single":         live["single"],
			"resolveEach":    live["resolveEach live"],
		}, trashedID, "", "in the Trash"},
		{"trash scope ignores a live id", trash, liveID, "", "nothing in the Trash matches"},
		{"liveThenTrash falls back to a trashed id", map[string]resolver{
			"singleScoped": live["singleScoped liveThenTrash"],
			"resolveEach":  live["resolveEach liveThenTrash"],
		}, trashedID, trashedID, ""},
	}
	for _, tc := range cases {
		for name, resolve := range tc.resolvers {
			t.Run(tc.name+"/"+name, func(t *testing.T) {
				stubTTY(t, false)
				s := accounts()
				cmd := &cobra.Command{}
				cmd.SetOut(io.Discard)
				cmd.SetErr(io.Discard)
				idx, err := resolve(s, cmd, tc.query)
				if tc.wantErr != "" {
					if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
						t.Fatalf("err = %v, want one containing %q", err, tc.wantErr)
					}
					return
				}
				if err != nil {
					t.Fatalf("resolve %q: %v", tc.query, err)
				}
				if got := s.accounts[idx].ID; got != tc.wantID {
					t.Errorf("resolve %q picked %q, want %q", tc.query, got, tc.wantID)
				}
			})
		}
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
	for _, want := range []string{"alias", "issuer", "account", "ac", "ac2", "alice", "bob"} {
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

	// On a terminal every match is listed with its code, and the command exits
	// 0: the table answers the question, so there is nothing to disambiguate for.
	stubTTY(t, true)
	c := newCodeCmd()
	var buf bytes.Buffer
	c.SetOut(&buf)
	c.SetArgs([]string{"acme"})
	if err := c.Execute(); err != nil {
		t.Fatalf("code on multi-match should not error: %v", err)
	}
	if !strings.Contains(buf.String(), "code") {
		t.Fatalf("expected a code column:\n%s", buf.String())
	}
}
