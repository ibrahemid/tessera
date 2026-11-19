package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ibrahemid/tessera/go/internal/secretstore"
	"github.com/ibrahemid/tessera/go/internal/secretstore/secretstoretest"
	"github.com/ibrahemid/tessera/go/internal/store"
)

// initVault creates a fresh vault at the withVault path.
func initVault(t *testing.T) {
	t.Helper()
	c := newVaultInitCmd()
	c.SetOut(io.Discard)
	if err := c.Execute(); err != nil {
		t.Fatalf("init: %v", err)
	}
}

// withVault points the resolved vault path at a temp file and supplies a
// passphrase for init, restoring the global on cleanup.
func withVault(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "vault.json")
	t.Setenv("TESSERA_PASSPHRASE", "testpass123")
	old := vaultPath
	vaultPath = path
	t.Cleanup(func() { vaultPath = old })
	return path
}

func TestVaultResetForceThenInit(t *testing.T) {
	path := withVault(t)

	initCmd := newVaultInitCmd()
	initCmd.SetOut(io.Discard)
	if err := initCmd.Execute(); err != nil {
		t.Fatalf("init: %v", err)
	}
	if !store.Exists(path) {
		t.Fatal("vault should exist after init")
	}

	resetCmd := newVaultResetCmd()
	resetCmd.SetOut(io.Discard)
	resetCmd.SetArgs([]string{"--force"})
	if err := resetCmd.Execute(); err != nil {
		t.Fatalf("reset --force: %v", err)
	}
	if store.Exists(path) {
		t.Fatal("vault file should be gone after reset")
	}

	initAgain := newVaultInitCmd()
	initAgain.SetOut(io.Discard)
	if err := initAgain.Execute(); err != nil {
		t.Fatalf("init after reset: %v", err)
	}
	if !store.Exists(path) {
		t.Fatal("vault should exist after re-init")
	}
}

