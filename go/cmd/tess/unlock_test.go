package main

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/secretstore"
	"github.com/ibrahemid/tessera/go/internal/secretstore/secretstoretest"
)

// swapStores replaces the platform secret stores with bs for one test.
func swapStores(t *testing.T, bs ...secretstore.Backend) {
	t.Helper()
	t.Cleanup(secretstore.SwapForTest(bs...))
}

func secretService() *secretstoretest.Fake {
	return &secretstoretest.Fake{BackendID: "secret-service", BackendName: "Secret Service"}
}

// slowStore is a secret store whose reads wait for the caller's deadline.
type slowStore struct{ *secretstoretest.Fake }

func (slowStore) Lookup(ctx context.Context, _ string) (string, bool, error) {
	<-ctx.Done()
	return "", false, ctx.Err()
}

// fakeUnlock builds an unlockProvider around scripted behavior and records which
// side effects fired.
type fakeUnlock struct {
	envPass    string
	kcPass     string
	kcHit      bool
	kcErr      error
	from       secretstore.Backend // the store a hit came from
	rightPass  string              // the passphrase that actually opens the vault
	promptPass string
	promptErr  error
	confirmYes bool
	target     secretstore.Backend // the store a typed passphrase may go to; nil offers nothing

	promptCalls  int
	confirmCalls int
	confirmText  string
	storedPass   string
	storedIn     secretstore.Backend
	storeCalls   int
}

func (f *fakeUnlock) provider() unlockProvider {
	return unlockProvider{
		envPass: f.envPass,
		lookup: func() (string, secretstore.Backend, bool, error) {
			return f.kcPass, f.from, f.kcHit, f.kcErr
		},
		rememberIn: func() secretstore.Backend { return f.target },
		store: func(b secretstore.Backend, p string) error {
			f.storeCalls++
			f.storedIn = b
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
	}
}

func TestResolveEnvWins(t *testing.T) {
	f := &fakeUnlock{envPass: "envpass", rightPass: "envpass", target: secretService()}
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

func TestResolveStoreHit(t *testing.T) {
	f := &fakeUnlock{kcPass: "kc", kcHit: true, from: secretService(), rightPass: "kc", target: secretService()}
	pass, accts, err := resolvePassphrase(f.provider())
	if err != nil || pass != "kc" || len(accts) != 1 {
		t.Fatalf("store hit: pass=%q err=%v", pass, err)
	}
	if f.promptCalls != 0 || f.confirmCalls != 0 || f.storeCalls != 0 {
		t.Fatalf("store hit must not prompt/confirm/store: %+v", f)
	}
}

func TestResolveStoreMissPromptsAndOffers(t *testing.T) {
	target := secretService()
	f := &fakeUnlock{promptPass: "typed", rightPass: "typed", confirmYes: true, target: target}
	pass, _, err := resolvePassphrase(f.provider())
	if err != nil || pass != "typed" {
		t.Fatalf("miss->prompt: pass=%q err=%v", pass, err)
	}
	if f.promptCalls != 1 || f.confirmCalls != 1 || f.storeCalls != 1 || f.storedPass != "typed" || f.storedIn != target {
		t.Fatalf("expected one prompt+confirm+store in the target: %+v", f)
	}
	if f.confirmText != "Remember in the Secret Service so tess stops prompting? [y/N] " {
		t.Fatalf("unexpected offer text: %q", f.confirmText)
	}
}

func TestResolveStoreMissDeclineDoesNotStore(t *testing.T) {
	f := &fakeUnlock{promptPass: "typed", rightPass: "typed", target: secretService()}
	if _, _, err := resolvePassphrase(f.provider()); err != nil {
		t.Fatal(err)
	}
	if f.confirmCalls != 1 || f.storeCalls != 0 {
		t.Fatalf("decline must not store: %+v", f)
	}
}

func TestResolveStaleEntryPromptsAndOffersUpdate(t *testing.T) {
	cases := []struct {
		name  string
		store secretstore.Backend
		want  string
	}{
		{"keychain", &secretstoretest.Fake{BackendID: "keychain", BackendName: "login keychain"},
			"Stored passphrase no longer opens the vault. Update the keychain entry? [y/N] "},
		{"secret service", secretService(),
			"Stored passphrase no longer opens the vault. Update the Secret Service entry? [y/N] "},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &fakeUnlock{kcPass: "old", kcHit: true, from: c.store, promptPass: "new", rightPass: "new", confirmYes: true, target: c.store}
			pass, _, err := resolvePassphrase(f.provider())
			if err != nil || pass != "new" {
				t.Fatalf("stale: pass=%q err=%v", pass, err)
			}
			if f.promptCalls != 1 || f.storeCalls != 1 || f.storedPass != "new" {
				t.Fatalf("stale must prompt and update store: %+v", f)
			}
			if f.confirmText != c.want {
				t.Fatalf("update text = %q, want %q", f.confirmText, c.want)
			}
		})
	}
}

