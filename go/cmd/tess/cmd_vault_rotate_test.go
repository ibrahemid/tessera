package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/store"
	"github.com/ibrahemid/tessera/go/internal/vault"
	"github.com/spf13/cobra"
)

func resaveVault(t *testing.T, path string) {
	t.Helper()
	env, err := store.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(path, env); err != nil {
		t.Fatal(err)
	}
	if !store.HasBackup(path) {
		t.Fatal("a second save must leave the .bak file")
	}
}

func addWraps(t *testing.T, path string, wraps ...map[string]string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	var list []json.RawMessage
	if err := json.Unmarshal(doc["wraps"], &list); err != nil {
		t.Fatal(err)
	}
	for _, w := range wraps {
		b, err := json.Marshal(w)
		if err != nil {
			t.Fatal(err)
		}
		list = append(list, b)
	}
	if doc["wraps"], err = json.Marshal(list); err != nil {
		t.Fatal(err)
	}
	if data, err = json.Marshal(doc); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func touchIDWrap() map[string]string {
	b64 := func(b byte, n int) string { return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{b}, n)) }
	return map[string]string{"type": "secure-enclave", "kdf": "se-keychain-v1", "se_key": b64(9, 16), "nonce": b64(3, 24), "ct": b64(4, 48)}
}

func vaultDEK(t *testing.T, path, pass string) []byte {
	t.Helper()
	env, err := store.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	dek, err := env.UnwrapDEK(pass)
	if err != nil {
		t.Fatalf("unwrap %s: %v", path, err)
	}
	return dek
}

func TestVaultPasswdDeletesTheBakAndReplacesTheStoredPassphrase(t *testing.T) {
	path := withVault(t)
	initVault(t)
	resaveVault(t, path)
	fake := secretService()
	fake.Put(path, "testpass123")
	swapStores(t, fake)
	t.Setenv("TESSERA_NEW_PASSPHRASE", "brand-new-pass")

	stdout, stderr, err := runRootStreams(t, "vault", "passwd")
	if err != nil {
		t.Fatalf("passwd: %v\n%s", err, stderr)
	}
	if store.HasBackup(path) {
		t.Error("<vault>.bak opens with the old key and must be deleted")
	}
	if got, ok := fake.Secret(path); !ok || got != "brand-new-pass" {
		t.Errorf("stored passphrase = %q, %v; want the new one", got, ok)
	}
	openVaultFile(t, path, "brand-new-pass")
	for _, want := range []string{"Passphrase changed", "Replaced the passphrase stored in the Secret Service"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout lacks %q:\n%s", want, stdout)
		}
	}
	if !strings.Contains(stderr, "$TESSERA_PASSPHRASE still holds the old passphrase") {
		t.Errorf("stderr must say the variable is stale:\n%s", stderr)
	}
}

func TestVaultPasswdClearsTheKernelKeyringEntry(t *testing.T) {
	path := withVault(t)
	initVault(t)
	keyring := kernelKeyring()
	keyring.Put(path, "testpass123")
	swapStores(t, keyring)
	t.Setenv("TESSERA_NEW_PASSPHRASE", "brand-new-pass")

	stdout, _, err := runRootStreams(t, "vault", "passwd")
	if err != nil {
		t.Fatalf("passwd: %v", err)
	}
	if _, ok := keyring.Secret(path); ok {
		t.Error("the kernel keyring must not keep the old passphrase")
	}
	if !strings.Contains(stdout, "Removed the old passphrase from the kernel keyring; run `tess vault remember`") {
		t.Errorf("stdout must say how to store the new passphrase:\n%s", stdout)
	}
}

func TestVaultPasswdWarnsAboutALockedStore(t *testing.T) {
	path := withVault(t)
	initVault(t)
	fake := secretService()
	fake.Put(path, "testpass123")
	fake.Lock(path)
	swapStores(t, fake)
	t.Setenv("TESSERA_NEW_PASSPHRASE", "brand-new-pass")

	_, stderr, err := runRootStreams(t, "vault", "passwd")
	if err != nil {
		t.Fatalf("passwd: %v", err)
	}
	if !strings.Contains(stderr, "the Secret Service is locked") || !strings.Contains(stderr, "tess vault remember") {
		t.Errorf("stderr must name the locked store and the fix:\n%s", stderr)
	}
}

