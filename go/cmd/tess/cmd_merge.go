package main

import (
	"os"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/store"
	"github.com/spf13/cobra"
)

// newMergeCmd folds the accounts from another Tessera vault or encrypted backup
// into the current vault. It is the tool that reconciles a CLI vault with the
// macOS app's own vault after they have diverged: export an encrypted backup
// from the app, then `tess merge` it here.
func newMergeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "merge <vault-file>",
		Short: "Merge accounts from another Tessera vault or encrypted backup into this vault",
		Long: `Merge another Tessera vault (or an encrypted backup exported from the macOS
app) into the current vault. Accounts are unioned, never dropped:

  - an account present in both (same id) keeps whichever copy was edited last
  - an account already present by content (same type/issuer/account/secret) is skipped
  - every other account is added

The source file is opened read-only; only the current vault is written. If the
source shares this vault's passphrase you are not prompted again; otherwise enter
the source's passphrase (or set $TESSERA_MERGE_PASSPHRASE).`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openSession()
			if err != nil {
				return err
			}
			src, err := openMergeSource(args[0], s.passphrase)
			if err != nil {
				return err
			}
			added, updated, skipped := mergeVault(s, src)
			if added > 0 || updated > 0 {
				if err := s.save(); err != nil {
					return err
				}
			}
			out(cmd, "Merged %s: %d added, %d updated, %d unchanged", args[0], added, updated, skipped)
			return nil
		},
	}
}

// openMergeSource loads and decrypts the source vault. It tries the current
// vault's passphrase first (the common case: a backup of the same vault), then
// $TESSERA_MERGE_PASSPHRASE, then an interactive prompt.
func openMergeSource(path, currentPass string) ([]account.Account, error) {
	env, err := store.Load(path)
	if err != nil {
		return nil, err
	}
	if accts, oerr := env.Open(currentPass); oerr == nil {
		return accts, nil
	}
	if p := os.Getenv("TESSERA_MERGE_PASSPHRASE"); p != "" {
		return env.Open(p)
	}
	pass, err := promptPassphrase("Source vault passphrase: ")
	if err != nil {
		return nil, err
	}
	return env.Open(pass)
}

// mergeVault unions src into s.accounts. Identity is the account id; a content
// match (dedupeKey) under a different id counts as already present. On an id
// collision the copy with the newer UpdatedAt wins. Handles that would collide
// after the merge are cleared so `save` reassigns unique ones.
func mergeVault(s *session, src []account.Account) (added, updated, skipped int) {
	byID := map[string]int{}
	content := map[string]bool{}
	for i, a := range s.accounts {
		byID[a.ID] = i
		content[dedupeKey(a)] = true
	}
	for _, a := range src {
		if idx, ok := byID[a.ID]; ok {
			if a.UpdatedAt > s.accounts[idx].UpdatedAt {
				s.accounts[idx] = a
				updated++
			} else {
				skipped++
			}
			continue
		}
		if content[dedupeKey(a)] {
			skipped++
			continue
		}
		if err := a.Validate(); err != nil {
			skipped++
			continue
		}
		byID[a.ID] = len(s.accounts)
		content[dedupeKey(a)] = true
		s.accounts = append(s.accounts, a)
		added++
	}
	dedupeHandles(s.accounts)
	return added, updated, skipped
}

// dedupeHandles clears any handle that repeats one already seen earlier in the
// slice, keeping the first occurrence. Cleared handles are regenerated uniquely
// by account.AssignHandles during save.
func dedupeHandles(accts []account.Account) {
	seen := map[string]bool{}
	for i := range accts {
		h := accts[i].Handle
		if h == "" {
			continue
		}
		if seen[h] {
			accts[i].Handle = ""
			continue
		}
		seen[h] = true
	}
}