func TestResolveWithoutTargetNeverOffers(t *testing.T) {
	f := &fakeUnlock{promptPass: "typed", rightPass: "typed"}
	pass, _, err := resolvePassphrase(f.provider())
	if err != nil || pass != "typed" {
		t.Fatalf("no target: pass=%q err=%v", pass, err)
	}
	if f.confirmCalls != 0 || f.storeCalls != 0 {
		t.Fatalf("no store target must never offer to remember: %+v", f)
	}
}

func TestResolveLookupErrorFallsToPrompt(t *testing.T) {
	f := &fakeUnlock{kcHit: true, kcErr: secretstore.ErrLocked, promptPass: "typed", rightPass: "typed"}
	pass, _, err := resolvePassphrase(f.provider())
	if err != nil || pass != "typed" {
		t.Fatalf("lookup error should fall through to prompt: pass=%q err=%v", pass, err)
	}
	if f.promptCalls != 1 {
		t.Fatalf("expected one prompt after lookup error: %+v", f)
	}
}

// unlockFixture seals a vault opened by "right-pass" and leaves only stores as
// the secret stores, with $TESSERA_PASSPHRASE unset.
func unlockFixture(t *testing.T, stores ...secretstore.Backend) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "vault.json")
	sealAt(t, path, "right-pass", []account.Account{totp("a", "ACME", "x", 1)})
	t.Setenv("TESSERA_PASSPHRASE", "")
	swapStores(t, stores...)
	return path
}

// typeAt answers the passphrase prompt with typed and records each prompt.
func typeAt(p *unlockProvider, typed string) *[]string {
	var shown []string
	p.prompt = func(q string) (string, error) {
		shown = append(shown, q)
		return typed, nil
	}
	return &shown
}

// writesTo counts the Store calls f received.
func writesTo(f *secretstoretest.Fake) int {
	n := 0
	for _, c := range f.Calls() {
		if c.Method == "Store" {
			n++
		}
	}
	return n
}

func TestUnlockReadsTheStoredPassphrase(t *testing.T) {
	store := secretService()
	path := unlockFixture(t, store)
	store.Put(path, "right-pass")
	p := interactiveUnlock(path)
	shown := typeAt(&p, "unused")

	s, err := unlockSession(path, p)
	if err != nil {
		t.Fatalf("unlock: %v", err)
	}
	if len(s.accounts) != 1 || len(*shown) != 0 {
		t.Fatalf("accounts = %d, prompts = %q", len(s.accounts), *shown)
	}
}

func TestUnlockTreatsALockedStoreAsAMiss(t *testing.T) {
	stubStdinTTY(t, true)
	store := secretService()
	path := unlockFixture(t, store)
	store.Put(path, "right-pass")
	store.Lock(path)
	visible := stubPrompts(t, nil, []string{"n"})
	p := interactiveUnlock(path)
	shown := typeAt(&p, "right-pass")

	if _, err := unlockSession(path, p); err != nil {
		t.Fatalf("unlock: %v", err)
	}
	if !slices.Equal(*shown, []string{"Vault passphrase: "}) {
		t.Fatalf("prompts = %q, want one passphrase prompt", *shown)
	}
	if !slices.Equal(*visible, []string{"Remember in the Secret Service so tess stops prompting? [y/N] "}) {
		t.Fatalf("offers = %q", *visible)
	}
	if writesTo(store) != 0 {
		t.Fatalf("a declined offer stored the passphrase: %v", store.Calls())
	}
}