func TestVaultPasswdReportsTheRemovedUnlocks(t *testing.T) {
	path := withVault(t)
	initVault(t)
	addWraps(t, path, touchIDWrap(), map[string]string{"type": "fido2"})
	swapStores(t)
	t.Setenv("TESSERA_NEW_PASSPHRASE", "brand-new-pass")

	stdout, _, err := runRootStreams(t, "vault", "passwd")
	if err != nil {
		t.Fatalf("passwd: %v", err)
	}
	for _, want := range []string{
		"Removed the Touch ID unlock of the Tessera app; the app adds it back the next time you unlock it with the passphrase",
		"Removed the fido2 unlock; add it again from the app that created it",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout lacks %q:\n%s", want, stdout)
		}
	}
	env, err := store.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := env.WrapTypes(); !slices.Equal(got, []string{"passphrase"}) {
		t.Errorf("wraps after passwd = %v", got)
	}
}

func TestVaultRekeyKeepsThePassphraseAndReplacesTheKey(t *testing.T) {
	path := withVault(t)
	sealVault(t, path, []account.Account{totp("a", "ACME", "x", 1)})
	resaveVault(t, path)
	oldDEK := vaultDEK(t, path, "testpass123")
	fake := secretService()
	fake.Put(path, "testpass123")
	swapStores(t, fake)

	stdout, _, err := runRootStreams(t, "vault", "rekey")
	if err != nil {
		t.Fatalf("rekey: %v", err)
	}
	if bytes.Equal(vaultDEK(t, path, "testpass123"), oldDEK) {
		t.Error("rekey must replace the vault key")
	}
	if got := openVaultFile(t, path, "testpass123"); len(got) != 1 || got[0].ID != "a" {
		t.Errorf("accounts after rekey = %+v", got)
	}
	if store.HasBackup(path) {
		t.Error("<vault>.bak opens with the old key and must be deleted")
	}
	if got, _ := fake.Secret(path); got != "testpass123" {
		t.Errorf("rekey must leave the stored passphrase alone, got %q", got)
	}
	if !strings.Contains(stdout, "Vault key replaced") {
		t.Errorf("stdout = %q", stdout)
	}
}

func TestVaultRekeyKeepsOnlyTodaysBackup(t *testing.T) {
	path := withVault(t)
	initVault(t)
	swapStores(t)
	dir := t.TempDir()
	t.Setenv(backupDirEnv, dir)
	stubClock(t, time.Date(2026, 3, 5, 12, 0, 0, 0, time.UTC))
	for _, name := range []string{"tessera-backup-20260301.json", "tessera-backup-20260302.json", "notes.txt", "tessera-backup-20260303.json.old"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	stdout, stderr, err := runRootStreams(t, "vault", "rekey")
	if err != nil {
		t.Fatalf("rekey: %v\n%s", err, stderr)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	want := []string{"notes.txt", "tessera-backup-20260303.json.old", "tessera-backup-20260305.json"}
	if !slices.Equal(names, want) {
		t.Errorf("backup folder = %v, want %v", names, want)
	}
	day := filepath.Join(dir, "tessera-backup-20260305.json")
	if !bytes.Equal(vaultDEK(t, day, "testpass123"), vaultDEK(t, path, "testpass123")) {
		t.Error("today's backup must hold the new key")
	}
	if !strings.Contains(stdout, "Deleted 2 older backups in "+dir+" that opened with the old key") {
		t.Errorf("stdout = %q", stdout)
	}
}

func TestVaultRotationStopsWhenTheVaultGotANewKey(t *testing.T) {
	path := withVault(t)
	initVault(t)
	other := filepath.Join(t.TempDir(), "other.json")
	sealAt(t, other, "testpass123", nil)
	staleDEK := vaultDEK(t, other, "testpass123")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	_, _, err = rotateLocked(&cobra.Command{}, path, staleDEK, "testpass123", "brand-new-pass")
	var changed *vaultChangedError
	if !errors.As(err, &changed) {
		t.Fatalf("err = %v, want a vaultChangedError", err)
	}
	if !strings.Contains(userMessage(err), "run the command again") {
		t.Errorf("message = %q", userMessage(err))
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Error("a stale key must leave the vault untouched")
	}
}

func TestVaultPasswdNamesTheBakOnAWrongPassphrase(t *testing.T) {
	path := withVault(t)
	initVault(t)
	resaveVault(t, path)
	t.Setenv("TESSERA_PASSPHRASE", "wrong-passphrase")
	t.Setenv("TESSERA_NEW_PASSPHRASE", "brand-new-pass")

	_, stderr, err := runRootStreams(t, "vault", "passwd")
	if !errors.Is(err, vault.ErrWrongPassphrase) {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(stderr, store.BackupPath(path)) || !strings.Contains(stderr, "tess vault restore --bak --vault "+quoteArg(path)) {
		t.Errorf("stderr must name the .bak and the restore command:\n%s", stderr)
	}
	if !store.HasBackup(path) {
		t.Error("a failed passwd must keep the .bak")
	}
}
