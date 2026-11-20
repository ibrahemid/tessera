package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/ibrahemid/tessera/go/internal/account"
)

func TestExportFileRefusesAnExistingFileWithoutForce(t *testing.T) {
	path := withVault(t)
	sealVault(t, path, []account.Account{totp("a", "ACME", "x", 1)})
	dir := t.TempDir()
	dst := filepath.Join(dir, "notes.txt")
	want := []byte("unrelated file the user keeps here\n")
	if err := os.WriteFile(dst, want, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := runRoot(t, "export", "--file", dst)
	var exists *exportExistsError
	if !errors.As(err, &exists) || exists.path != dst {
		t.Fatalf("err = %v, want the destination named as existing", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("export --file replaced an existing file without --force; now %d bytes", len(got))
	}
	if names := dirEntries(t, dir); !slices.Equal(names, []string{"notes.txt"}) {
		t.Fatalf("export --file left %v", names)
	}
}

func TestExportFileForceReplacesTheDestination(t *testing.T) {
	path := withVault(t)
	sealVault(t, path, []account.Account{totp("a", "ACME", "x", 1)})
	dir := t.TempDir()
	dst := filepath.Join(dir, "copy.json")
	if err := os.WriteFile(dst, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := runRoot(t, "export", "--file", dst, "--force"); err != nil {
		t.Fatal(err)
	}
	vaultBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, vaultBytes) {
		t.Fatal("the copy differs from the vault")
	}
	info, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("mode = %o, want 600", perm)
	}
	if names := dirEntries(t, dir); !slices.Equal(names, []string{"copy.json"}) {
		t.Fatalf("export --file left %v", names)
	}
}

func dirEntries(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name()
	}
	return names
}
