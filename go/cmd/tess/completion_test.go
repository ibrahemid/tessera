package main

import (
	"errors"
	"slices"
	"testing"

	"github.com/ibrahemid/tessera/go/internal/account"
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

// TestCanUnlockSilentlyFollowsTheEnvironment: $TESSERA_PASSPHRASE is the one
// source a completion can read without a prompt on every platform.
func TestCanUnlockSilentlyFollowsTheEnvironment(t *testing.T) {
	path := withVault(t) // sets $TESSERA_PASSPHRASE
	if !canUnlockSilently(path) {
		t.Error("a set $TESSERA_PASSPHRASE means the vault opens silently")
	}
}

// TestCompletionIsWiredOnAccountCommands pins that every command taking an
// account query offers handles; a missing one is invisible until a user tabs.
func TestCompletionIsWiredOnAccountCommands(t *testing.T) {
	root := newRootCmd()
	want := []string{"code", "show", "rm", "rename", "alias", "move", "tag", "export"}
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
