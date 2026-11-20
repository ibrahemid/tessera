//go:build !windows

package storetest

import (
	"os"
	"testing"
)

// AssertOwnerOnly fails t unless path is a regular file with mode 0600.
func AssertOwnerOnly(t testing.TB, path string) {
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

// AssertOwnerOnlyDir fails t unless path is a directory with mode 0700.
func AssertOwnerOnlyDir(t testing.TB, path string) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() || info.Mode().Perm() != 0o700 {
		t.Errorf("%s mode = %v, want a 0700 directory", path, info.Mode())
	}
}
