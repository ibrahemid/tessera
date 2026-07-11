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
	"github.com/ibrahemid/tessera/go/internal/vault"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// session bundles an opened vault for read/modify/save flows.
type session struct {
	path       string
	env        *vault.Envelope
	passphrase string
	accounts   []account.Account
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

// openSession resolves the vault path, loads the envelope, and decrypts it,
// resolving the passphrase from env, the login keychain, or an interactive prompt.
func openSession() (*session, error) {
	path, err := store.Resolve(vaultPath)
	if err != nil {
		return nil, err
	}
	env, err := store.Load(path)
	if err != nil {
		return nil, err
	}
	isTTY := term.IsTerminal(int(os.Stdin.Fd()))
	pass, accts, err := resolvePassphrase(unlockProvider{
		envPass:     os.Getenv("TESSERA_PASSPHRASE"),
		lookup:      func() (string, bool, error) { return keychain.Lookup(path) },
		store:       func(p string) error { return keychain.Store(path, p) },
		prompt:      promptPassphrase,
		confirm:     confirmYN,
		tryOpen:     env.Open,
		canRemember: keychain.Supported() && isTTY,
	})
	if err != nil {
		return nil, err
	}
	return &session{path: path, env: env, passphrase: pass, accounts: accts}, nil
}

// save re-seals the (possibly mutated) account list and persists the vault.
func (s *session) save() error {
	if err := s.env.UpdateAccounts(s.passphrase, s.accounts); err != nil {
		return err
	}
	return store.Save(s.path, s.env)
}

// find returns the index of the single account matching query (by id, or
// case-insensitive substring of "issuer:account"), erroring on none/ambiguous.
func (s *session) find(query string) (int, error) {
	q := strings.ToLower(query)
	var matches []int
	for i, a := range s.accounts {
		if a.ID == query {
			return i, nil
		}
		label := strings.ToLower(a.Issuer + ":" + a.Account)
		if strings.Contains(label, q) {
			matches = append(matches, i)
		}
	}
	switch len(matches) {
	case 0:
		return -1, fmt.Errorf("no account matches %q", query)
	case 1:
		return matches[0], nil
	default:
		return -1, fmt.Errorf("%q is ambiguous (%d matches); use the account id", query, len(matches))
	}
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

// shortQuery is a convenient query string for an account in hints.
func shortQuery(a account.Account) string { return labelText(a) }

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

func now() time.Time { return time.Now() }

// requireNewVaultPassphrase prompts twice and confirms a match.
func requireNewVaultPassphrase() (string, error) {
	if p := os.Getenv("TESSERA_PASSPHRASE"); p != "" {
		return p, nil
	}
	p1, err := readPassphrase("New vault passphrase: ")
	if err != nil {
		return "", err
	}
	p2, err := readPassphrase("Confirm passphrase: ")
	if err != nil {
		return "", err
	}
	if p1 != p2 {
		return "", fmt.Errorf("passphrases do not match")
	}
	if len(p1) < 8 {
		return "", fmt.Errorf("passphrase must be at least 8 characters")
	}
	return p1, nil
}

// out writes a line to the command's stdout.
func out(cmd *cobra.Command, format string, a ...any) {
	fmt.Fprintf(cmd.OutOrStdout(), format+"\n", a...)
}
