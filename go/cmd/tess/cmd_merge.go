package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/store"
	"github.com/ibrahemid/tessera/go/internal/vault"
	"github.com/spf13/cobra"
)

// newMergeCmd folds the accounts from another Tessera vault or encrypted backup
// into the current vault. It is the tool that reconciles a CLI vault with the
// macOS app's own vault after they have diverged: export an encrypted backup
// from the app, then `tess merge` it here. With --two-way the same rule is
// applied in both directions and both files are written.
func newMergeCmd() *cobra.Command {
	var twoWay bool
	cmd := &cobra.Command{
		Use:   "merge <vault-file>",
		Short: "Merge accounts from another Tessera vault or encrypted backup into this vault",
		Long: `Merge another Tessera vault (or an encrypted backup exported from the macOS
app) into the current vault. Accounts are unioned, never dropped:

  - an account present in both (same id) keeps whichever copy was edited last
  - an account already present by content (same type/issuer/account/secret) is skipped
  - every other account is added

By default the source file is opened read-only and only the current vault is
written. --two-way applies the same rule in both directions and WRITES BOTH
FILES, so two vaults that have drifted apart end up holding the same accounts:

  tess merge --two-way ~/Library/.../app-vault.json

Each vault keeps its own handles. If the source shares this vault's passphrase
you are not prompted again; otherwise enter the source's passphrase (or set
$TESSERA_MERGE_PASSPHRASE).`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openSession()
			if err != nil {
				return err
			}
			defer s.close()
			srcPath := args[0]
			if twoWay {
				same, err := sameVaultFile(s.path, srcPath)
				if err != nil {
					return err
				}
				if same {
					return fmt.Errorf("source and destination are the same vault")
				}
			}
			srcAccts, env, dek, err := openMergeSource(srcPath, s.passphrase)
			if err != nil {
				return err
			}
			defer zeroBytes(dek)

			if !twoWay {
				added, updated, skipped := mergeVault(s, srcAccts)
				if added > 0 || updated > 0 {
					if err := s.save(); err != nil {
						return err
					}
				}
				out(cmd, "Merged %s: %d added, %d updated, %d unchanged", srcPath, added, updated, skipped)
				return nil
			}
			return mergeTwoWay(cmd, s, srcPath, srcAccts, env, dek)
		},
	}
	cmd.Flags().BoolVar(&twoWay, "two-way", false, "also write the missing and newer accounts back into the source vault")
	return cmd
}

// mergeTwoWay reconciles both vaults in one pass. The local vault is written
// first: if that save fails the source is left untouched, so a run either
// advances both files or neither.
func mergeTwoWay(cmd *cobra.Command, s *session, srcPath string, srcAccts []account.Account, env *vault.Envelope, dek []byte) error {
	// The source lock is taken AFTER the session's lock on the destination, and
	// always in that order, so two runs pointed at each other cannot deadlock.
	lock, err := store.Acquire(srcPath)
	if err != nil {
		return err
	}
	defer func() {
		if rerr := lock.Release(); rerr != nil {
			fmt.Fprintln(os.Stderr, "warning:", rerr)
		}
	}()

	local := make([]account.Account, len(s.accounts))
	copy(local, s.accounts)

	merged, hereAdded, hereUpdated, skipped := mergeInto(s.accounts, srcAccts)
	s.accounts = merged
	// The reverse direction folds the vault as it was before this run, so the
	// two directions see the same starting point.
	srcMerged, thereAdded, thereUpdated, _ := mergeInto(srcAccts, local)

	if hereAdded > 0 || hereUpdated > 0 {
		if err := s.save(); err != nil {
			return err
		}
	}
	if thereAdded > 0 || thereUpdated > 0 {
		if err := saveMergeSource(srcPath, env, dek, srcMerged); err != nil {
			return err
		}
	}
	out(cmd, "Merged %s: +%d ~%d here, +%d ~%d there, %d unchanged",
		srcPath, hereAdded, hereUpdated, thereAdded, thereUpdated, skipped)
	return nil
}

