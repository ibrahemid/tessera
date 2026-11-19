package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/store"
	"github.com/ibrahemid/tessera/go/internal/vault"
)

func TestTwoWayMergeReadsTheSourceUnderItsLock(t *testing.T) {
	dst := withVault(t)
	sealVault(t, dst, []account.Account{totp("here", "Here", "a", 1)})
	src := filepath.Join(t.TempDir(), "other.json")
	sealAt(t, src, "testpass123", []account.Account{totp("there", "There", "b", 2)})

	var lockErr error
	old := loadMergeSource
	loadMergeSource = func(path string, key mergeSourceKey) ([]account.Account, *vault.Envelope, []byte, error) {
		if l, err := store.TryAcquire(path); err == nil {
			_ = l.Release()
		} else {
			lockErr = err
		}
		return old(path, key)
	}
	t.Cleanup(func() { loadMergeSource = old })

	runMergeTwoWay(t, src)
	if !errors.Is(lockErr, store.ErrLocked) {
		t.Fatalf("the source was read without its lock held (TryAcquire: %v)", lockErr)
	}
	if _, ok := ids(openVaultFile(t, src, "testpass123"))["here"]; !ok {
		t.Fatal("the merge did not write back into the source")
	}
	l, err := store.TryAcquire(src)
	if err != nil {
		t.Fatalf("the source lock outlived the merge: %v", err)
	}
	_ = l.Release()
}

func TestTwoWayMergeWithAMissingSourceLeavesNoLockFile(t *testing.T) {
	dst := withVault(t)
	sealVault(t, dst, []account.Account{totp("here", "Here", "a", 1)})
	src := filepath.Join(t.TempDir(), "missing.json")
	_, stderr, err := runRootStreams(t, "merge", "--two-way", src)
	if err == nil || !strings.Contains(stderr, "no vault at "+src) {
		t.Fatalf("err = %v stderr = %q", err, stderr)
	}
	if _, err := os.Stat(store.LockPath(src)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a lock file was created next to a missing source: %v", err)
	}
}
