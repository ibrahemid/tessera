//go:build !windows

package store

import (
	"os"
	"testing"
)

func assertOwnerOnly(t *testing.T, path string) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() {
		t.Errorf("%s mode = %v, want a regular file", path, info.Mode())
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("%s perms = %v, want 0600", path, info.Mode().Perm())
	}
}

func assertOwnerOnlyDir(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() || info.Mode().Perm()&0o077 != 0 {
		t.Errorf("%s mode = %v, want an owner-only directory", path, info.Mode())
	}
}
