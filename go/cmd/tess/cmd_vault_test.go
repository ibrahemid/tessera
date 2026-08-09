package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

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

	var buf bytes.Buffer
	statusCmd := newVaultStatusCmd()
	statusCmd.SetOut(&buf)
	if err := statusCmd.Execute(); err != nil {
		t.Fatalf("status: %v", err)
	}
	got := buf.String()
	for _, want := range []string{"Path:", path, "File:", "bytes", "Wraps:", "passphrase", "Keychain:"} {
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

// TestVaultRememberForget hits the real login keychain; skipped unless
// TESSERA_KEYCHAIN_TEST is set. The account is the temp vault path, so it never
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
