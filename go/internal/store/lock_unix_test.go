//go:build unix

package store

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/ibrahemid/tessera/go/internal/store/storetest"
)

func assertLockFileError(t *testing.T, err error, lockPath string) {
	t.Helper()
	var lfe *LockFileError
	if !errors.As(err, &lfe) {
		t.Fatalf("Acquire = %v, want *LockFileError", err)
	}
	if lfe.Path != lockPath || !strings.Contains(err.Error(), lockPath) {
		t.Errorf("error %q does not name %s", err, lockPath)
	}
}

func TestLockRefusesASymlinkedSidecar(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.json")
	target := filepath.Join(dir, "elsewhere")
	if err := os.WriteFile(target, []byte("untouched"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, LockPath(path)); err != nil {
		t.Fatal(err)
	}
	_, err := Acquire(path)
	assertLockFileError(t, err, LockPath(path))
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 || string(readBytes(t, target)) != "untouched" {
		t.Errorf("symlink target changed: mode %v", info.Mode())
	}
}

func TestLockRefusesADanglingSymlinkedSidecar(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.json")
	target := filepath.Join(dir, "missing")
	if err := os.Symlink(target, LockPath(path)); err != nil {
		t.Fatal(err)
	}
	_, err := TryAcquire(path)
	assertLockFileError(t, err, LockPath(path))
	if _, err := os.Lstat(target); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("symlink target was created: %v", err)
	}
}

func TestLockRefusesASidecarWithAForeignMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vault.json")
	if err := os.WriteFile(LockPath(path), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(LockPath(path), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Acquire(path)
	assertLockFileError(t, err, LockPath(path))
	if !strings.Contains(err.Error(), "0644") {
		t.Errorf("error %q does not state the mode", err)
	}
}

func TestLockCreatesTheSidecarAs0600UnderARestrictiveUmask(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vault.json")
	old := syscall.Umask(0o277)
	l, err := TryAcquire(path)
	syscall.Umask(old)
	if err != nil {
		t.Fatal(err)
	}
	storetest.AssertOwnerOnly(t, LockPath(path))
	if err := l.Release(); err != nil {
		t.Fatal(err)
	}
	again, err := TryAcquire(path)
	if err != nil {
		t.Fatalf("TryAcquire on the existing sidecar: %v", err)
	}
	again.Release()
}
