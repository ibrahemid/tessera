package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ibrahemid/tessera/go/internal/store"
	"github.com/ibrahemid/tessera/go/internal/vault"
)

func backupFolderWith(t *testing.T, names ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func statusLines(t *testing.T, args ...string) []string {
	t.Helper()
	stdout, err := runRoot(t, append([]string{"vault", "status"}, args...)...)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	return strings.Split(strings.TrimSuffix(stdout, "\n"), "\n")
}

func TestVaultStatusReportsFormatCostAndBackups(t *testing.T) {
	path := withVault(t)
	initVault(t)
	resaveVault(t, path)
	stubKeychainWording(t, false)
	fake := secretService()
	fake.Put(path, "testpass123")
	swapStores(t, fake)
	dir := backupFolderWith(t, "tessera-backup-20260301.json", "tessera-backup-20260310.json", "tessera-backup-20260320.json.old")
	t.Setenv(backupDirEnv, dir)
	env, err := store.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	d := vault.DefaultArgon2

	got := statusLines(t)
	want := []string{
		fmt.Sprintf("Version:  %d", env.Version),
		"Wraps:    passphrase",
		fmt.Sprintf("Argon2:   m=%d KiB, t=%d, p=%d", d.M, d.T, d.P),
		"Previous: " + store.BackupPath(path),
		"Backups:  " + dir + ", newest 2026-03-10",
		"Secrets:  entry present (Secret Service)",
	}
	if len(got) != len(want)+2 {
		t.Fatalf("status lines = %q", got)
	}
	for i, w := range want {
		if got[i+2] != w {
			t.Errorf("line %d = %q, want %q", i+2, got[i+2], w)
		}
	}
}

func TestVaultStatusFlagsABelowDefaultCost(t *testing.T) {
	path := withVault(t)
	def := vault.DefaultArgon2
	vault.DefaultArgon2 = vault.Argon2Params{V: def.V, M: 8, T: 1, P: 1}
	sealVault(t, path, nil)
	vault.DefaultArgon2 = def
	swapStores(t)

	stdout, err := runRoot(t, "vault", "status")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Argon2:   m=8 KiB, t=1, p=1 (below the default; raised at the next unlock)\n",
		"Previous: none\n",
		"Backups:  off; set $TESSERA_BACKUP_DIR to a folder to keep a daily copy\n",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("status lacks %q:\n%s", want, stdout)
		}
	}
}

func TestVaultStatusNamesAnUnusableBackupFolder(t *testing.T) {
	path := withVault(t)
	initVault(t)
	swapStores(t)
	empty := t.TempDir()

	for _, tc := range []struct{ dir, want string }{
		{"backups", "Backups:  not written: $TESSERA_BACKUP_DIR is a relative path (backups); set it to an absolute path\n"},
		{empty, "Backups:  " + empty + ", none yet\n"},
	} {
		t.Setenv(backupDirEnv, tc.dir)
		stdout, err := runRoot(t, "vault", "status")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(stdout, tc.want) {
			t.Errorf("%s: status lacks %q:\n%s", path, tc.want, stdout)
		}
	}
}

func TestVaultStatusJSON(t *testing.T) {
	path := withVault(t)
	initVault(t)
	resaveVault(t, path)
	fake := secretService()
	fake.Put(path, "testpass123")
	locked := kernelKeyring()
	locked.Put(path, "testpass123")
	locked.Lock(path)
	swapStores(t, fake, locked)
	dir := backupFolderWith(t, "tessera-backup-20260310.json")
	t.Setenv(backupDirEnv, dir)
	d := vault.DefaultArgon2

	stdout, err := runRoot(t, "vault", "status", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Path     string   `json:"path"`
		Exists   bool     `json:"exists"`
		Size     int64    `json:"size"`
		Modified string   `json:"modified"`
		Version  int      `json:"version"`
		Wraps    []string `json:"wraps"`
		Argon2   []struct {
			M            uint32 `json:"m"`
			T            uint32 `json:"t"`
			P            uint8  `json:"p"`
			BelowDefault bool   `json:"below_default"`
		} `json:"argon2"`
		Previous     string   `json:"previous"`
		BackupDir    string   `json:"backup_dir"`
		NewestBackup string   `json:"newest_backup"`
		RememberedIn []string `json:"remembered_in"`
		LockedIn     []string `json:"locked_in"`
	}
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("status --json is not JSON: %v\n%s", err, stdout)
	}
	switch {
	case got.Path != path, !got.Exists, got.Size == 0, got.Modified == "", got.Version == 0:
		t.Errorf("file facts = %+v", got)
	case len(got.Wraps) != 1 || got.Wraps[0] != "passphrase":
		t.Errorf("wraps = %v", got.Wraps)
	case len(got.Argon2) != 1 || got.Argon2[0].M != d.M || got.Argon2[0].T != d.T || got.Argon2[0].P != d.P || got.Argon2[0].BelowDefault:
		t.Errorf("argon2 = %+v", got.Argon2)
	case got.Previous != store.BackupPath(path):
		t.Errorf("previous = %q", got.Previous)
	case got.BackupDir != dir || got.NewestBackup != "2026-03-10":
		t.Errorf("backups = %q, %q", got.BackupDir, got.NewestBackup)
	case len(got.RememberedIn) != 1 || got.RememberedIn[0] != "Secret Service":
		t.Errorf("remembered_in = %v", got.RememberedIn)
	case len(got.LockedIn) != 1 || got.LockedIn[0] != "kernel keyring":
		t.Errorf("locked_in = %v", got.LockedIn)
	}
}

func TestVaultStatusJSONForAMissingVault(t *testing.T) {
	withVault(t)
	swapStores(t)

	stdout, err := runRoot(t, "vault", "status", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatal(err)
	}
	if got["exists"] != false {
		t.Errorf("exists = %v", got["exists"])
	}
	for _, key := range []string{"wraps", "argon2", "remembered_in", "locked_in"} {
		if list, ok := got[key].([]any); !ok || len(list) != 0 {
			t.Errorf("%s = %v, want an empty list", key, got[key])
		}
	}
}
