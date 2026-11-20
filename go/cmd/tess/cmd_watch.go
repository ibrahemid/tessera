package main

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/detect"
	"github.com/ibrahemid/tessera/go/internal/store"
	"github.com/ibrahemid/tessera/go/internal/tui"
	"github.com/ibrahemid/tessera/go/internal/vault"
	"github.com/spf13/cobra"
)

// runWatchView starts the watch view, replaceable in tests.
var runWatchView = tui.Run

func newWatchCmd() *cobra.Command {
	var lockAfter string
	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Live, auto-refreshing view with countdown bars and copy",
		RunE: func(cmd *cobra.Command, args []string) error {
			wipe, err := resolveClipClear(0, false, true)
			if err != nil {
				return err
			}
			idle, err := resolveWatchLock(lockAfter, cmd.Flags().Changed("lock-after"))
			if err != nil {
				return err
			}
			w, err := openWatchVault()
			if err != nil {
				return err
			}
			return runWatchView(w.options(wipe.schedule, idle))
		},
	}
	cmd.Flags().StringVar(&lockAfter, "lock-after", "",
		"lock the view after this long without a key press, such as 5m, 0 to never lock (default $TESSERA_WATCH_LOCK, else 15m)")
	return cmd
}

const (
	// watchLockEnv names the variable that sets how long the view waits for a
	// key press before it locks.
	watchLockEnv     = "TESSERA_WATCH_LOCK"
	watchLockDefault = 15 * time.Minute
)

// watchLockError reports a lock delay that is not a duration of zero or more.
type watchLockError struct {
	// source is "--lock-after" or the environment variable name.
	source string
	value  string
}

func (e *watchLockError) Error() string {
	return fmt.Sprintf("%s %q is not a duration; use a value such as 5m or 1h, or 0 to never lock", e.source, e.value)
}

// resolveWatchLock picks the idle time before the view locks: the flag, then
// $TESSERA_WATCH_LOCK, then 15 minutes. Zero never locks.
func resolveWatchLock(flag string, flagSet bool) (time.Duration, error) {
	source, raw := "--lock-after", flag
	if !flagSet {
		source, raw = watchLockEnv, os.Getenv(watchLockEnv)
		if strings.TrimSpace(raw) == "" {
			return watchLockDefault, nil
		}
	}
	d, err := time.ParseDuration(strings.TrimSpace(raw))
	if err != nil || d < 0 {
		return 0, &watchLockError{source: source, value: raw}
	}
	return d, nil
}

// watchVault connects the watch view to the vault. The view stays open
// indefinitely, so it never holds the vault lock while idle: every write
// re-reads the vault under the lock, and reads rely on atomic renames. It
// keeps the vault key but not the passphrase; a vault re-keyed elsewhere is
// unlocked again from the view.
type watchVault struct {
	mu   sync.Mutex
	s    *session
	stat os.FileInfo
}

var (
	errWatchUnchanged = errors.New("nothing to write")
	errWatchGone      = errors.New("the account is no longer in the vault")
	errWatchTrashed   = errors.New("the account is in the Trash")
)

func openWatchVault() (*watchVault, error) {
	s, err := openSession()
	if err != nil {
		return nil, err
	}
	defer s.close()
	s.passphrase = ""
	info, err := os.Stat(s.path)
	if err != nil {
		return nil, err
	}
	return &watchVault{s: s, stat: info}, nil
}

func (w *watchVault) options(clearCopy func(string) error, lockAfter time.Duration) tui.Options {
	return tui.Options{
		Accounts:     slices.Clone(w.s.accounts),
		Reload:       w.reload,
		Mutate:       w.mutate,
		Unlock:       w.unlock,
		UnlockStored: w.unlockStored,
		Lock:         w.lock,
		LockAfter:    lockAfter,
		Copy:         func(text string) error { return copyToClipboard(text) },
		ClearCopy:    clearCopy,
		Now:          now,
	}
}

func fileChanged(was, is os.FileInfo) bool {
	return !os.SameFile(was, is) || !was.ModTime().Equal(is.ModTime()) || was.Size() != is.Size()
}

// watchErr carries the message the view shows and keeps the cause for errors.Is.
type watchErr struct {
	msg string
	err error
}

func (e watchErr) Error() string { return e.msg }
func (e watchErr) Unwrap() error { return e.err }

// watchError turns a vault error into what the view shows. A vault the held
// key no longer opens was re-keyed elsewhere.
func watchError(err error) error {
	if errors.Is(err, vault.ErrWrongPassphrase) {
		return tui.ErrKeyReplaced
	}
	return watchErr{userMessage(err), err}
}

