package main

import (
	"path/filepath"
	"testing"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/store"
)

func stamped(a account.Account, updated int64) account.Account {
	a.UpdatedAt = updated
	return a
}

func TestMergeCarriesTheTrashWithTheNewerCopy(t *testing.T) {
	cases := []struct {
		name        string
		here, there account.Account
		wantDeleted int64
		wantReport  string
	}{
		{"trashed there later", stamped(totp("a", "Acme", "me", 1), 5), trashedAt(totp("a", "Acme", "me", 1), 10), 10,
			"1 added, 1 updated, 0 unchanged"},
		{"restored there later", trashedAt(totp("a", "Acme", "me", 1), 10), stamped(totp("a", "Acme", "me", 1), 20), 0,
			"1 added, 1 updated, 0 unchanged"},
		{"trashed there earlier", stamped(totp("a", "Acme", "me", 1), 20), trashedAt(totp("a", "Acme", "me", 1), 10), 0,
			"1 added, 0 updated, 1 unchanged"},
		{"trashed here later", trashedAt(totp("a", "Acme", "me", 1), 20), stamped(totp("a", "Acme", "me", 1), 10), 20,
			"1 added, 0 updated, 1 unchanged"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dst := withVault(t)
			sealVault(t, dst, []account.Account{c.here})
			src := filepath.Join(t.TempDir(), "other.json")
			sealAt(t, src, "testpass123", []account.Account{c.there, totp("b", "Bravo", "me", 2)})
			if got, want := runMerge(t, src), "Merged "+src+": "+c.wantReport+"\n"; got != want {
				t.Errorf("report = %q, want %q", got, want)
			}
			if a := ids(openVaultFile(t, dst, "testpass123"))["a"]; a.DeletedAt != c.wantDeleted {
				t.Errorf("deleted_at = %d, want %d", a.DeletedAt, c.wantDeleted)
			}
		})
	}
}

func TestMergeBringsBackAPurgedAccountAsAdded(t *testing.T) {
	dst := withVault(t)
	sealVault(t, dst, []account.Account{totp("b", "Bravo", "me", 2)})
	src := filepath.Join(t.TempDir(), "other.json")
	sealAt(t, src, "testpass123", []account.Account{totp("a", "Acme", "me", 1), totp("b", "Bravo", "me", 2)})
	if got := runMerge(t, src); got != "Merged "+src+": 1 added, 0 updated, 1 unchanged\n" {
		t.Errorf("report = %q", got)
	}
	if a, ok := ids(openVaultFile(t, dst, "testpass123"))["a"]; !ok || a.Trashed() {
		t.Fatalf("purged account did not return live: %+v", a)
	}
}

func TestMergeLeavesTheTrashOutOfDuplicateChecks(t *testing.T) {
	dst := withVault(t)
	sealVault(t, dst, []account.Account{
		trashedAt(totp("x", "Acme", "me", 1), 10),
		totp("y", "Bravo", "me", 1),
	})
	src := filepath.Join(t.TempDir(), "other.json")
	sealAt(t, src, "testpass123", []account.Account{
		totp("x2", "Acme", "me", 3),
		trashedAt(totp("y2", "Bravo", "me", 3), 30),
	})
	if got := runMerge(t, src); got != "Merged "+src+": 2 added, 0 updated, 0 unchanged\n" {
		t.Errorf("report = %q", got)
	}
	got := ids(openVaultFile(t, dst, "testpass123"))
	if got["x2"].Trashed() || !got["y2"].Trashed() || got["y2"].DeletedAt != 30 {
		t.Fatalf("merged = %+v", got)
	}
	if err := account.CheckHandleUniqueness(openVaultFile(t, dst, "testpass123")); err != nil {
		t.Fatalf("handles collide across the Trash: %v", err)
	}
}

func TestMergeAsksForTheSourcePassphraseWithNoLockHeld(t *testing.T) {
	for _, twoWay := range []bool{false, true} {
		dst := withVault(t)
		sealVault(t, dst, []account.Account{totp("a", "Acme", "me", 1)})
		src := filepath.Join(t.TempDir(), "other.json")
		sealAt(t, src, "other-pass-456", []account.Account{totp("b", "Bravo", "me", 2)})
		t.Setenv("TESSERA_MERGE_PASSPHRASE", "")
		var lockErr error
		asked := false
		old := promptMergePassphrase
		promptMergePassphrase = func(string) (string, error) {
			asked = true
			for _, p := range []string{dst, src} {
				l, err := store.TryAcquire(p)
				if err != nil {
					lockErr = err
					continue
				}
				_ = l.Release()
			}
			return "other-pass-456", nil
		}
		t.Cleanup(func() { promptMergePassphrase = old })
		if twoWay {
			runMergeTwoWay(t, src)
		} else {
			runMerge(t, src)
		}
		if !asked {
			t.Fatalf("two-way %v: the source passphrase was not asked for", twoWay)
		}
		if lockErr != nil {
			t.Fatalf("two-way %v: a vault lock was held during the prompt: %v", twoWay, lockErr)
		}
		if _, ok := ids(openVaultFile(t, dst, "testpass123"))["b"]; !ok {
			t.Fatalf("two-way %v: the source account was not merged", twoWay)
		}
		if twoWay {
			if _, ok := ids(openVaultFile(t, src, "other-pass-456"))["a"]; !ok {
				t.Fatal("the merge did not write back into the source")
			}
		}
	}
}
