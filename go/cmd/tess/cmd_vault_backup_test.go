package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/store"
	"github.com/ibrahemid/tessera/go/internal/store/storetest"
	"github.com/ibrahemid/tessera/go/internal/vault"
)

func captureWarnings(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	old := warnOut
	warnOut = &buf
	t.Cleanup(func() { warnOut = old })
	return &buf
}

func saveSession(t *testing.T, accts []account.Account) error {
	t.Helper()
	s, err := openSession()
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.close()
	s.accounts = accts
	return s.save()
}

func TestSessionSaveWritesTheDayBackup(t *testing.T) {
	path := withVault(t)
	initVault(t)
	dir := t.TempDir()
	t.Setenv(backupDirEnv, dir)
	stubClock(t, time.Date(2026, 3, 4, 23, 30, 0, 0, time.FixedZone("EST", -5*3600)))
	warnings := captureWarnings(t)

	if err := saveSession(t, []account.Account{totp("a", "ACME", "x", 1)}); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := saveSession(t, []account.Account{totp("a", "ACME", "x", 1), totp("b", "ACME", "y", 2)}); err != nil {
		t.Fatalf("save: %v", err)
	}
	if warnings.Len() != 0 {
		t.Errorf("warnings: %s", warnings)
	}
	names, err := store.ListBackups(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(names, []string{"tessera-backup-20260305.json"}) {
		t.Fatalf("backup folder = %v, want one file named by the UTC day", names)
	}
	day := filepath.Join(dir, names[0])
	if got := accountIDs(openVaultFile(t, day, "testpass123")); got != "a,b" {
		t.Errorf("a same-day write must replace the day file, it holds %q", got)
	}
	storetest.AssertOwnerOnly(t, day)
	if got := accountIDs(openVaultFile(t, path, "testpass123")); got != "a,b" {
		t.Errorf("vault holds %q", got)
	}
}

func TestVaultInitWritesTheDayBackup(t *testing.T) {
	withVault(t)
	dir := t.TempDir()
	t.Setenv(backupDirEnv, dir)
	stubClock(t, time.Date(2026, 3, 5, 12, 0, 0, 0, time.UTC))

	initVault(t)
	if _, err := os.Stat(filepath.Join(dir, "tessera-backup-20260305.json")); err != nil {
		t.Errorf("init must write the day backup: %v", err)
	}
}

func TestUnlockUpgradeIsPersistedAndBackedUp(t *testing.T) {
	path := withVault(t)
	dir := t.TempDir()
	t.Setenv(backupDirEnv, dir)
	stubClock(t, time.Date(2026, 3, 5, 12, 0, 0, 0, time.UTC))
	def := vault.DefaultArgon2
	vault.DefaultArgon2 = vault.Argon2Params{V: def.V, M: 8, T: 1, P: 1}
	sealVault(t, path, []account.Account{totp("a", "ACME", "x", 1)})
	vault.DefaultArgon2 = def
	if got := passphraseCost(t, path); got.M != 8 {
		t.Fatalf("setup: sealed wrap m = %d", got.M)
	}

	if _, err := runRoot(t, "list"); err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, p := range []string{path, filepath.Join(dir, "tessera-backup-20260305.json")} {
		if got := passphraseCost(t, p); got.M != def.M || got.T != def.T || got.P != def.P {
			t.Errorf("%s: wrap cost = %+v, want the default %+v", p, got, def)
		}
	}
}

func passphraseCost(t *testing.T, path string) vault.Argon2Params {
	t.Helper()
	env, err := store.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range env.Wraps {
		if w.Type == "passphrase" && w.Params != nil {
			return *w.Params
		}
	}
	t.Fatalf("%s has no passphrase wrap", path)
	return vault.Argon2Params{}
}

func TestSessionSaveWarnsAboutAnUnusableBackupFolder(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(t.TempDir(), "gone")
	for _, tc := range []struct {
		dir, want string
	}{
		{"backups", "no backup written: $TESSERA_BACKUP_DIR is a relative path (backups); set it to an absolute path"},
		{missing, "no backup written: the folder " + missing + " in $TESSERA_BACKUP_DIR does not exist; create it or change the variable"},
		{file, "no backup written: " + file + " in $TESSERA_BACKUP_DIR is not a folder; set the variable to a folder"},
	} {
		t.Run(filepath.Base(tc.dir), func(t *testing.T) {
			withVault(t)
			initVault(t)
			t.Setenv(backupDirEnv, tc.dir)
			warnings := captureWarnings(t)

			if err := saveSession(t, []account.Account{totp("a", "ACME", "x", 1)}); err != nil {
				t.Fatalf("a backup problem must not fail the save: %v", err)
			}
			if got := warnings.String(); got != "warning: "+tc.want+"\n" {
				t.Errorf("warning = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestBackupFailureNamesAVaultWithoutAPassphrase(t *testing.T) {
	if got := backupFailure(store.ErrBackupNeedsPassphrase); got != "no backup written: the vault has no passphrase unlock" {
		t.Errorf("backupFailure = %q", got)
	}
}

func TestVaultBackupWritesToTheVariableFolder(t *testing.T) {
	withVault(t)
	initVault(t)
	dir := t.TempDir()
	t.Setenv(backupDirEnv, dir)
	stubClock(t, time.Date(2026, 3, 5, 12, 0, 0, 0, time.UTC))
	for day := 1; day <= store.BackupRetention+1; day++ {
		name := fmt.Sprintf("tessera-backup-202501%02d.json", day)
		if err := os.WriteFile(filepath.Join(dir, name), []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	stdout, stderr, err := runRootStreams(t, "vault", "backup")
	if err != nil {
		t.Fatalf("backup: %v\n%s", err, stderr)
	}
	want := "Wrote " + filepath.Join(dir, "tessera-backup-20260305.json") + "\nDeleted 2 older backups\n"
	if stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
	names, err := store.ListBackups(dir)
	if err != nil || len(names) != store.BackupRetention {
		t.Errorf("backup folder holds %d files, %v; want %d", len(names), err, store.BackupRetention)
	}
}

func TestVaultBackupToARelativeFolder(t *testing.T) {
	withVault(t)
	initVault(t)
	work := t.TempDir()
	if err := os.Mkdir(filepath.Join(work, "out"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(work)
	stubClock(t, time.Date(2026, 3, 5, 12, 0, 0, 0, time.UTC))

	if _, stderr, err := runRootStreams(t, "vault", "backup", "--to", "out"); err != nil {
		t.Fatalf("backup: %v\n%s", err, stderr)
	}
	openVaultFile(t, filepath.Join(work, "out", "tessera-backup-20260305.json"), "testpass123")
}

func TestVaultBackupNeedsAUsableFolder(t *testing.T) {
	withVault(t)
	initVault(t)
	missing := filepath.Join(t.TempDir(), "gone")

	_, _, err := runRootStreams(t, "vault", "backup")
	if !errors.Is(err, errNoBackupDir) {
		t.Errorf("no folder: err = %v", err)
	}

	t.Setenv(backupDirEnv, missing)
	_, stderr, err := runRootStreams(t, "vault", "backup")
	if err == nil || !strings.Contains(stderr, "the folder "+missing+" in $TESSERA_BACKUP_DIR does not exist") {
		t.Errorf("missing variable folder: err = %v, stderr = %q", err, stderr)
	}

	_, stderr, err = runRootStreams(t, "vault", "backup", "--to", missing)
	if err == nil || !strings.Contains(stderr, "the backup folder "+missing+" does not exist") {
		t.Errorf("missing --to folder: err = %v, stderr = %q", err, stderr)
	}
}
