package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/merge"
	"github.com/ibrahemid/tessera/go/internal/store"
	"github.com/ibrahemid/tessera/go/internal/ui"
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
  - a copy that fails validation is left out
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
			return mergeFromSource(cmd, args[0], twoWay)
		},
	}
	cmd.Flags().BoolVar(&twoWay, "two-way", false, "also write the missing and newer accounts back into the source vault")
	return cmd
}

// errNothingToWrite ends a merge that changed nothing in this vault, so the
// session does not re-seal it.
var errNothingToWrite = errors.New("nothing to write")

// mergeFromSource unlocks the source with no lock held, since that may ask for its
// passphrase, then merges under the destination lock and, with twoWay, the
// source lock as well, both taken in the order withVaultPair sets.
func mergeFromSource(cmd *cobra.Command, srcPath string, twoWay bool) error {
	s, err := openSession()
	if err != nil {
		return err
	}
	defer s.close()
	shownPath := srcPath
	if twoWay {
		if srcPath, err = resolveMergeSource(srcPath); err != nil {
			return err
		}
		if err := checkTwoWaySource(s.path, srcPath); err != nil {
			return err
		}
	}
	s.releaseLock()
	key, err := unlockMergeSource(srcPath, s.passphrase)
	if err != nil {
		return err
	}
	defer zeroBytes(key.dek)
	var report string
	apply := func(fresh *session) error {
		srcAccts, env, dek, err := loadMergeSource(srcPath, key)
		if err != nil {
			return err
		}
		defer zeroBytes(dek)
		if twoWay {
			report, err = mergeTwoWay(fresh, srcPath, shownPath, srcAccts, env, dek)
			if err != nil {
				return err
			}
			return errNothingToWrite
		}
		c := mergeVault(fresh, srcAccts)
		report = mergeReport(srcPath, countsPhrase(c))
		if !c.Changed() {
			return errNothingToWrite
		}
		return nil
	}
	if twoWay {
		err = withVaultPair(s.path, srcPath, func() error { return s.mutateHeld(apply) })
	} else {
		err = s.mutate(apply)
	}
	if err != nil && !errors.Is(err, errNothingToWrite) {
		return err
	}
	out(cmd, "%s", report)
	return nil
}

// resolveMergeSource resolves a two-way source the way the vault path is
// resolved: a symbolic link names the file it points at, so the source's lock,
// temporary file, backup and rename land next to the real file and the link
// survives the write.
func resolveMergeSource(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("no vault at %s", path)
	}
	resolved, err := store.Resolve(path)
	var ipe *store.InvalidPathError
	if errors.As(err, &ipe) {
		return "", &store.InvalidPathError{Path: ipe.Path, Source: "the merge source argument"}
	}
	return resolved, err
}

// checkTwoWaySource rejects a two-way source that is the destination itself or
// does not exist.
func checkTwoWaySource(dstPath, srcPath string) error {
	same, err := sameVaultFile(dstPath, srcPath)
	if err != nil {
		return err
	}
	if same {
		return fmt.Errorf("source and destination are the same vault")
	}
	if !store.Exists(srcPath) {
		return fmt.Errorf("no vault at %s", srcPath)
	}
	return nil
}

// mergeTwoWay reconciles both vaults in one pass and writes both itself. The
// caller holds both vault locks. The local vault is written first: if that
// save fails the source is left untouched, so a run either advances both files
// or neither.
func mergeTwoWay(s *session, srcPath, shownPath string, srcAccts []account.Account, env *vault.Envelope, dek []byte) (string, error) {
	merged, srcMerged, here, there := merge.TwoWay(s.accounts, srcAccts)
	s.accounts = merged

	if here.Changed() {
		if err := s.save(); err != nil {
			return "", err
		}
	}
	if there.Changed() {
		if err := saveMergeSource(s.notifier(), srcPath, env, dek, srcMerged); err != nil {
			return "", err
		}
	}
	var sides []string
	if p := countsPhrase(here); p != "" {
		sides = append(sides, p+" here")
	}
	if p := countsPhrase(there); p != "" {
		sides = append(sides, p+" in the source")
	}
	return mergeReport(shownPath, strings.Join(sides, "; ")), nil
}

// mergeReport is the line a merge prints. Every source account lands in one
// count, so an empty summary means there was nothing to merge.
func mergeReport(path, summary string) string {
	if summary == "" {
		summary = "no accounts"
	}
	return fmt.Sprintf("Merged %s: %s", ui.Sanitize(path), summary)
}

// countsPhrase names the non-zero counts of one side in plain words.
func countsPhrase(c merge.Counts) string {
	var parts []string
	for _, p := range []struct {
		n    int
		word string
	}{
		{c.Added, "added"}, {c.Updated, "updated"}, {c.Trashed, "moved to the Trash"},
		{c.Unchanged, "unchanged"}, {c.Duplicate, "already present"}, {c.Invalid, "invalid"},
	} {
		if p.n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", p.n, p.word))
		}
	}
	return strings.Join(parts, ", ")
}

