package main

import (
	"errors"
	"os"
	"strings"
	"time"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/secretstore"
	"github.com/ibrahemid/tessera/go/internal/store"
	"github.com/spf13/cobra"
)

// errNoSilentUnlock reports that listing accounts would have to ask for the
// passphrase. A completion runs while the user is mid-command, so it offers
// nothing rather than taking over the terminal with a prompt.
var errNoSilentUnlock = errors.New("vault cannot be opened without prompting")

// completionAccounts loads the accounts offered as completions, replaceable in tests.
var completionAccounts = loadCompletionAccounts

// completionLookupTimeout bounds the secret store read behind a completion.
const completionLookupTimeout = time.Second

// loadCompletionAccounts opens the vault only when the passphrase is already
// available from the environment or the secret store.
func loadCompletionAccounts() ([]account.Account, error) {
	path, err := store.Resolve(vaultPath)
	if err != nil {
		return nil, err
	}
	p, ok := silentUnlock(path)
	if !ok {
		return nil, errNoSilentUnlock
	}
	// store.Acquire waits for the lock; a completion that blocks holds the
	// user's Tab key hostage while another tess process finishes, so this path
	// takes the lock or gives up at once.
	lock, err := store.TryAcquire(path)
	if err != nil {
		return nil, err
	}
	defer lock.Release()
	s, err := unlockSession(path, p)
	if err != nil {
		return nil, err
	}
	return s.accounts, nil
}

// silentUnlock returns an unlock that never prompts, and whether a passphrase
// is available for it from the environment or the secret store. A locked or
// unavailable store is a miss.
func silentUnlock(path string) (unlockProvider, bool) {
	refuse := func(string) (string, error) { return "", errNoSilentUnlock }
	if pass := os.Getenv("TESSERA_PASSPHRASE"); pass != "" {
		return unlockProvider{envPass: pass, prompt: refuse}, true
	}
	pass, from, hit, err := lookupStored(path, completionLookupTimeout)
	if err != nil || !hit {
		return unlockProvider{}, false
	}
	return unlockProvider{
		lookup: func() (string, secretstore.Backend, bool, error) { return pass, from, true, nil },
		prompt: refuse,
	}, true
}

// completeHandles offers the live handles matching prefix, each described by
// its issuer and account so two handles for the same service stay
// distinguishable.
func completeHandles(prefix string) ([]string, cobra.ShellCompDirective) {
	return completeHandlesWhere(prefix, isLive, nil)
}

func isLive(a account.Account) bool { return !a.Trashed() }

// completeHandlesWhere offers the handles matching prefix among the accounts
// keep accepts, leaving out the handles in exclude.
func completeHandlesWhere(prefix string, keep func(account.Account) bool, exclude []string) ([]string, cobra.ShellCompDirective) {
	accts, err := completionAccounts()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	skip := make(map[string]bool, len(exclude))
	for _, e := range exclude {
		skip[strings.ToLower(e)] = true
	}
	p := strings.ToLower(prefix)
	matches := make([]string, 0, len(accts))
	for _, a := range accts {
		if a.Handle == "" || !keep(a) || skip[a.Handle] || !strings.HasPrefix(a.Handle, p) {
			continue
		}
		if l := label(a); l != "" {
			matches = append(matches, displayHandle(a)+"\t"+l)
			continue
		}
		matches = append(matches, displayHandle(a))
	}
	return matches, cobra.ShellCompDirectiveNoFileComp
}

// completeFirstArg completes an account handle in the first positional slot,
// which is where every account-taking command expects its query.
func completeFirstArg(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return completeHandles(toComplete)
}

// completeEveryArg completes a live handle in every positional slot, for the
// commands that take several queries, leaving out the ones already given.
func completeEveryArg(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return completeHandlesWhere(toComplete, isLive, args)
}

// completeTrashedArgs completes the handles of accounts in the Trash, for
// tess restore.
func completeTrashedArgs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return completeHandlesWhere(toComplete, account.Account.Trashed, args)
}

// completeRemoveArgs completes live handles for tess rm, and the Trash as well
// once --purge is given.
func completeRemoveArgs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	keep := isLive
	if cmd != nil && cmd.Flags().Changed("purge") {
		keep = func(account.Account) bool { return true }
	}
	return completeHandlesWhere(toComplete, keep, args)
}
