package main

import (
	"errors"
	"testing"

	"github.com/ibrahemid/tessera/go/internal/account"
)

// fakeUnlock builds an unlockProvider around scripted behavior and records which
// side effects fired.
type fakeUnlock struct {
	envPass     string
	kcPass      string
	kcHit       bool
	kcErr       error
	rightPass   string // the passphrase that actually opens the vault
	promptPass  string
	promptErr   error
	confirmYes  bool
	canRemember bool

	promptCalls  int
	confirmCalls int
	confirmText  string
	storedPass   string
	storeCalls   int
}

func (f *fakeUnlock) provider() unlockProvider {
	return unlockProvider{
		envPass: f.envPass,
		lookup:  func() (string, bool, error) { return f.kcPass, f.kcHit, f.kcErr },
		store: func(p string) error {
			f.storeCalls++
			f.storedPass = p
			return nil
		},
		prompt: func(string) (string, error) {
			f.promptCalls++
			return f.promptPass, f.promptErr
		},
		confirm: func(q string) bool {
			f.confirmCalls++
			f.confirmText = q
			return f.confirmYes
		},
		tryOpen: func(p string) ([]account.Account, error) {
			if p == f.rightPass {
				return []account.Account{{ID: "x"}}, nil
			}
			return nil, errors.New("wrong passphrase")
		},
		canRemember: f.canRemember,
	}
}

func TestResolveEnvWins(t *testing.T) {
	f := &fakeUnlock{envPass: "envpass", rightPass: "envpass", canRemember: true}
	pass, accts, err := resolvePassphrase(f.provider())
	if err != nil || pass != "envpass" || len(accts) != 1 {
		t.Fatalf("env: pass=%q err=%v accts=%d", pass, err, len(accts))
	}
	if f.promptCalls != 0 || f.confirmCalls != 0 || f.storeCalls != 0 {
		t.Fatalf("env path must not prompt/confirm/store: %+v", f)
	}
}

func TestResolveEnvWrongErrors(t *testing.T) {
	f := &fakeUnlock{envPass: "bad", rightPass: "good"}
	if _, _, err := resolvePassphrase(f.provider()); err == nil {
		t.Fatal("wrong env passphrase should error")
	}
	if f.promptCalls != 0 {
		t.Fatal("must not fall back to prompt for a wrong env passphrase")
	}
}

func TestResolveKeychainHit(t *testing.T) {
	f := &fakeUnlock{kcPass: "kc", kcHit: true, rightPass: "kc", canRemember: true}
	pass, accts, err := resolvePassphrase(f.provider())
	if err != nil || pass != "kc" || len(accts) != 1 {
		t.Fatalf("keychain hit: pass=%q err=%v", pass, err)
	}
	if f.promptCalls != 0 || f.confirmCalls != 0 || f.storeCalls != 0 {
		t.Fatalf("keychain hit must not prompt/confirm/store: %+v", f)
	}
}

func TestResolveKeychainMissPromptsAndOffers(t *testing.T) {
	f := &fakeUnlock{kcHit: false, promptPass: "typed", rightPass: "typed", confirmYes: true, canRemember: true}
	pass, _, err := resolvePassphrase(f.provider())
	if err != nil || pass != "typed" {
		t.Fatalf("miss->prompt: pass=%q err=%v", pass, err)
	}
	if f.promptCalls != 1 || f.confirmCalls != 1 || f.storeCalls != 1 || f.storedPass != "typed" {
		t.Fatalf("expected one prompt+confirm+store: %+v", f)
	}
	if f.confirmText != "Remember in the login keychain so tess stops prompting? [y/N] " {
		t.Fatalf("unexpected offer text: %q", f.confirmText)
	}
}

func TestResolveKeychainMissDeclineDoesNotStore(t *testing.T) {
	f := &fakeUnlock{kcHit: false, promptPass: "typed", rightPass: "typed", confirmYes: false, canRemember: true}
	if _, _, err := resolvePassphrase(f.provider()); err != nil {
		t.Fatal(err)
	}
	if f.confirmCalls != 1 || f.storeCalls != 0 {
		t.Fatalf("decline must not store: %+v", f)
	}
}

func TestResolveKeychainStalePromptsAndOffersUpdate(t *testing.T) {
	f := &fakeUnlock{kcPass: "old", kcHit: true, promptPass: "new", rightPass: "new", confirmYes: true, canRemember: true}
	pass, _, err := resolvePassphrase(f.provider())
	if err != nil || pass != "new" {
		t.Fatalf("stale: pass=%q err=%v", pass, err)
	}
	if f.promptCalls != 1 || f.storeCalls != 1 || f.storedPass != "new" {
		t.Fatalf("stale must prompt and update store: %+v", f)
	}
	if f.confirmText != "Stored passphrase no longer opens the vault. Update the keychain entry? [y/N] " {
		t.Fatalf("unexpected update text: %q", f.confirmText)
	}
}

func TestResolveNonInteractiveNeverOffers(t *testing.T) {
	f := &fakeUnlock{kcHit: false, promptPass: "typed", rightPass: "typed", canRemember: false}
	pass, _, err := resolvePassphrase(f.provider())
	if err != nil || pass != "typed" {
		t.Fatalf("noninteractive: pass=%q err=%v", pass, err)
	}
	if f.confirmCalls != 0 || f.storeCalls != 0 {
		t.Fatalf("non-interactive must never offer to remember: %+v", f)
	}
}

func TestResolveKeychainLookupErrorFallsToPrompt(t *testing.T) {
	f := &fakeUnlock{kcHit: true, kcErr: errors.New("locked"), promptPass: "typed", rightPass: "typed", canRemember: false}
	pass, _, err := resolvePassphrase(f.provider())
	if err != nil || pass != "typed" {
		t.Fatalf("lookup error should fall through to prompt: pass=%q err=%v", pass, err)
	}
	if f.promptCalls != 1 {
		t.Fatalf("expected one prompt after lookup error: %+v", f)
	}
}
