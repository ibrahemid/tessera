//go:build unix || windows

package store

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLockRefusesADirectorySidecar(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vault.json")
	if err := os.Mkdir(LockPath(path), 0o700); err != nil {
		t.Fatal(err)
	}
	_, err := TryAcquire(path)
	var lfe *LockFileError
	if !errors.As(err, &lfe) || lfe.Path != LockPath(path) || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("TryAcquire = %v, want *LockFileError for a directory at %s", err, LockPath(path))
	}
}