func TestVaultStatusShowsWrapsWithoutPassphrase(t *testing.T) {
	path := withVault(t)
	initVault(t)
	swapStores(t, secretService())
	label := "Secrets:"
	if isKeychainWording {
		label = "Keychain:"
	}

	var buf bytes.Buffer
	statusCmd := newVaultStatusCmd()
	statusCmd.SetOut(&buf)
	if err := statusCmd.Execute(); err != nil {
		t.Fatalf("status: %v", err)
	}
	got := buf.String()
	for _, want := range []string{"Path:", path, "File:", "bytes", "Wraps:", "passphrase", label} {
		if !strings.Contains(got, want) {
			t.Fatalf("status output missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "testpass123") {
		t.Fatalf("status must never print the passphrase:\n%s", got)
	}
}

func TestVaultStatusMissingFile(t *testing.T) {
	withVault(t)
	swapStores(t, secretService())
	var buf bytes.Buffer
	statusCmd := newVaultStatusCmd()
	statusCmd.SetOut(&buf)
	if err := statusCmd.Execute(); err != nil {
		t.Fatalf("status on missing vault: %v", err)
	}
	if !strings.Contains(buf.String(), "not found") {
		t.Fatalf("expected 'not found':\n%s", buf.String())
	}
}

// TestVaultRememberForget hits this system's real secret store; skipped unless
// TESSERA_KEYCHAIN_TEST is set. The key is the temp vault path, so it never
// collides with the user's real entry, and forget cleans it up.
func TestVaultRememberForget(t *testing.T) {
	if os.Getenv("TESSERA_KEYCHAIN_TEST") == "" {
		t.Skip("set TESSERA_KEYCHAIN_TEST=1 to run the keychain CLI test")
	}
	path := withVault(t)
	initVault(t)
	t.Cleanup(func() {
		fc := newVaultForgetCmd()
		fc.SetOut(io.Discard)
		_ = fc.Execute()
	})

	rc := newVaultRememberCmd()
	rc.SetOut(io.Discard)
	if err := rc.Execute(); err != nil {
		t.Fatalf("remember: %v", err)
	}

	var buf bytes.Buffer
	sc := newVaultStatusCmd()
	sc.SetOut(&buf)
	if err := sc.Execute(); err != nil {
		t.Fatalf("status: %v", err)
	}
	if !strings.Contains(buf.String(), "entry present") {
		t.Fatalf("expected keychain entry present:\n%s", buf.String())
	}

	fc := newVaultForgetCmd()
	fc.SetOut(io.Discard)
	if err := fc.Execute(); err != nil {
		t.Fatalf("forget: %v", err)
	}
	buf.Reset()
	sc2 := newVaultStatusCmd()
	sc2.SetOut(&buf)
	if err := sc2.Execute(); err != nil {
		t.Fatalf("status after forget: %v", err)
	}
	if !strings.Contains(buf.String(), "no entry") {
		t.Fatalf("expected no keychain entry after forget:\n%s", buf.String())
	}
	_ = path
}

// runPasswd executes `vault passwd` and returns its error.
func runPasswd(t *testing.T) error {
	t.Helper()
	swapStores(t, secretService())
	c := newVaultPasswdCmd()
	c.SetOut(io.Discard)
	return c.Execute()
}

// TestVaultPasswdRefusesToReuseTheCurrentPassphrase is the regression test for
// the silent no-op: with only $TESSERA_PASSPHRASE set, the replacement used to
// be sourced from the same variable, so the command reported success while
// changing nothing and skipping both the confirmation and the length floor.
func TestVaultPasswdRefusesToReuseTheCurrentPassphrase(t *testing.T) {
	path := withVault(t) // TESSERA_PASSPHRASE=testpass123
	initVault(t)

	err := runPasswd(t)
	if err == nil {
		t.Fatal("passwd with no new-passphrase source must fail, not silently no-op")
	}
	if !strings.Contains(err.Error(), "TESSERA_NEW_PASSPHRASE") {
		t.Errorf("error should name the scripted source, got %v", err)
	}
	env, lerr := store.Load(path)
	if lerr != nil {
		t.Fatal(lerr)
	}
	if _, oerr := env.Open("testpass123"); oerr != nil {
		t.Errorf("the original passphrase must still open the vault: %v", oerr)
	}
}

func TestVaultPasswdUsesNewPassphraseEnv(t *testing.T) {
	path := withVault(t)
	initVault(t)
	t.Setenv("TESSERA_NEW_PASSPHRASE", "brand-new-pass")

	if err := runPasswd(t); err != nil {
		t.Fatalf("passwd: %v", err)
	}
	env, err := store.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.Open("testpass123"); err == nil {
		t.Error("the old passphrase must stop working")
	}
	if _, err := env.Open("brand-new-pass"); err != nil {
		t.Errorf("the new passphrase must open the vault: %v", err)
	}
}

func TestVaultPasswdEnforcesLengthFloor(t *testing.T) {
	path := withVault(t)
	initVault(t)
	t.Setenv("TESSERA_NEW_PASSPHRASE", "short")

	if err := runPasswd(t); err == nil {
		t.Fatal("a 5-character replacement must be rejected")
	}
	env, err := store.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.Open("testpass123"); err != nil {
		t.Errorf("a rejected change must leave the vault alone: %v", err)
	}
}

func TestVaultResetMissingVaultErrors(t *testing.T) {
	withVault(t)
	resetCmd := newVaultResetCmd()
	resetCmd.SetOut(io.Discard)
	resetCmd.SetArgs([]string{"--force"})
	if err := resetCmd.Execute(); err == nil {
		t.Fatal("reset on missing vault should error")
	}
}

// stubKeychainWording forces the macOS keychain wording on or off.
func stubKeychainWording(t *testing.T, on bool) {
	t.Helper()
	old := isKeychainWording
	isKeychainWording = on
	t.Cleanup(func() { isKeychainWording = old })
}

func kernelKeyring() *secretstoretest.Fake {
	return &secretstoretest.Fake{BackendID: "kernel-keyring", BackendName: "kernel keyring", TTLSupported: true}
}

// failingStore is a secret store whose presence check fails with err.
type failingStore struct {
	*secretstoretest.Fake
	err error
}

func (f failingStore) Has(context.Context, string) (bool, error) { return false, f.err }

func unavailable(f *secretstoretest.Fake, reason string) *secretstoretest.Fake {
	f.AvailableErr = &secretstore.UnavailableError{Backend: f.BackendName, Reason: reason}
	return f
}

func TestSecretStatusLineNamesEachStore(t *testing.T) {
	const path = "/v/vault.json"
	queryErr := errors.New("secretstore: could not query the Secret Service; unlock it and retry")
	cases := []struct {
		name   string
		stores func() []secretstore.Backend
		want   string
	}{
		{"present in two stores", func() []secretstore.Backend {
			a, b := secretService(), kernelKeyring()
			a.Put(path, "p")
			b.Put(path, "p")
			return []secretstore.Backend{a, b}
		}, "Secrets:  entry present (Secret Service, kernel keyring)"},
		{"locked", func() []secretstore.Backend {
			a := secretService()
			a.Put(path, "p")
			a.Lock(path)
			return []secretstore.Backend{a, kernelKeyring()}
		}, "Secrets:  locked (Secret Service)"},
		{"no entry", func() []secretstore.Backend {
			return []secretstore.Backend{secretService(), kernelKeyring()}
		}, "Secrets:  no entry (stores in Secret Service)"},
		{"no entry with a fallback", func() []secretstore.Backend {
			return []secretstore.Backend{unavailable(secretService(), "no session bus"), kernelKeyring()}
		}, "Secrets:  no entry (stores in kernel keyring)"},
		{"query failure", func() []secretstore.Backend {
			return []secretstore.Backend{failingStore{secretService(), queryErr}}
		}, "Secrets:  unknown (could not query the Secret Service; unlock it and retry)"},
		{"every store unavailable", func() []secretstore.Backend {
			return []secretstore.Backend{
				unavailable(secretService(), "no session bus"),
				unavailable(kernelKeyring(), "keyctl is blocked"),
			}
		}, "Secrets:  unavailable: Secret Service: no session bus; kernel keyring: keyctl is blocked; set TESSERA_PASSPHRASE for scripts"},
		{"no stores", func() []secretstore.Backend { return nil },
			"Secrets:  unavailable: no secret store on this system; set TESSERA_PASSPHRASE for scripts"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			swapStores(t, c.stores()...)
			if got := secretStatusLine(context.Background(), path, false); got != c.want {
				t.Errorf("status = %q, want %q", got, c.want)
			}
		})
	}
}

func TestSecretStatusLineKeepsTheKeychainWording(t *testing.T) {
	const path = "/v/vault.json"
	keychain := func() *secretstoretest.Fake {
		return &secretstoretest.Fake{BackendID: "keychain", BackendName: "login keychain"}
	}
	queryErr := errors.New("secretstore: could not query the login keychain; unlock the login keychain and retry")
	cases := []struct {
		name  string
		store func() secretstore.Backend
		want  string
	}{
		{"present", func() secretstore.Backend {
			k := keychain()
			k.Put(path, "p")
			return k
		}, "Keychain: entry present"},
		{"no entry", func() secretstore.Backend { return keychain() }, "Keychain: no entry"},
		{"query failure", func() secretstore.Backend { return failingStore{keychain(), queryErr} },
			"Keychain: unknown (could not query the login keychain; unlock the login keychain and retry)"},
		{"locked", func() secretstore.Backend {
			k := keychain()
			k.Put(path, "p")
			k.Lock(path)
			return k
		}, "Keychain: unknown (the secret store is locked; unlock it and retry)"},
		{"unavailable", func() secretstore.Backend { return unavailable(keychain(), "/usr/bin/security not found") },
			"Keychain: unknown (no secret store is available (login keychain: /usr/bin/security not found); set TESSERA_PASSPHRASE to unlock without one)"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			swapStores(t, c.store())
			if got := secretStatusLine(context.Background(), path, true); got != c.want {
				t.Errorf("status = %q, want %q", got, c.want)
			}
		})
	}
}

