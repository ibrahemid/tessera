package main

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/code"
	"github.com/ibrahemid/tessera/go/internal/keychain"
	"github.com/ibrahemid/tessera/go/internal/store"
	"github.com/ibrahemid/tessera/go/internal/ui"
	"github.com/ibrahemid/tessera/go/internal/vault"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// session bundles an opened vault for read/modify/save flows. It holds the
// vault lock from the moment the envelope is read until close, so the whole
// load -> mutate -> save cycle is serialized against other tess processes. dek
// is the unwrapped Data Encryption Key, cached so a mutating command runs
// argon2id once instead of once per write.
type session struct {
	path       string
	env        *vault.Envelope
	passphrase string
	dek        []byte
	accounts   []account.Account
	lock       *store.Lock
}

// readPassphrase returns $TESSERA_PASSPHRASE if set, else prompts without echo.
func readPassphrase(prompt string) (string, error) {
	if p := os.Getenv("TESSERA_PASSPHRASE"); p != "" {
		return p, nil
	}
	return promptPassphrase(prompt)
}

// promptPassphrase reads a passphrase from the terminal without echo, ignoring
// $TESSERA_PASSPHRASE (env resolution is handled by the caller).
func promptPassphrase(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("read passphrase: %w", err)
	}
	return string(b), nil
}

// confirmYN prints prompt and returns true only for an explicit y/yes.
func confirmYN(prompt string) bool {
	fmt.Fprint(os.Stderr, prompt)
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	s := strings.ToLower(strings.TrimSpace(line))
	return s == "y" || s == "yes"
}

// unlockProvider supplies the side effects of passphrase resolution so the
// ordering logic in resolvePassphrase stays pure and unit-testable.
type unlockProvider struct {
	envPass     string                              // $TESSERA_PASSPHRASE ("" if unset)
	lookup      func() (string, bool, error)        // silent keychain lookup for this vault
	store       func(pass string) error             // persist pass to the keychain
	prompt      func(prompt string) (string, error) // interactive passphrase prompt
	confirm     func(prompt string) bool            // y/N prompt
	tryOpen     func(pass string) ([]account.Account, error)
	canRemember bool // keychain available AND interactive: only then may we offer to store
}

// resolvePassphrase applies the unlock order: $TESSERA_PASSPHRASE, then a silent
// keychain lookup, then an interactive prompt. After a successful interactive
// unlock (and only then) it may offer to remember the passphrase. It returns the
// passphrase and the opened accounts so the vault is decrypted exactly once.
func resolvePassphrase(p unlockProvider) (string, []account.Account, error) {
	if p.envPass != "" {
		accts, err := p.tryOpen(p.envPass)
		if err != nil {
			return "", nil, err
		}
		return p.envPass, accts, nil
	}

	stale := false // a keychain passphrase that no longer opens the vault (rekeyed)
	if kp, hit, err := p.lookup(); err == nil && hit {
		if accts, oerr := p.tryOpen(kp); oerr == nil {
			return kp, accts, nil
		}
		stale = true
	}

	entered, err := p.prompt("Vault passphrase: ")
	if err != nil {
		return "", nil, err
	}
	accts, err := p.tryOpen(entered)
	if err != nil {
		return "", nil, err
	}
	if p.canRemember {
		q := "Remember in the login keychain so tess stops prompting? [y/N] "
		if stale {
			q = "Stored passphrase no longer opens the vault. Update the keychain entry? [y/N] "
		}
		if p.confirm(q) {
			if serr := p.store(entered); serr != nil {
				fmt.Fprintln(os.Stderr, "warning:", serr)
			}
		}
	}
	return entered, accts, nil
}

// openSession resolves the vault path, takes the vault lock, loads the envelope
// and decrypts it, resolving the passphrase from env, the login keychain, or an
// interactive prompt. Every caller must close the returned session.
func openSession() (*session, error) {
	path, err := store.Resolve(vaultPath)
	if err != nil {
		return nil, err
	}
	lock, err := store.Acquire(path)
	if err != nil {
		return nil, err
	}
	s, err := loadSession(path)
	if err != nil {
		_ = lock.Release()
		return nil, err
	}
	s.lock = lock
	return s, nil
}

// loadSession reads and decrypts the vault at path. The caller owns the lock.
func loadSession(path string) (*session, error) {
	env, err := store.Load(path)
	if err != nil {
		return nil, err
	}
	isTTY := term.IsTerminal(int(os.Stdin.Fd()))
	var dek []byte
	pass, accts, err := resolvePassphrase(unlockProvider{
		envPass: os.Getenv("TESSERA_PASSPHRASE"),
		lookup:  func() (string, bool, error) { return keychain.Lookup(path) },
		store:   func(p string) error { return keychain.Store(path, p) },
		prompt:  promptPassphrase,
		confirm: confirmYN,
		tryOpen: func(p string) ([]account.Account, error) {
			d, err := env.UnwrapDEK(p)
			if err != nil {
				return nil, err
			}
			a, err := env.OpenWithDEK(d)
			if err != nil {
				return nil, err
			}
			dek = d
			return a, nil
		},
		canRemember: keychain.Supported() && isTTY,
	})
	if err != nil {
		return nil, err
	}
	s := &session{path: path, env: env, passphrase: pass, dek: dek, accounts: accts}
	// Migration: accounts written before handles existed gain one deterministically
	// at unlock. An argon2 params.v upgrade performed during the unwrap rides the
	// same write. Persist once, atomically (wraps otherwise untouched). A
	// read-only vault keeps both in memory only; the next writable unlock redoes
	// them from the same deterministic inputs.
	if account.AssignHandles(s.accounts) || env.NeedsPersist() {
		if err := env.UpdateAccountsWithDEK(dek, s.accounts); err == nil {
			_ = store.Save(path, env)
		}
	}
	return s, nil
}