func TestUnlockTreatsAnUnavailableStoreAsAMiss(t *testing.T) {
	stubStdinTTY(t, true)
	store := secretService()
	store.AvailableErr = &secretstore.UnavailableError{Backend: "Secret Service", Reason: "no session bus"}
	path := unlockFixture(t, store)
	visible := stubPrompts(t, nil, nil)
	p := interactiveUnlock(path)
	shown := typeAt(&p, "right-pass")

	if _, err := unlockSession(path, p); err != nil {
		t.Fatalf("unlock: %v", err)
	}
	if len(*shown) != 1 || len(*visible) != 0 {
		t.Fatalf("prompts = %q, offers = %q; want a prompt and no offer", *shown, *visible)
	}
	for _, c := range store.Calls() {
		if c.Method != "Available" {
			t.Fatalf("an unavailable store was used: %v", store.Calls())
		}
	}
}

func TestUnlockRemembersATypedPassphrase(t *testing.T) {
	stubStdinTTY(t, true)
	store := secretService()
	path := unlockFixture(t, store)
	visible := stubPrompts(t, nil, []string{"y"})
	p := interactiveUnlock(path)
	typeAt(&p, "right-pass")

	if _, err := unlockSession(path, p); err != nil {
		t.Fatalf("unlock: %v", err)
	}
	if !slices.Equal(*visible, []string{"Remember in the Secret Service so tess stops prompting? [y/N] "}) {
		t.Fatalf("offers = %q", *visible)
	}
	if got, ok := store.Secret(path); !ok || got != "right-pass" {
		t.Fatalf("stored = %q, %v", got, ok)
	}
	if ttl := store.TTL(path); ttl != 0 {
		t.Fatalf("an unlock stored with ttl %v, want none", ttl)
	}
}

func TestUnlockOffersToUpdateAStaleEntry(t *testing.T) {
	stubStdinTTY(t, true)
	store := secretService()
	path := unlockFixture(t, store)
	store.Put(path, "old-pass")
	visible := stubPrompts(t, nil, []string{"y"})
	p := interactiveUnlock(path)
	typeAt(&p, "right-pass")

	if _, err := unlockSession(path, p); err != nil {
		t.Fatalf("unlock: %v", err)
	}
	want := "Stored passphrase no longer opens the vault. Update the Secret Service entry? [y/N] "
	if !slices.Equal(*visible, []string{want}) {
		t.Fatalf("offers = %q, want %q", *visible, want)
	}
	if got, _ := store.Secret(path); got != "right-pass" {
		t.Fatalf("stored = %q, want the typed passphrase", got)
	}
}

func TestUnlockOffATerminalNeverOffersToRemember(t *testing.T) {
	store := secretService()
	path := unlockFixture(t, store)
	visible := stubPrompts(t, nil, []string{"y"})
	p := interactiveUnlock(path)
	typeAt(&p, "right-pass")

	if _, err := unlockSession(path, p); err != nil {
		t.Fatalf("unlock: %v", err)
	}
	if len(*visible) != 0 || writesTo(store) != 0 {
		t.Fatalf("offers = %q, calls = %v", *visible, store.Calls())
	}
}

func TestUnlockOffATerminalNamesTheVariable(t *testing.T) {
	path := withVault(t)
	sealAt(t, path, "right-pass", []account.Account{totp("a", "ACME", "x", 1)})
	t.Setenv("TESSERA_PASSPHRASE", "")
	swapStores(t, secretService())

	_, stderr, err := runRootStreams(t, "list")
	if !errors.Is(err, errNoUnlockTerminal) {
		t.Fatalf("err = %v, want the terminal error", err)
	}
	if stderr != "error: set $TESSERA_PASSPHRASE or run on a terminal to enter the vault passphrase\n" {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestUnlockOffATerminalNamesAStaleStore(t *testing.T) {
	store := secretService()
	path := unlockFixture(t, store)
	store.Put(path, "old-pass")

	_, err := unlockSession(path, interactiveUnlock(path))
	want := "the Secret Service passphrase doesn't open " + path + "; run `tess vault forget`"
	if got := userMessage(err); got != want {
		t.Fatalf("message = %q, want %q", got, want)
	}
}

func TestLookupStoredStopsAtTheDeadline(t *testing.T) {
	store := slowStore{secretService()}
	swapStores(t, store)

	start := time.Now()
	_, _, hit, err := lookupStored("/v/vault.json", 20*time.Millisecond)
	if hit || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("hit = %v, err = %v; want a deadline miss", hit, err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("lookup took %v past a 20ms deadline", elapsed)
	}
}
