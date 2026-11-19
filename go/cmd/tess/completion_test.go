package main

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/secretstore"
	"github.com/ibrahemid/tessera/go/internal/secretstore/secretstoretest"
	"github.com/ibrahemid/tessera/go/internal/vault"
	"github.com/spf13/cobra"
)

// stubCompletionAccounts replaces the completion's vault loader.
func stubCompletionAccounts(t *testing.T, fn func() ([]account.Account, error)) {
	t.Helper()
	old := completionAccounts
	completionAccounts = fn
	t.Cleanup(func() { completionAccounts = old })
}

func handled(id, issuer, acct, handle string) account.Account {
	a := totp(id, issuer, acct, 1)
	a.Handle = handle
	return a
}

func TestCompleteHandlesDescribesEachAccount(t *testing.T) {
	stubCompletionAccounts(t, func() ([]account.Account, error) {
		return []account.Account{
			handled("a", "GitHub", "dev", "gdev"),
			handled("b", "GitHub", "ops", "gops"),
			handled("c", "ACME", "me", "acme"),
		}, nil
	})

	got, directive := completeHandles("g")
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Errorf("directive = %v, want NoFileComp", directive)
	}
	want := []string{"gdev\tGitHub (dev)", "gops\tGitHub (ops)"}
	if !slices.Equal(got, want) {
		t.Errorf("completeHandles(\"g\") = %v, want %v", got, want)
	}
}

func TestCompleteHandlesIsCaseInsensitive(t *testing.T) {
	stubCompletionAccounts(t, func() ([]account.Account, error) {
		return []account.Account{handled("a", "ACME", "me", "acme")}, nil
	})

	got, _ := completeHandles("AC")
	if len(got) != 1 {
		t.Fatalf("completeHandles(\"AC\") = %v, want the acme handle", got)
	}
}

// TestCompleteHandlesStaysSilentWhenLocked pins that a completion never blocks
// the user's shell on a passphrase prompt: it just offers nothing.
func TestCompleteHandlesStaysSilentWhenLocked(t *testing.T) {
	stubCompletionAccounts(t, func() ([]account.Account, error) {
		return nil, errNoSilentUnlock
	})

	got, directive := completeHandles("")
	if len(got) != 0 {
		t.Errorf("a locked vault must offer no candidates, got %v", got)
	}
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Errorf("directive = %v, want NoFileComp", directive)
	}
}

func TestCompleteHandlesSwallowsVaultErrors(t *testing.T) {
	stubCompletionAccounts(t, func() ([]account.Account, error) {
		return nil, errors.New("vault is corrupt")
	})

	if got, _ := completeHandles(""); len(got) != 0 {
		t.Errorf("a broken vault must offer no candidates, got %v", got)
	}
}

// TestCompleteFirstArgOnlyCompletesTheQuerySlot: `tess alias work <tab>` wants a
// new handle, not one that already exists.
func TestCompleteFirstArgOnlyCompletesTheQuerySlot(t *testing.T) {
	stubCompletionAccounts(t, func() ([]account.Account, error) {
		return []account.Account{handled("a", "ACME", "me", "acme")}, nil
	})

	if got, _ := completeFirstArg(nil, nil, ""); len(got) != 1 {
		t.Errorf("first slot should complete handles, got %v", got)
	}
	if got, _ := completeFirstArg(nil, []string{"acme"}, ""); len(got) != 0 {
		t.Errorf("second slot should offer nothing, got %v", got)
	}
}

// TestSilentUnlockFollowsTheEnvironment: $TESSERA_PASSPHRASE is the one
// source a completion can read without a prompt on every platform.
func TestSilentUnlockFollowsTheEnvironment(t *testing.T) {
	path := withVault(t) // sets $TESSERA_PASSPHRASE
	store := secretService()
	swapStores(t, store)
	p, ok := silentUnlock(path)
	if !ok {
		t.Fatal("a set $TESSERA_PASSPHRASE means the vault opens silently")
	}
	if _, err := p.prompt("Vault passphrase: "); !errors.Is(err, errNoSilentUnlock) {
		t.Fatalf("a silent unlock prompted: %v", err)
	}
	if len(store.Calls()) != 0 {
		t.Fatalf("the environment passphrase still read the store: %v", store.Calls())
	}
}