// close releases the vault lock. Safe on a nil session and safe to call twice.
func (s *session) close() {
	if s == nil {
		return
	}
	if err := s.lock.Release(); err != nil {
		fmt.Fprintln(os.Stderr, "warning:", err)
	}
	s.lock = nil
}

// mutate performs a locked read-modify-write against the vault as it is on disk
// right now. Long-running views (`tess watch`) release the lock while idle, so
// their in-memory account list can be stale by the time the user advances an
// HOTP counter; re-reading first keeps another process's edits from being
// re-sealed away.
func (s *session) mutate(fn func(*session) error) error {
	lock, err := store.Acquire(s.path)
	if err != nil {
		return err
	}
	defer lock.Release()

	env, err := store.Load(s.path)
	if err != nil {
		return err
	}
	dek := s.dek
	accts, err := env.OpenWithDEK(dek)
	if err != nil {
		// The vault was re-keyed while the view was idle: derive again.
		dek, err = env.UnwrapDEK(s.passphrase)
		if err != nil {
			return err
		}
		accts, err = env.OpenWithDEK(dek)
		if err != nil {
			return err
		}
	}
	account.AssignHandles(accts)
	fresh := &session{path: s.path, env: env, passphrase: s.passphrase, dek: dek, accounts: accts}
	if err := fn(fresh); err != nil {
		return err
	}
	if err := fresh.save(); err != nil {
		return err
	}
	s.env, s.dek, s.accounts = fresh.env, fresh.dek, fresh.accounts
	return nil
}

// save assigns handles to any new accounts, then re-seals the (possibly mutated)
// account list and persists the vault. The caller must hold the vault lock.
func (s *session) save() error {
	account.AssignHandles(s.accounts)
	if err := account.CheckHandleUniqueness(s.accounts); err != nil {
		return err
	}
	if err := s.env.UpdateAccountsWithDEK(s.dek, s.accounts); err != nil {
		return err
	}
	return store.Save(s.path, s.env)
}

// indexByID returns the position of the account with the given id, or -1.
func indexByID(accts []account.Account, id string) int {
	for i := range accts {
		if accts[i].ID == id {
			return i
		}
	}
	return -1
}

// resolveMatches applies the spec account-resolution precedence and returns the
// indices matched by the first stage that matches anything: exact handle, then
// exact issuer/account (case-insensitive), then unique substring over
// handle/issuer/account. A single index is a unique resolution; more than one
// means the caller must disambiguate. Zero total matches is an error.
func (s *session) resolveMatches(query string) ([]int, error) {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return nil, fmt.Errorf("empty query")
	}
	var stage []int
	for i, a := range s.accounts {
		if a.Handle != "" && a.Handle == q {
			stage = append(stage, i)
		}
	}
	if len(stage) > 0 {
		return stage, nil
	}
	for i, a := range s.accounts {
		if strings.ToLower(a.Issuer) == q || strings.ToLower(a.Account) == q {
			stage = append(stage, i)
		}
	}
	if len(stage) > 0 {
		return stage, nil
	}
	for i, a := range s.accounts {
		hay := strings.ToLower(a.Handle + " " + a.Issuer + " " + a.Account)
		if strings.Contains(hay, q) {
			stage = append(stage, i)
		}
	}
	if len(stage) > 0 {
		return stage, nil
	}
	return nil, fmt.Errorf("no account matches %q", query)
}

// single resolves query to exactly one account index. On multiple matches it
// prints the disambiguation table and returns an error so the command exits
// non-zero (it cannot act without a unique target).
func (s *session) single(cmd *cobra.Command, query string) (int, error) {
	idxs, err := s.resolveMatches(query)
	if err != nil {
		return -1, err
	}
	if len(idxs) > 1 {
		printMatchTable(cmd, s.accounts, idxs, false)
		return -1, fmt.Errorf("be specific: use a handle")
	}
	return idxs[0], nil
}

