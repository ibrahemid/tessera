package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/importers"
)

// runExport executes the export command with args, returning stdout, stderr
// and the command error.
func runExport(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	c := newExportCmd()
	var stdout, stderr bytes.Buffer
	c.SetOut(&stdout)
	c.SetErr(&stderr)
	c.SetArgs(args)
	err := c.Execute()
	return stdout.String(), stderr.String(), err
}

func steamAccount(id string) account.Account {
	return account.Account{
		ID: id, Type: account.Steam, Issuer: "Steam", Account: "gaben",
		Secret: []byte("12345678901234567890"), Algorithm: "SHA1", Digits: 5, Period: 30,
		CreatedAt: 1, UpdatedAt: 1,
	}
}

// TestExportFormatToStdout: the default sink is stdout, and what comes out is
// a file the importers read back.
func TestExportFormatToStdout(t *testing.T) {
	path := withVault(t)
	sealVault(t, path, []account.Account{totp("a", "ACME", "x", 1), totp("b", "Beta", "y", 2)})

	stdout, _, err := runExport(t, "--format", "aegis")
	if err != nil {
		t.Fatalf("export --format aegis: %v", err)
	}
	accts, source, ok, err := importers.Parse([]byte(stdout))
	if !ok || err != nil {
		t.Fatalf("re-import of the export failed: ok=%v source=%q err=%v", ok, source, err)
	}
	if len(accts) != 2 {
		t.Fatalf("re-imported %d accounts, want 2", len(accts))
	}
}

// TestExportFormatQueryNarrowsToOneAccount: the positional query works the same
// way it does for --uri and --qr.
func TestExportFormatQueryNarrowsToOneAccount(t *testing.T) {
	path := withVault(t)
	sealVault(t, path, []account.Account{totp("a", "ACME", "x", 1), totp("b", "Beta", "y", 2)})

	stdout, _, err := runExport(t, "--format", "2fas", "beta")
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	accts, _, _, err := importers.Parse([]byte(stdout))
	if err != nil {
		t.Fatalf("re-import: %v", err)
	}
	if len(accts) != 1 || accts[0].Issuer != "Beta" {
		t.Fatalf("want only the Beta account, got %+v", accts)
	}
}

// TestExportFormatWritesFile pins the file mode: an export holds cleartext
// secrets and must not be group- or world-readable.
func TestExportFormatWritesFile(t *testing.T) {
	path := withVault(t)
	sealVault(t, path, []account.Account{totp("a", "ACME", "x", 1)})
	dst := filepath.Join(t.TempDir(), "aegis.json")

	if _, _, err := runExport(t, "--format", "aegis", "--out", dst); err != nil {
		t.Fatalf("export: %v", err)
	}
	info, err := os.Stat(dst)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("export file mode = %v, want 0600", info.Mode().Perm())
	}
}

// TestExportFormatWritesDirectory covers the multi-file format: a directory is
// created 0700 and every batch lands inside it.
func TestExportFormatWritesDirectory(t *testing.T) {
	path := withVault(t)
	var accts []account.Account
	for i := 0; i < 25; i++ {
		accts = append(accts, totp(string(rune('a'+i)), "Issuer", "user", int64(i)))
	}
	sealVault(t, path, accts)
	dir := filepath.Join(t.TempDir(), "qrcodes")

	stdout, _, err := runExport(t, "--format", "google-migration", "--out", dir)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) < 3 {
		t.Fatalf("25 accounts should span at least 3 batches, got %d files", len(entries))
	}
	if !strings.Contains(stdout, "google-migration-1-of-") {
		t.Errorf("expected the written paths on stdout, got %q", stdout)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Errorf("export directory mode = %v, want 0700", info.Mode().Perm())
	}
	for _, e := range entries {
		fi, err := os.Stat(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatalf("stat %s: %v", e.Name(), err)
		}
		if fi.Mode().Perm() != 0o600 {
			t.Errorf("%s mode = %v, want 0600", e.Name(), fi.Mode().Perm())
		}
	}
}

// TestExportFormatRejectsIllegalCombinations: every rejected run must write
// nothing at all.
func TestExportFormatRejectsIllegalCombinations(t *testing.T) {
	path := withVault(t)
	sealVault(t, path, []account.Account{totp("a", "ACME", "x", 1)})
	dst := filepath.Join(t.TempDir(), "out.json")

	cases := []struct {
		name string
		args []string
		want string
	}{
		{"uri", []string{"--format", "aegis", "--uri", "--out", dst}, "use exactly one of"},
		{"secret", []string{"--format", "aegis", "--secret", "--out", dst}, "use exactly one of"},
		{"qr", []string{"--format", "aegis", "--qr", dst, "--out", dst}, "use exactly one of"},
		{"file", []string{"--format", "aegis", "--file", dst, "--out", dst}, "use exactly one of"},
		{"unknown format", []string{"--format", "nope", "--out", dst}, "unknown format"},
		{"multi-file to stdout", []string{"--format", "google-migration"}, "pass --out <directory>"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := runExport(t, tc.args...)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want an error containing %q, got %v", tc.want, err)
			}
			if _, serr := os.Stat(dst); serr == nil {
				t.Fatalf("a rejected export must not write %s", dst)
			}
		})
	}
}

// TestExportFormatReportsSkips: an account the format cannot store is named on
// stderr, so it never lands in a piped export file.
func TestExportFormatReportsSkips(t *testing.T) {
	path := withVault(t)
	sealVault(t, path, []account.Account{totp("a", "ACME", "x", 1), steamAccount("s")})

	stdout, stderr, err := runExport(t, "--format", "google-migration", "--out", filepath.Join(t.TempDir(), "qr"))
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if !strings.Contains(stderr, "Skipped Steam (gaben)") {
		t.Errorf("expected the Steam account reported on stderr, got %q", stderr)
	}
	if strings.Contains(stdout, "Skipped") {
		t.Errorf("skip lines must stay off stdout, got %q", stdout)
	}
}

// TestExportFormatNothingToExport: a vault the format cannot carry at all is an
// error, not an empty file.
func TestExportFormatNothingToExport(t *testing.T) {
	path := withVault(t)
	sealVault(t, path, []account.Account{steamAccount("s")})
	dst := filepath.Join(t.TempDir(), "qr")

	_, _, err := runExport(t, "--format", "google-migration", "--out", dst)
	if err == nil || !strings.Contains(err.Error(), "nothing to export") {
		t.Fatalf("want a nothing-to-export error, got %v", err)
	}
	if _, serr := os.Stat(dst); serr == nil {
		t.Fatal("nothing should have been written")
	}
}

// TestExportWithoutAModeStillExplainsItself keeps the no-flag guidance current.
func TestExportWithoutAModeStillExplainsItself(t *testing.T) {
	path := withVault(t)
	sealVault(t, path, []account.Account{totp("a", "ACME", "x", 1)})

	_, _, err := runExport(t)
	if err == nil || !strings.Contains(err.Error(), "--format") {
		t.Fatalf("the usage error should mention --format, got %v", err)
	}
}
