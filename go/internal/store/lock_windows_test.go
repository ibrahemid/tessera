package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLockRefusesASymlinkedSidecar(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.json")
	target := filepath.Join(dir, "elsewhere")
	if err := os.WriteFile(target, []byte("untouched"), 0o600); err != nil {
		t.Fatal(err)
	}
	symlinkOrSkip(t, target, LockPath(path))
	_, err := TryAcquire(path)
	var lfe *LockFileError
	if !errors.As(err, &lfe) || lfe.Path != LockPath(path) {
		t.Fatalf("TryAcquire = %v, want *LockFileError for %s", err, LockPath(path))
	}
	if got := readBytes(t, target); string(got) != "untouched" {
		t.Errorf("symlink target changed to %q", got)
	}
}
