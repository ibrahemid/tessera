package main

import (
	"testing"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/store"
)

func TestSessionCloseClearsTheKey(t *testing.T) {
	path := withVault(t)
	sealVault(t, path, []account.Account{totp("a", "ACME", "x", 1)})
	s, err := openSession()
	if err != nil {
		t.Fatal(err)
	}
	key := s.dek
	if len(key) == 0 {
		t.Fatal("the session holds no key")
	}
	s.close()
	for i, b := range key {
		if b != 0 {
			t.Fatalf("byte %d of the key is still set after close", i)
		}
	}
	if s.dek != nil {
		t.Fatal("the session still points at a key after close")
	}
	s.close()
}

func TestSessionReleaseLockKeepsTheKey(t *testing.T) {
	path := withVault(t)
	sealVault(t, path, []account.Account{totp("a", "ACME", "x", 1)})
	s, err := openSession()
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	want := append([]byte(nil), s.dek...)
	s.releaseLock()
	lock, err := store.TryAcquire(path)
	if err != nil {
		t.Fatalf("the lock is still held: %v", err)
	}
	_ = lock.Release()
	if string(s.dek) != string(want) {
		t.Fatal("releaseLock changed the key")
	}
	if err := s.mutate(func(fresh *session) error {
		fresh.accounts = append(fresh.accounts, totp("b", "Other", "o", 2))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