// saveMergeSource re-seals the source vault with the merged accounts, using the
// DEK that opened it so argon2id is not derived a second time.
func saveMergeSource(path string, env *vault.Envelope, dek []byte, accts []account.Account) error {
	// The source is its own handle namespace: a handle carried over from the
	// local vault may collide there, so collisions are cleared and reassigned
	// against this list before the uniqueness check.
	dedupeHandles(accts)
	account.AssignHandles(accts)
	if err := account.CheckHandleUniqueness(accts); err != nil {
		return err
	}
	if err := env.UpdateAccountsWithDEK(dek, accts); err != nil {
		return err
	}
	return store.Save(path, env)
}

// sameVaultFile reports whether two paths name the same file. Symlinks are
// resolved when they can be; a path that cannot be resolved (it may not exist
// yet) falls back to its absolute form.
func sameVaultFile(a, b string) (bool, error) {
	ra, err := resolvePath(a)
	if err != nil {
		return false, err
	}
	rb, err := resolvePath(b)
	if err != nil {
		return false, err
	}
	return ra == rb, nil
}

func resolvePath(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", fmt.Errorf("resolve %q: %w", p, err)
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved, nil
	}
	return abs, nil
}

// zeroBytes wipes key material the caller is done with.
func zeroBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

// openMergeSource loads and decrypts the source vault, keeping the envelope and
// the unwrapped DEK so a two-way merge can write it back without deriving the
// key again. It tries the current vault's passphrase first (the common case: a
// backup of the same vault), then $TESSERA_MERGE_PASSPHRASE, then an
// interactive prompt.
func openMergeSource(path, currentPass string) ([]account.Account, *vault.Envelope, []byte, error) {
	env, err := store.Load(path)
	if err != nil {
		return nil, nil, nil, err
	}
	open := func(pass string) ([]account.Account, []byte, error) {
		dek, derr := env.UnwrapDEK(pass)
		if derr != nil {
			return nil, nil, derr
		}
		accts, oerr := env.OpenWithDEK(dek)
		if oerr != nil {
			zeroBytes(dek)
			return nil, nil, oerr
		}
		return accts, dek, nil
	}
	if accts, dek, oerr := open(currentPass); oerr == nil {
		return accts, env, dek, nil
	}
	if p := os.Getenv("TESSERA_MERGE_PASSPHRASE"); p != "" {
		accts, dek, oerr := open(p)
		return accts, env, dek, oerr
	}
	pass, err := promptPassphrase("Source vault passphrase: ")
	if err != nil {
		return nil, nil, nil, err
	}
	accts, dek, err := open(pass)
	return accts, env, dek, err
}

// mergeVault unions src into the session's accounts under the merge rule.
func mergeVault(s *session, src []account.Account) (added, updated, skipped int) {
	merged, added, updated, skipped := mergeInto(s.accounts, src)
	s.accounts = merged
	return added, updated, skipped
}

// mergeInto folds src into dst under the vault merge rule and returns the new
// slice plus counts. Identity is the account id; a content match (dedupeKey)
// under a different id counts as already present. On an id collision the copy
// with the STRICTLY newer UpdatedAt wins, so an equal timestamp writes nothing
// on either side and repeated merges cannot ping-pong. Handles that would
// collide after the merge are cleared so save reassigns unique ones.
//
// It is pure: no session, no I/O, so one rule governs both directions of a
// two-way merge.
func mergeInto(dst, src []account.Account) (out []account.Account, added, updated, skipped int) {
	out = dst
	byID := map[string]int{}
	content := map[string]bool{}
	for i, a := range out {
		byID[a.ID] = i
		content[dedupeKey(a)] = true
	}
	for _, a := range src {
		if idx, ok := byID[a.ID]; ok {
			if a.UpdatedAt > out[idx].UpdatedAt {
				// The handle is how the user refers to this account in this
				// vault; the other side is a different vault's namespace, so
				// the local one is kept rather than silently renamed.
				local := out[idx].Handle
				out[idx] = a
				if local != "" {
					out[idx].Handle = local
				}
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
		byID[a.ID] = len(out)
		content[dedupeKey(a)] = true
		out = append(out, a)
		added++
	}
	dedupeHandles(out)
	return out, added, updated, skipped
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
