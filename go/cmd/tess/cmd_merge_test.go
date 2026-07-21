package main

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/store"
	"github.com/ibrahemid/tessera/go/internal/vault"
)

// sealAt writes a vault with the given passphrase at path and returns nothing.
func sealAt(t *testing.T, path, pass string, accts []account.Account) {
	t.Helper()
	env, err := vault.Seal(accts, pass)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if err := store.Save(path, env); err != nil {
		t.Fatalf("save: %v", err)
	}
}

func runMerge(t *testing.T, srcPath string) string {
	t.Helper()
	c := newMergeCmd()
	var buf bytes.Buffer
	c.SetOut(&buf)
	c.SetArgs([]string{srcPath})
	if err := c.Execute(); err != nil {
		t.Fatalf("merge: %v", err)
	}
	return buf.String()
}

func TestMergeUnionsNewAccounts(t *testing.T) {
	target := withVault(t) // passphrase testpass123
	sealAt(t, target, "testpass123", []account.Account{
		totp("a", "ACME", "x", 1),
	})
	src := filepath.Join(t.TempDir(), "src.json")
	sealAt(t, src, "testpass123", []account.Account{
		totp("a", "ACME", "x", 1), // duplicate by id -> unchanged
		totp("b", "Beta", "y", 2), // new -> added
	})

	runMerge(t, src)

	s, err := openSession()
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if len(s.accounts) != 2 {
		t.Fatalf("want 2 accounts after merge, got %d", len(s.accounts))
	}
	ids := map[string]bool{}
	for _, a := range s.accounts {
		ids[a.ID] = true
	}
	if !ids["a"] || !ids["b"] {
		t.Fatalf("missing merged ids: %v", ids)
	}
}

func TestMergeNewestWinsOnIDCollision(t *testing.T) {
	target := withVault(t)
	old := totp("a", "ACME", "x", 1)
	old.Issuer = "OldName"
	old.UpdatedAt = 100
	sealAt(t, target, "testpass123", []account.Account{old})

	newer := totp("a", "ACME", "x", 1)
	newer.Issuer = "NewName"
	newer.UpdatedAt = 200
	src := filepath.Join(t.TempDir(), "src.json")
	sealAt(t, src, "testpass123", []account.Account{newer})

	runMerge(t, src)

	s, _ := openSession()
	if len(s.accounts) != 1 || s.accounts[0].Issuer != "NewName" {
		t.Fatalf("newest should win: got %+v", s.accounts)
	}
}

func TestMergeSkipsContentDuplicateUnderDifferentID(t *testing.T) {
	target := withVault(t)
	sealAt(t, target, "testpass123", []account.Account{
		totp("a", "ACME", "x", 1),
	})
	// Same content (type/issuer/account/secret) but a different id.
	dup := totp("zzz", "ACME", "x", 1)
	src := filepath.Join(t.TempDir(), "src.json")
	sealAt(t, src, "testpass123", []account.Account{dup})

	runMerge(t, src)

	s, _ := openSession()
	if len(s.accounts) != 1 {
		t.Fatalf("content duplicate should be skipped, got %d accounts", len(s.accounts))
	}
}

func TestMergeClearsCollidingHandles(t *testing.T) {
	target := withVault(t)
	a := totp("a", "ACME", "x", 1)
	a.Handle = "ac"
	sealAt(t, target, "testpass123", []account.Account{a})

	b := totp("b", "Beta", "y", 2)
	b.Handle = "ac" // collides with target's handle
	src := filepath.Join(t.TempDir(), "src.json")
	sealAt(t, src, "testpass123", []account.Account{b})

	runMerge(t, src)

	s, _ := openSession()
	if err := account.CheckHandleUniqueness(s.accounts); err != nil {
		t.Fatalf("handles not unique after merge: %v", err)
	}
	var handles []string
	for _, x := range s.accounts {
		handles = append(handles, x.Handle)
	}
	if handles[0] == handles[1] {
		t.Fatalf("handles still collide: %v", handles)
	}
}
