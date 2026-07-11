package main

import (
	"io"
	"path/filepath"
	"testing"

	"github.com/ibrahemid/tessera/go/internal/store"
)

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

func TestVaultResetMissingVaultErrors(t *testing.T) {
	withVault(t)
	resetCmd := newVaultResetCmd()
	resetCmd.SetOut(io.Discard)
	resetCmd.SetArgs([]string{"--force"})
	if err := resetCmd.Execute(); err == nil {
		t.Fatal("reset on missing vault should error")
	}
}