func (w *watchVault) reload() ([]account.Account, bool, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.s.dek == nil {
		return nil, false, nil
	}
	info, err := os.Stat(w.s.path)
	if err != nil {
		return nil, false, watchError(err)
	}
	if !fileChanged(w.stat, info) {
		return nil, false, nil
	}
	env, err := store.Load(w.s.path)
	if err != nil {
		return nil, false, watchError(err)
	}
	accts, err := env.OpenWithDEK(w.s.dek)
	if err != nil {
		w.stat = info
		return nil, false, watchError(err)
	}
	account.AssignHandles(accts)
	w.s.env, w.s.accounts, w.stat = env, accts, info
	return slices.Clone(accts), true, nil
}

// unlock opens the vault with a typed passphrase and keeps only its key.
func (w *watchVault) unlock(passphrase string) ([]account.Account, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	accts, err := w.open(passphrase)
	if err != nil {
		return nil, watchErr{userMessage(err), err}
	}
	return accts, nil
}

// unlockStored opens the vault the way every command does without a prompt:
// with $TESSERA_PASSPHRASE, else the passphrase remembered for the vault. It
// returns tui.ErrPassphraseNeeded when neither is set.
func (w *watchVault) unlockStored() ([]account.Account, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	p := interactiveUnlock(w.s.path)
	p.path = w.s.path
	p.rememberIn = nil
	p.prompt = func(string) (string, error) { return "", tui.ErrPassphraseNeeded }
	p.tryOpen = w.open
	_, accts, err := resolvePassphrase(p)
	switch {
	case errors.Is(err, tui.ErrPassphraseNeeded):
		return nil, err
	case err != nil:
		return nil, watchErr{userMessage(err), err}
	}
	return accts, nil
}

// open reads the vault, unwraps its key with passphrase and keeps the key and
// the accounts. The caller holds w.mu.
func (w *watchVault) open(passphrase string) ([]account.Account, error) {
	info, err := os.Stat(w.s.path)
	if err != nil {
		return nil, err
	}
	env, err := store.Load(w.s.path)
	if err != nil {
		return nil, err
	}
	dek, err := env.UnwrapDEK(passphrase)
	if err != nil {
		return nil, err
	}
	accts, err := env.OpenWithDEK(dek)
	if err != nil {
		return nil, err
	}
	account.AssignHandles(accts)
	clear(w.s.dek)
	w.s.env, w.s.dek, w.s.accounts, w.stat = env, dek, accts, info
	return slices.Clone(accts), nil
}

// lock drops the key and the accounts until the next unlock.
func (w *watchVault) lock() {
	w.mu.Lock()
	defer w.mu.Unlock()
	clear(w.s.dek)
	w.s.env, w.s.dek, w.s.accounts = nil, nil, nil
}

func (w *watchVault) mutate(c tui.Change) (tui.Result, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.s.dek == nil {
		return tui.Result{}, tui.ErrKeyReplaced
	}
	var res tui.Result
	var adding []account.Account
	if c.Kind == tui.ChangeAdd {
		var err error
		adding, res.Problems, err = parseWatchAdd(c.Add)
		if err != nil {
			return res, err
		}
	}
	var latest *session
	err := w.s.mutate(func(fresh *session) error {
		latest = fresh
		return applyWatchChange(fresh, c, adding, &res)
	})
	switch {
	case errors.Is(err, errWatchUnchanged):
		w.s.env, w.s.dek, w.s.accounts = latest.env, latest.dek, latest.accounts
	case err != nil:
		return res, watchError(err)
	}
	// Decoding the sealed payload gives the accounts exactly as a later read
	// sees them, so a snapshot taken from them compares equal under the lock.
	if accts, oerr := w.s.env.OpenWithDEK(w.s.dek); oerr == nil {
		account.AssignHandles(accts)
		w.s.accounts = accts
	}
	res.Accounts = slices.Clone(w.s.accounts)
	return res, nil
}

// parseWatchAdd reads the accounts in an otpauth link, migration link or
// setup key. The issuer and account the user typed name a single account.
func parseWatchAdd(add tui.Add) ([]account.Account, []string, error) {
	issuer, acct := account.CleanText(add.Issuer), account.CleanText(add.Account)
	text := strings.TrimSpace(add.Text)
	if detect.Classify(text) == detect.Invalid {
		return nil, nil, errors.New("not an otpauth link or a setup key")
	}
	accts, problems, err := addFromText(text, "input", issuer, acct, "SHA1", 6, 30)
	if err != nil {
		return nil, nil, watchErr{userMessage(err), err}
	}
	reasons := make([]string, 0, len(problems))
	for _, p := range problems {
		reasons = append(reasons, p.source+": "+p.reason)
	}
	if len(accts) == 0 {
		if len(reasons) > 0 {
			return nil, nil, fmt.Errorf("no account found in the input (%s)", strings.Join(reasons, "; "))
		}
		return nil, nil, errors.New("no account found in the input")
	}
	if len(accts) == 1 && issuer != "" {
		accts[0].Issuer = issuer
	}
	if len(accts) == 1 && acct != "" {
		accts[0].Account = acct
	}
	if len(accts) == 1 && isUnnamed(accts[0]) {
		return nil, nil, errors.New("enter an issuer or an account name")
	}
	return accts, reasons, nil
}

