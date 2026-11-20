package store

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/store/storetest"
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
	storetest.AssertOwnerOnly(t, path)
	storetest.AssertOwnerOnlyDir(t, filepath.Dir(path))
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

func TestMkdirPrivateCreatesEachMissingLevelOwnerOnly(t *testing.T) {
	top := filepath.Join(t.TempDir(), "a")
	leaf := filepath.Join(top, "b")
	if err := MkdirPrivate(leaf); err != nil {
		t.Fatal(err)
	}
	storetest.AssertOwnerOnlyDir(t, top)
	storetest.AssertOwnerOnlyDir(t, leaf)
	if err := MkdirPrivate(leaf); err != nil {
		t.Fatalf("MkdirPrivate on an existing directory: %v", err)
	}
	file := filepath.Join(top, "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := MkdirPrivate(file); err == nil {
		t.Fatal("MkdirPrivate over a file succeeded")
	}
}

func TestLoadMissingReturnsNotFound(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "nope.json")); err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}
