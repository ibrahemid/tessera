//go:build darwin

package main

import (
	"slices"
	"testing"

	"github.com/ibrahemid/tessera/go/internal/secretstore"
	"github.com/ibrahemid/tessera/go/internal/secretstore/secretstoretest"
)

// loginKeychain is an in-memory store that carries the identity of the
// platform's first secret store, so output can be compared without touching the
// real keychain.
func loginKeychain(t *testing.T) *secretstoretest.Fake {
	t.Helper()
	registered := secretstore.Backends()
	if len(registered) == 0 {
		t.Fatal("no secret store is registered on macOS")
	}
	return &secretstoretest.Fake{BackendID: registered[0].ID(), BackendName: registered[0].Name()}
}

func TestMacOutputMatchesTheKeychainWording(t *testing.T) {
	if !isKeychainWording {
		t.Fatal("macOS must print the keychain wording")
	}
	path := withVault(t)
	k := loginKeychain(t)
	swapStores(t, k)

	status := func() string {
		t.Helper()
		got, err := runRoot(t, "vault", "status")
		if err != nil {
			t.Fatalf("status: %v", err)
		}
		return got
	}
	if got, want := status(), "Path:     "+path+"\nFile:     not found\nKeychain: no entry\n"; got != want {
		t.Fatalf("status = %q, want %q", got, want)
	}
	k.Put(path, "testpass123")
	if got, want := status(), "Path:     "+path+"\nFile:     not found\nKeychain: entry present\n"; got != want {
		t.Fatalf("status = %q, want %q", got, want)
	}

	got, err := runRoot(t, "vault", "forget")
	if err != nil || got != "Removed any keychain entry for "+path+"\n" {
		t.Fatalf("forget = %q, %v", got, err)
	}
	got, err = runRoot(t, "vault", "forget")
	if err != nil || got != "Removed any keychain entry for "+path+"\n" {
		t.Fatalf("forget without an entry = %q, %v", got, err)
	}

	initVault(t)
	got, err = runRoot(t, "vault", "remember")
	if err != nil || got != "Stored passphrase for "+path+" in the login keychain\n" {
		t.Fatalf("remember = %q, %v", got, err)
	}
}

func TestMacPromptsMatchTheKeychainWording(t *testing.T) {
	stubStdinTTY(t, true)
	k := loginKeychain(t)
	path := unlockFixture(t, k)

	visible := stubPrompts(t, nil, []string{"n"})
	p := interactiveUnlock(path)
	typeAt(&p, "right-pass")
	if _, err := unlockSession(path, p); err != nil {
		t.Fatalf("unlock: %v", err)
	}
	if want := []string{"Remember in the login keychain so tess stops prompting? [y/N] "}; !slices.Equal(*visible, want) {
		t.Fatalf("offer = %q, want %q", *visible, want)
	}

	k.Put(path, "old-pass")
	visible = stubPrompts(t, nil, []string{"n"})
	p = interactiveUnlock(path)
	typeAt(&p, "right-pass")
	if _, err := unlockSession(path, p); err != nil {
		t.Fatalf("unlock: %v", err)
	}
	if want := []string{"Stored passphrase no longer opens the vault. Update the keychain entry? [y/N] "}; !slices.Equal(*visible, want) {
		t.Fatalf("offer = %q, want %q", *visible, want)
	}
}
