package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
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
	defer s.close()
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
	defer s.close()
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
	defer s.close()
	if len(s.accounts) != 1 {
		t.Fatalf("content duplicate should be skipped, got %d accounts", len(s.accounts))
	}
}

// TestMergeKeepsLocalHandleOnUpdate pins that a newer copy from the source
// vault updates the account's fields without renaming it: the handle is the
// string the user types here, and the source is a different vault's namespace.
func TestMergeKeepsLocalHandleOnUpdate(t *testing.T) {
	target := withVault(t)
	local := totp("a", "ACME", "x", 1)
	local.Handle = "mine"
	local.UpdatedAt = 100
	sealAt(t, target, "testpass123", []account.Account{local})

	incoming := totp("a", "ACME", "x", 1)
	incoming.Issuer = "NewName"
	incoming.Handle = "theirs"
	incoming.UpdatedAt = 200
	src := filepath.Join(t.TempDir(), "src.json")
	sealAt(t, src, "testpass123", []account.Account{incoming})

	runMerge(t, src)

	s, _ := openSession()
	defer s.close()
	if len(s.accounts) != 1 {
		t.Fatalf("want 1 account, got %d", len(s.accounts))
	}
	got := s.accounts[0]
	if got.Issuer != "NewName" {
		t.Errorf("newer copy should win on content: issuer=%q", got.Issuer)
	}
	if got.Handle != "mine" {
		t.Errorf("local handle was clobbered: got %q, want %q", got.Handle, "mine")
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
	defer s.close()
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

// runMergeTwoWay executes `tess merge --two-way <src>` and returns its output.
func runMergeTwoWay(t *testing.T, srcPath string) string {
	t.Helper()
	c := newMergeCmd()
	var buf bytes.Buffer
	c.SetOut(&buf)
	c.SetArgs([]string{"--two-way", srcPath})
	if err := c.Execute(); err != nil {
		t.Fatalf("merge --two-way: %v", err)
	}
	return buf.String()
}

// openVaultFile reads a vault file directly, the way a second Tessera install
// would see it.
func openVaultFile(t *testing.T, path, pass string) []account.Account {
	t.Helper()
	env, err := store.Load(path)
	if err != nil {
		t.Fatalf("load %s: %v", path, err)
	}
	accts, err := env.Open(pass)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	return accts
}

func ids(accts []account.Account) map[string]account.Account {
	m := map[string]account.Account{}
	for _, a := range accts {
		m[a.ID] = a
	}
	return m
}

// TestMergeTwoWayConvergesInOneRun: each vault holds an account and an edit the
// other has not seen; after one run both files hold both, with the newer copy
// of the shared account on each side.
func TestMergeTwoWayConvergesInOneRun(t *testing.T) {
	target := withVault(t)
	shared := totp("shared", "ACME", "x", 1)
	shared.UpdatedAt = 100
	onlyHere := totp("here", "Here", "h", 1)
	sealAt(t, target, "testpass123", []account.Account{shared, onlyHere})

	newerShared := totp("shared", "ACME", "x", 1)
	newerShared.Issuer = "ACME Renamed"
	newerShared.UpdatedAt = 200
	onlyThere := totp("there", "There", "t", 1)
	src := filepath.Join(t.TempDir(), "src.json")
	sealAt(t, src, "testpass123", []account.Account{newerShared, onlyThere})

	runMergeTwoWay(t, src)

	here := ids(openVaultFile(t, target, "testpass123"))
	there := ids(openVaultFile(t, src, "testpass123"))
	for name, got := range map[string]map[string]account.Account{"local": here, "source": there} {
		if len(got) != 3 {
			t.Fatalf("%s vault holds %d accounts, want 3", name, len(got))
		}
		if _, ok := got["here"]; !ok {
			t.Errorf("%s vault is missing the account only the local vault had", name)
		}
		if _, ok := got["there"]; !ok {
			t.Errorf("%s vault is missing the account only the source had", name)
		}
		if got["shared"].Issuer != "ACME Renamed" {
			t.Errorf("%s vault kept the older copy: issuer %q", name, got["shared"].Issuer)
		}
	}
}

// TestMergeTwoWaySecondRunIsANoOp: once converged, running again writes and
// changes nothing on either side.
func TestMergeTwoWaySecondRunIsANoOp(t *testing.T) {
	target := withVault(t)
	sealAt(t, target, "testpass123", []account.Account{totp("a", "ACME", "x", 1)})
	src := filepath.Join(t.TempDir(), "src.json")
	sealAt(t, src, "testpass123", []account.Account{totp("b", "Beta", "y", 2)})

	runMergeTwoWay(t, src)
	out := runMergeTwoWay(t, src)

	if !strings.Contains(out, "+0 ~0 here, +0 ~0 there") {
		t.Fatalf("a converged second run should change nothing, got %q", out)
	}
	if len(openVaultFile(t, target, "testpass123")) != 2 || len(openVaultFile(t, src, "testpass123")) != 2 {
		t.Fatal("both vaults should still hold exactly the two accounts")
	}
}

// TestMergeTwoWayEqualTimestampsWriteNothing: with the same id edited on both
// sides to the same second, neither copy wins, so repeated merges cannot
// ping-pong the account between the two files.
func TestMergeTwoWayEqualTimestampsWriteNothing(t *testing.T) {
	target := withVault(t)
	here := totp("a", "HereName", "x", 1)
	here.UpdatedAt = 500
	sealAt(t, target, "testpass123", []account.Account{here})

	there := totp("a", "ThereName", "x", 1)
	there.UpdatedAt = 500
	src := filepath.Join(t.TempDir(), "src.json")
	sealAt(t, src, "testpass123", []account.Account{there})

	runMergeTwoWay(t, src)

	if got := ids(openVaultFile(t, target, "testpass123"))["a"].Issuer; got != "HereName" {
		t.Errorf("local copy changed on an equal timestamp: %q", got)
	}
	if got := ids(openVaultFile(t, src, "testpass123"))["a"].Issuer; got != "ThereName" {
		t.Errorf("source copy changed on an equal timestamp: %q", got)
	}
}

// TestMergeTwoWayKeepsHandlesUniqueInBothVaults: each vault is its own handle
// namespace, and a handle carried across must not collide with one already
// there.
func TestMergeTwoWayKeepsHandlesUniqueInBothVaults(t *testing.T) {
	target := withVault(t)
	a := totp("a", "ACME", "x", 1)
	a.Handle = "ac"
	sealAt(t, target, "testpass123", []account.Account{a})

	b := totp("b", "Beta", "y", 2)
	b.Handle = "ac" // the same handle, a different account
	src := filepath.Join(t.TempDir(), "src.json")
	sealAt(t, src, "testpass123", []account.Account{b})

	runMergeTwoWay(t, src)

	for name, path := range map[string]string{"local": target, "source": src} {
		accts := openVaultFile(t, path, "testpass123")
		if err := account.CheckHandleUniqueness(accts); err != nil {
			t.Errorf("%s vault: %v", name, err)
		}
		for _, x := range accts {
			if x.Handle == "" {
				t.Errorf("%s vault: %s has no handle after the merge", name, x.ID)
			}
		}
	}
}

// TestMergeTwoWayRefusesSelfMerge: merging a vault into itself would take the
// same lock twice and write the file from two directions.
func TestMergeTwoWayRefusesSelfMerge(t *testing.T) {
	target := withVault(t)
	sealAt(t, target, "testpass123", []account.Account{totp("a", "ACME", "x", 1)})

	c := newMergeCmd()
	c.SetOut(&bytes.Buffer{})
	c.SetArgs([]string{"--two-way", target})
	err := c.Execute()
	if err == nil || !strings.Contains(err.Error(), "same vault") {
		t.Fatalf("want a self-merge refusal, got %v", err)
	}
}

// TestMergeOneWayLeavesTheSourceAlone pins that the default is still read-only
// on the source.
func TestMergeOneWayLeavesTheSourceAlone(t *testing.T) {
	target := withVault(t)
	sealAt(t, target, "testpass123", []account.Account{totp("a", "ACME", "x", 1)})
	src := filepath.Join(t.TempDir(), "src.json")
	sealAt(t, src, "testpass123", []account.Account{totp("b", "Beta", "y", 2)})
	before, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read source: %v", err)
	}

	out := runMerge(t, src)

	after, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read source: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Error("a one-way merge must not write the source vault")
	}
	if !strings.Contains(out, "1 added") {
		t.Errorf("one-way output changed: %q", out)
	}
}
