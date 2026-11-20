package store

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func fileOfSize(t *testing.T, path string, size int64) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(size); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestLoadRefusesAFileOverTheCap(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vault.json")
	fileOfSize(t, path, MaxVaultSize+1)

	_, err := Load(path)
	var tooLarge *TooLargeError
	if !errors.As(err, &tooLarge) {
		t.Fatalf("Load = %v, want a *TooLargeError", err)
	}
	if tooLarge.Path != path || tooLarge.Limit != MaxVaultSize {
		t.Errorf("error = %+v", tooLarge)
	}
	var withBak *BackupAvailableError
	if errors.As(err, &withBak) {
		t.Errorf("no backup exists, yet the error names one: %v", err)
	}
}

func TestLoadReadsAFileAtTheCap(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vault.json")
	fileOfSize(t, path, MaxVaultSize)

	_, err := Load(path)
	var tooLarge *TooLargeError
	if err == nil || errors.As(err, &tooLarge) {
		t.Fatalf("Load = %v, want a parse error for a file at the cap", err)
	}
}

func TestLoadNamesTheBackupForAFileOverTheCap(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vault.json")
	if err := Save(path, sealOne(t, "first")); err != nil {
		t.Fatal(err)
	}
	if err := Save(path, sealOne(t, "second")); err != nil {
		t.Fatal(err)
	}
	fileOfSize(t, path, MaxVaultSize+1)

	_, err := Load(path)
	var withBak *BackupAvailableError
	if !errors.As(err, &withBak) || withBak.Backup != BackupPath(path) {
		t.Fatalf("Load = %v, want a *BackupAvailableError naming %s", err, BackupPath(path))
	}
	var tooLarge *TooLargeError
	if !errors.As(err, &tooLarge) {
		t.Errorf("Load = %v, want it to wrap a *TooLargeError", err)
	}
}

func TestSaveOverAFileOverTheCapKeepsNoBackupOfIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vault.json")
	fileOfSize(t, path, MaxVaultSize+1)

	if err := Save(path, sealOne(t, "fresh")); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := os.Lstat(BackupPath(path)); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("an oversized file was kept as the backup: %v", err)
	}
	if _, err := Load(path); err != nil {
		t.Errorf("Load after Save: %v", err)
	}
}
