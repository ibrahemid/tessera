package main

import (
	"errors"
	"os"
	"strings"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/keychain"
	"github.com/ibrahemid/tessera/go/internal/store"
	"github.com/spf13/cobra"
)

// errNoSilentUnlock reports that listing accounts would have to ask for the
// passphrase. A completion runs while the user is mid-command, so it offers
// nothing rather than taking over the terminal with a prompt.
var errNoSilentUnlock = errors.New("vault cannot be opened without prompting")

// completionAccounts loads the accounts offered as completions, replaceable in tests.
var completionAccounts = loadCompletionAccounts

// loadCompletionAccounts opens the vault only when the passphrase is already
// available from the environment or the login keychain.
func loadCompletionAccounts() ([]account.Account, error) {
	path, err := store.Resolve(vaultPath)
	if err != nil {
		return nil, err
	}
	if !canUnlockSilently(path) {
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
	s, err := loadSession(path)
	if err != nil {
		return nil, err
	}
	return s.accounts, nil
}

// canUnlockSilently reports whether the vault passphrase can be resolved
// without prompting.
func canUnlockSilently(path string) bool {
	if os.Getenv("TESSERA_PASSPHRASE") != "" {
		return true
	}
	_, hit, err := keychain.Lookup(path)
	return err == nil && hit
}

// completeHandles offers the handles matching prefix, each described by its
// issuer and account so two handles for the same service stay distinguishable.
func completeHandles(prefix string) ([]string, cobra.ShellCompDirective) {
	accts, err := completionAccounts()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	p := strings.ToLower(prefix)
	matches := make([]string, 0, len(accts))
	for _, a := range accts {
		if a.Handle == "" || !strings.HasPrefix(a.Handle, p) {
			continue
		}
		if l := label(a); l != "" {
			matches = append(matches, a.Handle+"\t"+l)
			continue
		}
		matches = append(matches, a.Handle)
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
