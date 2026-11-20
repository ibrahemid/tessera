package main

import (
	"io"
	"path/filepath"
	"testing"
	"time"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/store"
)

func TestTwoWayMergeTakesTheLocksInPathOrder(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a.json")
	dst := filepath.Join(dir, "b.json")
	withVault(t)
	vaultPath = dst
	sealVault(t, dst, []account.Account{totp("here", "Here", "a", 1)})
	sealAt(t, src, "source-pass", []account.Account{totp("there", "There", "b", 2)})
	t.Setenv("TESSERA_MERGE_PASSPHRASE", "")

	held, err := store.TryAcquire(src)
	if err != nil {
		t.Fatal(err)
	}
	reached := make(chan struct{})
	old := promptMergePassphrase
	promptMergePassphrase = func(string) (string, error) {
		close(reached)
		return "source-pass", nil
	}
	t.Cleanup(func() { promptMergePassphrase = old })

	done := make(chan error, 1)
	go func() {
		c := newMergeCmd()
		c.SetOut(io.Discard)
		c.SetErr(io.Discard)
		c.SetArgs([]string{"--two-way", src})
		done <- c.Execute()
	}()
	<-reached

	blocked := 0
	for deadline := time.Now().Add(500 * time.Millisecond); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		l, err := store.TryAcquire(dst)
		if err != nil {
			blocked++
			continue
		}
		_ = l.Release()
	}
	if err := held.Release(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("merge --two-way: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("merge --two-way did not finish after the lock was released")
	}
	if blocked > 0 {
		t.Fatalf("the merge held %s while it waited for %s (%d probes refused): a merge from the other side would wait on it", filepath.Base(dst), filepath.Base(src), blocked)
	}
	for _, path := range []string{dst, src} {
		pass := "testpass123"
		if path == src {
			pass = "source-pass"
		}
		got := ids(openVaultFile(t, path, pass))
		if _, ok := got["here"]; !ok {
			t.Errorf("%s lacks the destination account", filepath.Base(path))
		}
		if _, ok := got["there"]; !ok {
			t.Errorf("%s lacks the source account", filepath.Base(path))
		}
	}
}