// completionFixture seals a vault opened by "right-pass" at the resolved path,
// with $TESSERA_PASSPHRASE unset and store as the only secret store.
func completionFixture(t *testing.T, store secretstore.Backend) string {
	t.Helper()
	path := withVault(t)
	sealAt(t, path, "right-pass", []account.Account{handled("a", "ACME", "me", "acme")})
	t.Setenv("TESSERA_PASSPHRASE", "")
	swapStores(t, store)
	return path
}

func TestCompletionReadsTheStoreOnce(t *testing.T) {
	store := secretService()
	path := completionFixture(t, store)
	store.Put(path, "right-pass")

	accts, err := loadCompletionAccounts()
	if err != nil || len(accts) != 1 {
		t.Fatalf("accounts = %d, err = %v", len(accts), err)
	}
	lookups := 0
	for _, c := range store.Calls() {
		if c.Method == "Lookup" {
			lookups++
		}
	}
	if lookups != 1 {
		t.Fatalf("lookups = %d, want 1: %v", lookups, store.Calls())
	}
}

// TestCompletionNeverPrompts pins that a completion offers nothing, without a
// prompt, when the store cannot supply a passphrase that opens the vault.
func TestCompletionNeverPrompts(t *testing.T) {
	cases := []struct {
		name  string
		setup func(f *secretstoretest.Fake, path string)
		want  error
	}{
		{"missing entry", func(*secretstoretest.Fake, string) {}, errNoSilentUnlock},
		{"locked store", func(f *secretstoretest.Fake, path string) {
			f.Put(path, "right-pass")
			f.Lock(path)
		}, errNoSilentUnlock},
		{"unavailable store", func(f *secretstoretest.Fake, path string) {
			f.Put(path, "right-pass")
			f.AvailableErr = &secretstore.UnavailableError{Backend: "Secret Service", Reason: "no session bus"}
		}, errNoSilentUnlock},
		{"stale entry", func(f *secretstoretest.Fake, path string) { f.Put(path, "old-pass") }, vault.ErrWrongPassphrase},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			stubStdinTTY(t, true)
			store := secretService()
			path := completionFixture(t, store)
			c.setup(store, path)
			shown := stubPrompts(t, []string{"right-pass"}, []string{"y"})

			accts, err := loadCompletionAccounts()
			if !errors.Is(err, c.want) || len(accts) != 0 {
				t.Fatalf("accounts = %d, err = %v; want %v", len(accts), err, c.want)
			}
			if len(*shown) != 0 || writesTo(store) != 0 {
				t.Fatalf("prompts = %q, calls = %v", *shown, store.Calls())
			}
			if got, _ := completeHandles(""); len(got) != 0 {
				t.Fatalf("completeHandles = %v, want nothing", got)
			}
		})
	}
}

func TestCompletionLookupStopsAtItsDeadline(t *testing.T) {
	path := completionFixture(t, slowStore{secretService()})

	start := time.Now()
	if _, ok := silentUnlock(path); ok {
		t.Fatal("a store that never answers must be a miss")
	}
	if elapsed := time.Since(start); elapsed > completionLookupTimeout+time.Second {
		t.Fatalf("lookup took %v, past the %v deadline", elapsed, completionLookupTimeout)
	}
}

// TestCompletionIsWiredOnAccountCommands pins that every command taking an
// account query offers handles; a missing one is invisible until a user tabs.
func TestCompletionIsWiredOnAccountCommands(t *testing.T) {
	root := newRootCmd()
	want := []string{
		"code", "show", "rm", "rename", "alias", "move", "tag", "export",
		"restore", "pin", "unpin", "order", "note", "edit",
	}
	for _, name := range want {
		c, _, err := root.Find([]string{name})
		if err != nil {
			t.Fatalf("find %q: %v", name, err)
		}
		if c.ValidArgsFunction == nil {
			t.Errorf("tess %s does not complete account handles", name)
		}
	}
	if root.ValidArgsFunction == nil {
		t.Error("the bare `tess <query>` shortcut does not complete account handles")
	}
}
