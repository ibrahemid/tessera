package store

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/vault"
)

func sealOne(t *testing.T, issuer string) *vault.Envelope {
	t.Helper()
	env, err := vault.Seal([]account.Account{
		{ID: issuer, Type: account.TOTP, Issuer: issuer, Account: "a", Secret: []byte("12345678901234567890"),
			Algorithm: "SHA1", Digits: 6, Period: 30},
	}, "pw")
	if err != nil {
		t.Fatal(err)
	}
	return env
}

func readBytes(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func assertNoTempFiles(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("temporary file %s left behind", e.Name())
		}
	}
}

func TestSaveKeepsThePreviousGenerationAsBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vault.json")
	if err := Save(path, sealOne(t, "first")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(BackupPath(path)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("first save left a backup: %v", err)
	}
	first := readBytes(t, path)
	if err := Save(path, sealOne(t, "second")); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(readBytes(t, BackupPath(path)), first) {
		t.Fatal("backup after the second save is not the first generation")
	}
	second := readBytes(t, path)
	if err := Save(path, sealOne(t, "third")); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(readBytes(t, BackupPath(path)), second) {
		t.Fatal("backup after the third save is not the second generation")
	}
	assertOwnerOnly(t, BackupPath(path))
	assertOwnerOnly(t, path)
	assertNoTempFiles(t, filepath.Dir(path))
}

func TestRemoveBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vault.json")
	if err := RemoveBackup(path); err != nil {
		t.Fatalf("RemoveBackup without a backup: %v", err)
	}
	for _, issuer := range []string{"first", "second"} {
		if err := Save(path, sealOne(t, issuer)); err != nil {
			t.Fatal(err)
		}
	}
	if err := RemoveBackup(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(BackupPath(path)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("backup still present: %v", err)
	}
	if err := RemoveBackup(path); err != nil {
		t.Fatalf("second RemoveBackup: %v", err)
	}
	if _, err := Load(path); err != nil {
		t.Fatalf("vault unreadable after RemoveBackup: %v", err)
	}
}

func TestLoadNamesTheBackupWhenTheVaultDoesNotParse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vault.json")
	for _, issuer := range []string{"first", "second"} {
		if err := Save(path, sealOne(t, issuer)); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(path, []byte(`{"version":`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	var bae *BackupAvailableError
	if !errors.As(err, &bae) {
		t.Fatalf("Load = %v, want *BackupAvailableError", err)
	}
	if bae.Path != path || bae.Backup != BackupPath(path) || bae.Err == nil {
		t.Errorf("BackupAvailableError = %+v", bae)
	}
	if !strings.Contains(err.Error(), BackupPath(path)) {
		t.Errorf("error %q does not name the backup", err)
	}

	env, err := LoadBackup(path)
	if err != nil {
		t.Fatalf("LoadBackup: %v", err)
	}
	accts, err := env.Open("pw")
	if err != nil {
		t.Fatal(err)
	}
	if len(accts) != 1 || accts[0].Issuer != "first" {
		t.Errorf("backup holds %+v, want the first generation", accts)
	}

	good := readBytes(t, BackupPath(path))
	if err := Save(path, env); err != nil {
		t.Fatalf("Save of the restored backup: %v", err)
	}
	if !bytes.Equal(readBytes(t, BackupPath(path)), good) {
		t.Error("saving over an unparseable vault replaced the backup")
	}
	if _, err := Load(path); err != nil {
		t.Errorf("vault after restoring the backup: %v", err)
	}
}

func TestLoadWithoutBackupReturnsTheParseError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vault.json")
	if err := os.WriteFile(path, []byte(`{"version":`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	var bae *BackupAvailableError
	if err == nil || errors.As(err, &bae) {
		t.Fatalf("Load = %v, want a parse error without a backup", err)
	}
}

func TestLoadBackupWithoutBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vault.json")
	if err := Save(path, sealOne(t, "only")); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadBackup(path); !errors.Is(err, ErrNoBackup) {
		t.Fatalf("LoadBackup = %v, want ErrNoBackup", err)
	}
}

func TestWriteSecretFileCreatesAnOwnerOnlyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "export.txt")
	if err := WriteSecretFile(path, []byte("secret"), false); err != nil {
		t.Fatal(err)
	}
	if got := readBytes(t, path); string(got) != "secret" {
		t.Errorf("content = %q", got)
	}
	assertOwnerOnly(t, path)
	assertNoTempFiles(t, dir)
}

func TestWriteSecretFileRefusesAnExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "export.txt")
	if err := os.WriteFile(path, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := WriteSecretFile(path, []byte("secret"), false)
	var ee *ExistsError
	if !errors.As(err, &ee) || ee.Path != path || !errors.Is(err, fs.ErrExist) {
		t.Fatalf("WriteSecretFile = %v, want *ExistsError for %s", err, path)
	}
	if got := readBytes(t, path); string(got) != "keep" {
		t.Errorf("existing file changed to %q", got)
	}
	assertNoTempFiles(t, dir)
}

func TestWriteSecretFileReplacesWhenAsked(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "export.txt")
	if err := os.WriteFile(path, []byte("old"), 0o666); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o666); err != nil {
		t.Fatal(err)
	}
	if err := WriteSecretFile(path, []byte("new"), true); err != nil {
		t.Fatal(err)
	}
	if got := readBytes(t, path); string(got) != "new" {
		t.Errorf("content = %q", got)
	}
	assertOwnerOnly(t, path)
	assertNoTempFiles(t, dir)
}

func symlinkOrSkip(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
}

func TestWriteSecretFileNeverWritesThroughASymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.txt")
	if err := os.WriteFile(target, []byte("target"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "export.txt")
	symlinkOrSkip(t, target, link)

	err := WriteSecretFile(link, []byte("secret"), false)
	if !errors.Is(err, fs.ErrExist) {
		t.Fatalf("WriteSecretFile over a symlink = %v, want *ExistsError", err)
	}
	if info, err := os.Lstat(link); err != nil || info.Mode()&fs.ModeSymlink == 0 {
		t.Fatalf("symlink was changed: %v %v", info, err)
	}

	if err := WriteSecretFile(link, []byte("secret"), true); err != nil {
		t.Fatal(err)
	}
	if got := readBytes(t, target); string(got) != "target" {
		t.Errorf("symlink target changed to %q", got)
	}
	if got := readBytes(t, link); string(got) != "secret" {
		t.Errorf("content = %q", got)
	}
	assertOwnerOnly(t, link)
	assertNoTempFiles(t, dir)
}

func TestWriteSecretFileRefusesADanglingSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "missing.txt")
	link := filepath.Join(dir, "export.txt")
	symlinkOrSkip(t, target, link)
	if err := WriteSecretFile(link, []byte("secret"), false); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("WriteSecretFile over a dangling symlink = %v, want *ExistsError", err)
	}
	if _, err := os.Lstat(target); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("symlink target was created: %v", err)
	}
}