// liveIndex finds the live account id in the vault as it is on disk now.
func liveIndex(fresh *session, id string) (int, error) {
	i := indexByID(fresh.accounts, id)
	if i < 0 {
		return -1, errWatchGone
	}
	if fresh.accounts[i].Trashed() {
		return -1, errWatchTrashed
	}
	return i, nil
}

// trashedIndex finds the account id in the Trash as it is on disk now.
func trashedIndex(fresh *session, id string) (int, error) {
	i := indexByID(fresh.accounts, id)
	if i < 0 {
		return -1, errWatchGone
	}
	if !fresh.accounts[i].Trashed() {
		return -1, errors.New("the account is no longer in the Trash")
	}
	return i, nil
}

func applyWatchChange(fresh *session, c tui.Change, adding []account.Account, res *tui.Result) error {
	ts := now().Unix()
	switch c.Kind {
	case tui.ChangeAdd:
		r := mergeAccountsDetailed(fresh, adding)
		res.IDs, res.Skipped = r.added, len(r.skipped)
		for _, p := range r.failed {
			res.Problems = append(res.Problems, p.source+": "+p.reason)
		}
		if len(r.added) == 0 {
			return errWatchUnchanged
		}
	case tui.ChangeEdit:
		return applyWatchEdit(fresh, c.ID, c.Edit)
	case tui.ChangePin:
		i, err := liveIndex(fresh, c.ID)
		if err != nil {
			return err
		}
		if fresh.accounts[i].Pinned == c.Pinned {
			return errWatchUnchanged
		}
		fresh.accounts[i].Pinned, fresh.accounts[i].UpdatedAt = c.Pinned, ts
	case tui.ChangeTrash:
		i, err := liveIndex(fresh, c.ID)
		if err != nil {
			return err
		}
		fresh.accounts[i].DeletedAt, fresh.accounts[i].UpdatedAt = ts, ts
	case tui.ChangeRestore:
		i, err := trashedIndex(fresh, c.ID)
		if err != nil {
			return err
		}
		fresh.accounts[i].DeletedAt, fresh.accounts[i].UpdatedAt = 0, ts
	case tui.ChangePurge:
		if _, err := trashedIndex(fresh, c.ID); err != nil {
			return err
		}
		if err := purgeUnchanged(fresh, []account.Account{c.Snapshot}); err != nil {
			if errors.Is(err, errChangedWhileAsking) {
				return errors.New("the account changed while tess waited for an answer; nothing was deleted")
			}
			return err
		}
	case tui.ChangeAdvance:
		i, err := liveIndex(fresh, c.ID)
		if err != nil {
			return err
		}
		a := &fresh.accounts[i]
		if a.Type != account.HOTP {
			return errors.New("the account no longer uses a counter")
		}
		next, err := account.NextCounter(a.Counter)
		if err != nil {
			return err
		}
		if next != c.Counter {
			return errors.New("the counter changed in another process")
		}
		a.Counter, a.UpdatedAt = next, ts
	default:
		return fmt.Errorf("unknown change %d", c.Kind)
	}
	return nil
}

// applyWatchEdit applies the fields an edit changed, cleaned the way the
// rename, alias, move, tag and note commands clean them.
func applyWatchEdit(fresh *session, id string, e tui.Edit) error {
	i, err := liveIndex(fresh, id)
	if err != nil {
		return err
	}
	before := fresh.accounts[i]
	a := &fresh.accounts[i]
	if e.Alias != nil && strings.ToLower(strings.TrimSpace(*e.Alias)) != a.Handle {
		if err := setHandle(fresh, i, *e.Alias); err != nil {
			return err
		}
	}
	if e.Issuer != nil {
		a.Issuer = account.CleanText(*e.Issuer)
	}
	if e.Account != nil {
		a.Account = account.CleanText(*e.Account)
	}
	if e.Folder != nil {
		a.Folder = account.CleanText(*e.Folder)
	}
	if e.Tags != nil {
		a.Tags = parseWatchTags(*e.Tags)
	}
	if e.Note != nil {
		a.Note = account.CleanNote(*e.Note)
	}
	if (e.Issuer != nil || e.Account != nil) && isUnnamed(*a) {
		return errors.New("enter an issuer or an account name")
	}
	if a.Issuer == before.Issuer && a.Account == before.Account && a.Handle == before.Handle &&
		a.Folder == before.Folder && slices.Equal(a.Tags, before.Tags) && a.Note == before.Note {
		return errWatchUnchanged
	}
	a.UpdatedAt = now().Unix()
	return a.Validate()
}

// parseWatchTags splits comma-separated tags, cleaned, without empties or
// repeats, in the order typed.
func parseWatchTags(text string) []string {
	var tags []string
	for _, part := range strings.Split(text, ",") {
		t := account.CleanText(part)
		if t != "" && !slices.Contains(tags, t) {
			tags = append(tags, t)
		}
	}
	return tags
}