// saveMergeSource re-seals the source vault with the merged accounts, using the
// DEK that opened it so argon2id is not derived a second time.
func saveMergeSource(notify func(string), path string, env *vault.Envelope, dek []byte, accts []account.Account) error {
	// The source is its own handle namespace: a handle carried over from the
	// local vault may collide there, so collisions are cleared and reassigned
	// against this list before the uniqueness check.
	merge.DedupeHandles(accts)
	account.AssignHandles(accts)
	if err := account.CheckHandleUniqueness(accts); err != nil {
		return err
	}
	if err := env.UpdateAccountsWithDEK(dek, accts); err != nil {
		return err
	}
	return writeVault(notify, path, env)
}

// withVaultPair runs fn holding the locks of the vaults at a and b. The locks
// are taken in the order of the fully resolved paths, the same order for every
// run, so two runs locking the same two vaults cannot wait on each other.
func withVaultPair(a, b string, fn func() error) error {
	ka, err := resolvePath(a)
	if err != nil {
		return err
	}
	kb, err := resolvePath(b)
	if err != nil {
		return err
	}
	if kb < ka {
		a, b = b, a
	}
	first, err := store.Acquire(a)
	if err != nil {
		return err
	}
	defer releaseVaultLock(first)
	second, err := store.Acquire(b)
	if err != nil {
		return err
	}
	defer releaseVaultLock(second)
	return fn()
}

func releaseVaultLock(l *store.Lock) {
	if err := l.Release(); err != nil {
		fmt.Fprintln(os.Stderr, "warning:", err)
	}
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

// mergeSourceKey unlocks the source vault: the passphrase that opened it and
// the DEK it unwrapped.
type mergeSourceKey struct {
	pass string
	dek  []byte
}

// promptMergePassphrase asks for the source vault's passphrase, replaceable in
// tests.
var promptMergePassphrase = promptPassphrase

// unlockMergeSource finds the passphrase that opens the source vault. It tries
// the current vault's passphrase first (the common case: a backup of the same
// vault), then $TESSERA_MERGE_PASSPHRASE, then an interactive prompt. The
// caller must not hold a vault lock.
func unlockMergeSource(path, currentPass string) (mergeSourceKey, error) {
	env, err := store.Load(path)
	if errors.Is(err, store.ErrNotFound) {
		return mergeSourceKey{}, fmt.Errorf("no vault at %s", path)
	}
	if err != nil {
		return mergeSourceKey{}, err
	}
	if dek, err := env.UnwrapDEK(currentPass); err == nil {
		return mergeSourceKey{pass: currentPass, dek: dek}, nil
	}
	if p := os.Getenv("TESSERA_MERGE_PASSPHRASE"); p != "" {
		dek, err := env.UnwrapDEK(p)
		if errors.Is(err, vault.ErrWrongPassphrase) {
			err = &passphraseSourceError{source: "TESSERA_MERGE_PASSPHRASE", path: path, err: err}
		}
		return mergeSourceKey{pass: p, dek: dek}, err
	}
	pass, err := promptMergePassphrase("Source vault passphrase: ")
	if err != nil {
		return mergeSourceKey{}, err
	}
	dek, err := env.UnwrapDEK(pass)
	return mergeSourceKey{pass: pass, dek: dek}, err
}

// loadMergeSource is openMergeSource, replaceable in tests.
var loadMergeSource = openMergeSource

// openMergeSource loads and decrypts the source vault with key, keeping the
// envelope and a copy of the DEK so a two-way merge can write it back without
// deriving the key again. A source re-keyed since it was unlocked is unwrapped
// again with the passphrase.
func openMergeSource(path string, key mergeSourceKey) ([]account.Account, *vault.Envelope, []byte, error) {
	env, err := store.Load(path)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil, nil, fmt.Errorf("no vault at %s", path)
	}
	if err != nil {
		return nil, nil, nil, err
	}
	dek := append([]byte(nil), key.dek...)
	accts, err := env.OpenWithDEK(dek)
	if err == nil {
		return accts, env, dek, nil
	}
	zeroBytes(dek)
	if dek, err = env.UnwrapDEK(key.pass); err != nil {
		return nil, nil, nil, err
	}
	if accts, err = env.OpenWithDEK(dek); err != nil {
		zeroBytes(dek)
		return nil, nil, nil, err
	}
	return accts, env, dek, nil
}

// mergeVault unions src into the session's accounts under the merge rule.
func mergeVault(s *session, src []account.Account) merge.Counts {
	merged, c := merge.Into(s.accounts, src)
	s.accounts = merged
	return c
}