// printMatchTable renders the ambiguous matches as an aligned handle/issuer/
// account table so the user can pick a unique reference. With withCodes it adds
// each account's current code (used by `tess code` without --copy). The handle
// column is never truncated: it is the string the user types.
func printMatchTable(cmd *cobra.Command, accts []account.Account, idxs []int, withCodes bool) {
	hw, iw, aw := len("handle"), len("issuer"), len("account")
	for _, i := range idxs {
		hw = max(hw, len(accts[i].Handle))
		iw = max(iw, len([]rune(accts[i].Issuer)))
		aw = max(aw, len([]rune(accts[i].Account)))
	}
	header := fmt.Sprintf("%s  %s  %s", padRight("handle", hw), padRight("issuer", iw), "account")
	if withCodes {
		header = fmt.Sprintf("%s  %s  %s  %s", padRight("handle", hw), padRight("issuer", iw), padRight("account", aw), "code")
	}
	out(cmd, "%s", ui.SubtleStyle.Render(header))
	t := now()
	for _, i := range idxs {
		a := accts[i]
		h := ui.Handle(a.Issuer+a.Account, padRight(a.Handle, hw))
		if withCodes {
			c, err := genCode(a, t)
			if err != nil {
				c = "------"
			}
			out(cmd, "%s  %s  %s  %s", h, padRight(a.Issuer, iw), padRight(a.Account, aw), ui.CodeStyle.Render(ui.GroupCode(c)))
			continue
		}
		out(cmd, "%s  %s  %s", h, padRight(a.Issuer, iw), a.Account)
	}
}

// setHandle validates and assigns a new handle to account idx (charset +
// vault-uniqueness), freeing the old value. It does not persist.
func setHandle(s *session, idx int, newHandle string) error {
	h := strings.ToLower(strings.TrimSpace(newHandle))
	if !account.ValidHandle(h) {
		return fmt.Errorf("invalid handle %q: 1-12 chars, lowercase letter first, then letters/digits", newHandle)
	}
	for i, a := range s.accounts {
		if i != idx && a.Handle == h {
			return fmt.Errorf("handle %q already used by %s", h, label(a))
		}
	}
	s.accounts[idx].Handle = h
	s.accounts[idx].UpdatedAt = now().Unix()
	return nil
}

// genCode computes the current code for an account at time t.
func genCode(a account.Account, t time.Time) (string, error) {
	return code.For(a, t)
}

func label(a account.Account) string {
	if a.Issuer != "" {
		return a.Issuer + " (" + a.Account + ")"
	}
	return a.Account
}

// labelText is the primary display name (issuer, else account).
func labelText(a account.Account) string {
	if a.Issuer != "" {
		return a.Issuer
	}
	return a.Account
}

// handleWidth returns the widest handle among accts, so the handle column can
// be padded without ever truncating the string the user types.
func handleWidth(accts []account.Account) int {
	w := 0
	for _, a := range accts {
		if len(a.Handle) > w {
			w = len(a.Handle)
		}
	}
	return w
}

// padRight pads or ellipsizes s to n runes.
func padRight(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s + strings.Repeat(" ", n-len(r))
}

func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// now is the clock, replaceable in tests.
var now = time.Now

// minPassphraseLen is the floor for a passphrase the user chooses.
const minPassphraseLen = 8

// requireNewVaultPassphrase prompts twice and confirms a match. `vault init`
// has no current passphrase, so $TESSERA_PASSPHRASE is the scripted source.
func requireNewVaultPassphrase() (string, error) {
	if p := os.Getenv("TESSERA_PASSPHRASE"); p != "" {
		return p, nil
	}
	return promptNewPassphrase()
}

// requireChangedVaultPassphrase sources the REPLACEMENT passphrase for `tess
// vault passwd`. It never reads $TESSERA_PASSPHRASE: that variable holds the
// current passphrase, and taking it here would turn the change into a silent
// no-op that also skipped the confirmation and the length floor. Scripts set
// $TESSERA_NEW_PASSPHRASE; otherwise a terminal is required.
func requireChangedVaultPassphrase() (string, error) {
	if p := os.Getenv("TESSERA_NEW_PASSPHRASE"); p != "" {
		if len(p) < minPassphraseLen {
			return "", fmt.Errorf("passphrase must be at least %d characters", minPassphraseLen)
		}
		return p, nil
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", fmt.Errorf("set $TESSERA_NEW_PASSPHRASE or run on a terminal to change the passphrase")
	}
	return promptNewPassphrase()
}

// promptNewPassphrase reads a passphrase twice from the terminal and checks the
// two entries match and clear the length floor.
func promptNewPassphrase() (string, error) {
	p1, err := promptPassphrase("New vault passphrase: ")
	if err != nil {
		return "", err
	}
	p2, err := promptPassphrase("Confirm passphrase: ")
	if err != nil {
		return "", err
	}
	if p1 != p2 {
		return "", fmt.Errorf("passphrases do not match")
	}
	if len(p1) < minPassphraseLen {
		return "", fmt.Errorf("passphrase must be at least %d characters", minPassphraseLen)
	}
	return p1, nil
}

// out writes a line to the command's stdout.
func out(cmd *cobra.Command, format string, a ...any) {
	fmt.Fprintf(cmd.OutOrStdout(), format+"\n", a...)
}

// errOut writes a line to the command's stderr, for progress notes that must
// stay out of piped output.
func errOut(cmd *cobra.Command, format string, a ...any) {
	fmt.Fprintf(cmd.ErrOrStderr(), format+"\n", a...)
}