func TestVaultStatusPrintsTheSecretsLine(t *testing.T) {
	path := withVault(t)
	stubKeychainWording(t, false)
	store := secretService()
	store.Put(path, "testpass123")
	swapStores(t, store)

	got, err := runRoot(t, "vault", "status")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	want := "Path:     " + path + "\nFile:     not found\nSecrets:  entry present (Secret Service)\n"
	if got != want {
		t.Fatalf("status = %q, want %q", got, want)
	}
}

func TestVaultRememberRejectsABadDuration(t *testing.T) {
	for _, arg := range []string{"-1h", "soon"} {
		t.Run(arg, func(t *testing.T) {
			withVault(t)
			initVault(t)
			store := kernelKeyring()
			swapStores(t, store)

			_, err := runRoot(t, "vault", "remember", "--for", arg)
			if err == nil {
				t.Fatalf("--for %s was accepted", arg)
			}
			if arg == "-1h" && !errors.Is(err, errNegativeTTL) {
				t.Fatalf("err = %v, want the negative duration error", err)
			}
			if len(store.Calls()) != 0 {
				t.Fatalf("a refused --for still used the store: %v", store.Calls())
			}
		})
	}
}

func TestVaultRememberNamesTheStore(t *testing.T) {
	at := time.Date(2026, 3, 4, 10, 30, 0, 0, time.Local)
	cases := []struct {
		name  string
		store *secretstoretest.Fake
		args  []string
		want  string
		ttl   time.Duration
	}{
		{"secret service", secretService(), nil, "in the Secret Service\n", 0},
		{"kernel keyring", kernelKeyring(), nil, "in the kernel keyring until reboot\n", 0},
		{"kernel keyring with expiry", kernelKeyring(), []string{"--for", "8h"}, "in the kernel keyring until 2026-03-04 18:30\n", 8 * time.Hour},
		{"credential manager", &secretstoretest.Fake{BackendID: "credential-manager", BackendName: "Windows Credential Manager"}, nil,
			"in the Windows Credential Manager\n", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := withVault(t)
			initVault(t)
			stubClock(t, at)
			swapStores(t, c.store)

			got, err := runRoot(t, append([]string{"vault", "remember"}, c.args...)...)
			if err != nil {
				t.Fatalf("remember: %v", err)
			}
			if want := "Stored passphrase for " + path + " " + c.want; got != want {
				t.Fatalf("output = %q, want %q", got, want)
			}
			if secret, ok := c.store.Secret(path); !ok || secret != "testpass123" {
				t.Fatalf("stored = %q, %v", secret, ok)
			}
			if ttl := c.store.TTL(path); ttl != c.ttl {
				t.Fatalf("ttl = %v, want %v", ttl, c.ttl)
			}
		})
	}
}

