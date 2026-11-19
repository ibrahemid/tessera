package store

import (
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
	assertOwnerOnly(t, path)
	assertOwnerOnlyDir(t, filepath.Dir(path))
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
