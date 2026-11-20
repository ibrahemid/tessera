package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"testing"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/procenv"
	"github.com/ibrahemid/tessera/go/internal/store"
)

// TestVaultEditorProcess is the second tess process of the prompt tests: it
// takes the vault lock without waiting and adds one account.
func TestVaultEditorProcess(t *testing.T) {
	path := os.Getenv("TESS_EDITOR_VAULT")
	if path == "" {
		t.Skip("runs only as a child of the prompt tests")
	}
	if err := addUnderTryLock(path, os.Getenv("TESS_EDITOR_PASS")); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(3)
	}
}

func addUnderTryLock(path, pass string) error {
	lock, err := store.TryAcquire(path)
	if err != nil {
		return fmt.Errorf("take the lock: %w", err)
	}
	defer lock.Release()
	env, err := store.Load(path)
	if err != nil {
		return err
	}
	dek, err := env.UnwrapDEK(pass)
	if err != nil {
		return err
	}
	accts, err := env.OpenWithDEK(dek)
	if err != nil {
		return err
	}
	accts = append(accts, totp("other", "Other", "o", 2))
	account.AssignHandles(accts)
	if err := env.UpdateAccountsWithDEK(dek, accts); err != nil {
		return err
	}
	return store.Save(path, env)
}

func runVaultEditor(path, pass string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, "-test.run=^TestVaultEditorProcess$", "-test.count=1")
	cmd.Env = append(procenv.Child(), "TESS_EDITOR_VAULT="+path, "TESS_EDITOR_PASS="+pass)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%w: %s", err, out)
	}
	return nil
}

func TestUnlockPromptLeavesTheVaultLockFree(t *testing.T) {
	path := withVault(t)
	sealVault(t, path, []account.Account{totp("a", "ACME", "x", 1)})
	t.Setenv("TESSERA_PASSPHRASE", "")
	swapStores(t)
	stubStdinTTY(t, true)
	stubPrompts(t, nil, []string{"n"})
	var editErr error
	prompts := 0
	old := promptVaultPassphrase
	promptVaultPassphrase = func(string) (string, error) {
		prompts++
		editErr = runVaultEditor(path, "testpass123")
		return "testpass123", nil
	}
	t.Cleanup(func() { promptVaultPassphrase = old })

	if _, err := runRoot(t, "add", "otpauth://totp/Mine:me?secret=JBSWY3DPEHPK3PXP&issuer=Mine"); err != nil {
		t.Fatalf("add: %v", err)
	}
	if prompts != 1 {
		t.Fatalf("%d passphrase prompts, want 1", prompts)
	}
	if editErr != nil {
		t.Fatalf("a second process could not use the vault during the prompt: %v", editErr)
	}
	got := map[string]bool{}
	for _, a := range openVaultFile(t, path, "testpass123") {
		got[a.Issuer] = true
	}
	for _, issuer := range []string{"ACME", "Other", "Mine"} {
		if !got[issuer] {
			t.Errorf("the vault lacks %s after both writes: %v", issuer, got)
		}
	}
}

func TestUnlockRefusesAPassphraseChangedDuringThePrompt(t *testing.T) {
	path := withVault(t)
	sealVault(t, path, []account.Account{totp("a", "ACME", "x", 1)})
	t.Setenv("TESSERA_PASSPHRASE", "")
	swapStores(t)
	stubStdinTTY(t, true)
	stubPrompts(t, nil, nil)
	old := promptVaultPassphrase
	promptVaultPassphrase = func(string) (string, error) {
		sealAt(t, path, "another-pass", []account.Account{totp("b", "Other", "o", 2)})
		return "testpass123", nil
	}
	t.Cleanup(func() { promptVaultPassphrase = old })

	s, err := openSession()
	if err == nil {
		s.close()
		t.Fatal("the session opened a vault whose passphrase changed during the prompt")
	}
	var changed *vaultChangedError
	if !errors.As(err, &changed) {
		t.Fatalf("err = %v, want a vaultChangedError", err)
	}
	lock, err := store.TryAcquire(path)
	if err != nil {
		t.Fatalf("the refused session kept the lock: %v", err)
	}
	_ = lock.Release()
}