func TestVaultRememberPassesExpiryRefusalsThrough(t *testing.T) {
	path := withVault(t)
	initVault(t)
	store := secretService()
	swapStores(t, store)

	_, err := runRoot(t, "vault", "remember", "--for", "8h")
	if !errors.Is(err, secretstore.ErrTTLUnsupported) {
		t.Fatalf("err = %v, want the store's expiry refusal", err)
	}
	if got := userMessage(err); got != "expiry is not supported by the Secret Service; store without --for" {
		t.Fatalf("message = %q", got)
	}
	if _, ok := store.Secret(path); ok {
		t.Fatal("a refused expiry still stored the passphrase")
	}
}

func TestVaultRememberWithoutAStore(t *testing.T) {
	withVault(t)
	initVault(t)
	swapStores(t, unavailable(secretService(), "no session bus"))

	_, err := runRoot(t, "vault", "remember")
	var none *secretstore.NoBackendError
	if !errors.As(err, &none) {
		t.Fatalf("err = %v, want the no-store error", err)
	}
}

func TestVaultRememberRefusesAWrongPassphrase(t *testing.T) {
	path := withVault(t)
	initVault(t)
	store := secretService()
	swapStores(t, store)
	t.Setenv("TESSERA_PASSPHRASE", "not-the-passphrase")

	_, err := runRoot(t, "vault", "remember")
	if got := userMessage(err); got != "the passphrase in $TESSERA_PASSPHRASE doesn't open "+path {
		t.Fatalf("message = %q", got)
	}
	if writesTo(store) != 0 {
		t.Fatalf("a wrong passphrase was stored: %v", store.Calls())
	}
}

func TestVaultRememberOffATerminalNamesTheVariable(t *testing.T) {
	withVault(t)
	initVault(t)
	swapStores(t, secretService())
	t.Setenv("TESSERA_PASSPHRASE", "")

	if _, err := runRoot(t, "vault", "remember"); !errors.Is(err, errNoUnlockTerminal) {
		t.Fatalf("err = %v, want the terminal error", err)
	}
}

func TestVaultForgetClearsEveryStore(t *testing.T) {
	path := withVault(t)
	stubKeychainWording(t, false)
	a, b := secretService(), kernelKeyring()
	a.Put(path, "testpass123")
	b.Put(path, "testpass123")
	b.Lock(path)
	swapStores(t, a, b)

	got, err := runRoot(t, "vault", "forget")
	if err != nil {
		t.Fatalf("forget: %v", err)
	}
	want := "Removed the stored passphrase for " + path + " from the Secret Service\n" +
		"Removed the stored passphrase for " + path + " from the kernel keyring\n"
	if got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
	for _, f := range []*secretstoretest.Fake{a, b} {
		if _, ok := f.Secret(path); ok {
			t.Fatalf("the %s still holds the passphrase", f.BackendName)
		}
	}

	got, err = runRoot(t, "vault", "forget")
	if err != nil || got != "No stored passphrase for "+path+"\n" {
		t.Fatalf("second forget = %q, %v", got, err)
	}
}

func TestVaultForgetKeepsTheKeychainWording(t *testing.T) {
	path := withVault(t)
	stubKeychainWording(t, true)
	k := &secretstoretest.Fake{BackendID: "keychain", BackendName: "login keychain"}
	k.Put(path, "testpass123")
	swapStores(t, k)

	got, err := runRoot(t, "vault", "forget")
	if err != nil || got != "Removed any keychain entry for "+path+"\n" {
		t.Fatalf("forget = %q, %v", got, err)
	}
	if _, ok := k.Secret(path); ok {
		t.Fatal("the keychain still holds the passphrase")
	}
}
