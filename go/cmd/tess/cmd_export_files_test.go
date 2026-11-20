package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/store/storetest"
)

func sealTwoAccounts(t *testing.T) []account.Account {
	t.Helper()
	path := withVault(t)
	accts := []account.Account{totp("a", "ACME", "x", 1), totp("b", "Beta", "y", 2)}
	sealVault(t, path, accts)
	return accts
}

func assertExists(t *testing.T, err error, path string) {
	t.Helper()
	var exists *exportExistsError
	if !errors.As(err, &exists) || !errors.Is(err, fs.ErrExist) {
		t.Fatalf("want an exists error, got %v", err)
	}
	if want := path + " already exists; pass --force to replace it"; err.Error() != want {
		t.Fatalf("message = %q, want %q", err.Error(), want)
	}
}

func writeOld(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertContent(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("%s = %q, want %q", path, got, want)
	}
}

func TestExportOutReplacesAFileOnlyWithForce(t *testing.T) {
	sealTwoAccounts(t)
	dst := filepath.Join(t.TempDir(), "aegis.json")
	writeOld(t, dst)

	stdout, _, err := runRootStreams(t, "export", "--format", "aegis", "--out", dst)
	assertExists(t, err, dst)
	if stdout != "" {
		t.Fatalf("a refused export printed %q", stdout)
	}
	assertContent(t, dst, "old")

	stdout, _, err = runExport(t, "--format", "aegis", "--out", dst, "--force")
	if err != nil {
		t.Fatal(err)
	}
	if stdout != "Wrote "+dst+"\n" {
		t.Fatalf("stdout = %q", stdout)
	}
	storetest.AssertOwnerOnly(t, dst)
	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"issuer": "ACME"`) {
		t.Fatalf("the file was not replaced:\n%s", data)
	}
}

func TestExportQRWritesOwnerOnlyFiles(t *testing.T) {
	accts := sealTwoAccounts(t)
	dir := filepath.Join(t.TempDir(), "qr")
	if _, _, err := runExport(t, "--qr", dir); err != nil {
		t.Fatal(err)
	}
	storetest.AssertOwnerOnlyDir(t, dir)
	for i, a := range accts {
		storetest.AssertOwnerOnly(t, filepath.Join(dir, qrFileName(a, i)))
	}
}

func TestExportQRRefusedRunWritesNothing(t *testing.T) {
	accts := sealTwoAccounts(t)
	dir := t.TempDir()
	first, second := filepath.Join(dir, qrFileName(accts[0], 0)), filepath.Join(dir, qrFileName(accts[1], 1))
	writeOld(t, second)

	_, _, err := runExport(t, "--qr", dir)
	assertExists(t, err, second)
	if _, err := os.Lstat(first); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("a refused run wrote %s", first)
	}
	assertContent(t, second, "old")

	if _, _, err := runExport(t, "--qr", dir, "--force"); err != nil {
		t.Fatal(err)
	}
	storetest.AssertOwnerOnly(t, first)
	storetest.AssertOwnerOnly(t, second)
}

func TestExportNeverWritesThroughASymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need extra privileges on Windows")
	}
	sealTwoAccounts(t)
	dir := t.TempDir()
	target := filepath.Join(dir, "target.txt")
	writeOld(t, target)
	link := filepath.Join(dir, "export.json")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	_, _, err := runExport(t, "--format", "aegis", "--out", link)
	assertExists(t, err, link)
	assertContent(t, target, "old")

	if _, _, err := runExport(t, "--format", "aegis", "--out", link, "--force"); err != nil {
		t.Fatal(err)
	}
	storetest.AssertOwnerOnly(t, link)
	assertContent(t, target, "old")
}

func TestExportLeavesOutTrashedAccounts(t *testing.T) {
	path := withVault(t)
	sealVault(t, path, []account.Account{
		totp("a", "ACME", "x", 1),
		trashedAt(totp("b", "Dropbox", "me", 2), dropboxTrashedAt),
	})
	for _, args := range [][]string{{"--uri"}, {"--secret"}, {"--format", "aegis"}, {"--format", "bitwarden"}} {
		stdout, _, err := runExport(t, args...)
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if strings.Contains(stdout, "Dropbox") || stdout == "" {
			t.Errorf("%v: want only the live account:\n%s", args, stdout)
		}
		if args[0] == "--secret" && strings.Count(stdout, "\n") != 1 {
			t.Errorf("--secret printed %d lines, want 1", strings.Count(stdout, "\n"))
		}
	}
	dir := filepath.Join(t.TempDir(), "qr")
	stdout, _, err := runExport(t, "--qr", dir)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || strings.Contains(stdout, "Dropbox") {
		t.Fatalf("--qr wrote %d files:\n%s", len(entries), stdout)
	}
	if _, _, err := runExport(t, "--uri", "dropbox"); err == nil {
		t.Fatal("a query reached a trashed account")
	}
}

func TestExportCarriesTheNote(t *testing.T) {
	path := withVault(t)
	a := totp("a", "ACME", "x", 1)
	a.Note = "backup codes in the drawer"
	sealVault(t, path, []account.Account{a})
	for _, format := range []string{"aegis", "bitwarden", "proton", "apple-csv"} {
		stdout, _, err := runExport(t, "--format", format)
		if err != nil {
			t.Fatalf("%s: %v", format, err)
		}
		if !strings.Contains(stdout, a.Note) {
			t.Errorf("--format %s left out the note:\n%s", format, stdout)
		}
	}
}
