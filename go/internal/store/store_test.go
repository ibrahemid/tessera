package store

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/vault"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "vault.json")
	env, err := vault.Seal([]account.Account{
		{ID: "1", Type: account.TOTP, Issuer: "X", Account: "a", Secret: []byte("12345678901234567890"),
			Algorithm: "SHA1", Digits: 6, Period: 30},
	}, "pw")
	if err != nil {
		t.Fatal(err)
	}
	if err := Save(path, env); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("vault perms = %v, want 0600", info.Mode().Perm())
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := loaded.Open("pw")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Issuer != "X" {
		t.Errorf("round trip mismatch: %+v", got)
	}
}

func TestLoadMissingReturnsNotFound(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "nope.json")); err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestResolvePrecedence(t *testing.T) {
	t.Setenv("TESSERA_VAULT", "/env/path.json")
	if p, _ := Resolve("/override.json"); p != "/override.json" {
		t.Errorf("override should win, got %s", p)
	}
	if p, _ := Resolve(""); p != "/env/path.json" {
		t.Errorf("env should win when no override, got %s", p)
	}
}
