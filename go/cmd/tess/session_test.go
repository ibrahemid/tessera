package main

import (
	"errors"
	"fmt"
	"io"
	"sync"
	"testing"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/store"
)

// addURI runs the real `add` command so the test exercises the same
// open -> mutate -> save path a user does, lock included.
func addURI(t *testing.T, uri string) error {
	t.Helper()
	c := newAddCmd()
	c.SetOut(io.Discard)
	c.SetArgs([]string{uri})
	return c.Execute()
}

// TestConcurrentAddsKeepEveryAccount is the regression test for the unlocked
// read-modify-write: without the vault lock each writer re-seals the account
// list it read at unlock, so all but the last add is silently dropped.
func TestConcurrentAddsKeepEveryAccount(t *testing.T) {
	path := withVault(t)
	sealVault(t, path, nil)

	const writers = 4
	var wg sync.WaitGroup
	errs := make([]error, writers)
	wg.Add(writers)
	for i := range writers {
		go func() {
			defer wg.Done()
			uri := fmt.Sprintf("otpauth://totp/Iss%d:user%d?secret=JBSWY3DPEHPK3PXP&issuer=Iss%d", i, i, i)
			errs[i] = addURI(t, uri)
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("writer %d: %v", i, err)
		}
	}

	s, err := openSession()
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	if len(s.accounts) != writers {
		t.Fatalf("got %d accounts after %d concurrent adds, want %d (lost updates)", len(s.accounts), writers, writers)
	}
	seen := map[string]bool{}
	for _, a := range s.accounts {
		seen[a.Issuer] = true
	}
	for i := range writers {
		if !seen[fmt.Sprintf("Iss%d", i)] {
			t.Errorf("account from writer %d is missing", i)
		}
	}
	if err := account.CheckHandleUniqueness(s.accounts); err != nil {
		t.Errorf("handles not unique after concurrent adds: %v", err)
	}
}

// TestOpenSessionHoldsTheVaultLock pins that a session owns the lock for its
// whole lifetime, and gives it back on close.
func TestOpenSessionHoldsTheVaultLock(t *testing.T) {
	path := withVault(t)
	sealVault(t, path, []account.Account{totp("a", "ACME", "x", 1)})

	s, err := openSession()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.TryAcquire(path); !errors.Is(err, store.ErrLocked) {
		t.Fatalf("vault lock was available while a session was open: %v", err)
	}
	s.close()

	l, err := store.Acquire(path)
	if err != nil {
		t.Fatalf("lock not released on close: %v", err)
	}
	if err := l.Release(); err != nil {
		t.Fatal(err)
	}
}

// TestMutateRereadsBeforeWriting covers the `tess watch` path: the view drops
// the lock while idle, so a mutation must re-read what is on disk now rather
// than re-sealing the list it unlocked with.
func TestMutateRereadsBeforeWriting(t *testing.T) {
	path := withVault(t)
	sealVault(t, path, []account.Account{hotpAcct("a", "Bank", "ops")})

	s, err := openSession()
	if err != nil {
		t.Fatal(err)
	}
	s.close() // long-running view: not holding the lock

	// Another process adds an account while the view sits idle.
	if err := addURI(t, "otpauth://totp/Later:me?secret=JBSWY3DPEHPK3PXP&issuer=Later"); err != nil {
		t.Fatalf("concurrent add: %v", err)
	}

	if err := s.mutate(func(fresh *session) error {
		i := indexByID(fresh.accounts, "a")
		if i < 0 {
			return fmt.Errorf("account a missing")
		}
		fresh.accounts[i].Counter = 7
		return nil
	}); err != nil {
		t.Fatalf("mutate: %v", err)
	}

	after, err := openSession()
	if err != nil {
		t.Fatal(err)
	}
	defer after.close()
	if len(after.accounts) != 2 {
		t.Fatalf("got %d accounts, want 2: the concurrent add was re-sealed away", len(after.accounts))
	}
	i := indexByID(after.accounts, "a")
	if i < 0 || after.accounts[i].Counter != 7 {
		t.Fatalf("counter not persisted: %+v", after.accounts)
	}
}

// TestMutateRejectsMissingAccount pins that a vanished account is reported
// instead of being re-created from the stale in-memory list.
func TestMutateRejectsMissingAccount(t *testing.T) {
	path := withVault(t)
	sealVault(t, path, []account.Account{totp("a", "ACME", "x", 1)})
	s, err := openSession()
	if err != nil {
		t.Fatal(err)
	}
	s.close()
	err = s.mutate(func(fresh *session) error {
		if indexByID(fresh.accounts, "gone") < 0 {
			return fmt.Errorf("account is no longer in the vault")
		}
		return nil
	})
	if err == nil {
		t.Fatal("expected an error for a missing account")
	}
}

func hotpAcct(id, issuer, acct string) account.Account {
	a := totp(id, issuer, acct, 1)
	a.Type = account.HOTP
	return a
}
