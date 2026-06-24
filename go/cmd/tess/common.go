package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/otp"
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
	fmt.Fprint(os.Stderr, prompt)
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("read passphrase: %w", err)
	}
	return string(b), nil
}

// openSession resolves the vault path, loads the envelope, and decrypts it.
func openSession() (*session, error) {
	path, err := store.Resolve(vaultPath)
	if err != nil {
		return nil, err
	}
	env, err := store.Load(path)
	if err != nil {
		return nil, err
	}
	pass, err := readPassphrase("Vault passphrase: ")
	if err != nil {
		return nil, err
	}
	accts, err := env.Open(pass)
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
	alg, err := otp.ParseAlgorithm(a.Algorithm)
	if err != nil {
		return "", err
	}
	switch a.Type {
	case account.TOTP:
		return otp.TOTP(a.Secret, t, a.Period, a.Digits, alg)
	case account.Steam:
		return otp.Steam(a.Secret, t)
	case account.HOTP:
		return otp.HOTP(a.Secret, uint64(a.Counter), a.Digits, alg)
	default:
		return "", fmt.Errorf("unknown account type %q", a.Type)
	}
}

func label(a account.Account) string {
	if a.Issuer != "" {
		return a.Issuer + " (" + a.Account + ")"
	}
	return a.Account
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
