package store

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ibrahemid/tessera/go/internal/store/storetest"
	"golang.org/x/sys/windows"
)

func holdOpenWithoutShareDelete(t *testing.T, path string) windows.Handle {
	t.Helper()
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil,
		windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func TestSaveSetsAProtectedPrivateDACL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new", "vault.json")
	for _, issuer := range []string{"first", "second"} {
		if err := Save(path, sealOne(t, issuer)); err != nil {
			t.Fatal(err)
		}
	}
	storetest.AssertOwnerOnly(t, path)
	storetest.AssertOwnerOnly(t, BackupPath(path))
	storetest.AssertOwnerOnlyDir(t, filepath.Dir(path))
}

func TestCreateTempSetsTheDACLBeforeAnyWrite(t *testing.T) {
	f, err := createTemp(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	name := f.Name()
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(name); err != nil || info.Size() != 0 {
		t.Fatalf("temp file: %v %v", info, err)
	}
	storetest.AssertOwnerOnly(t, name)
}

func TestMkdirPrivateSetsAnInheritableDACLOnEachNewLevel(t *testing.T) {
	top := filepath.Join(t.TempDir(), "a")
	leaf := filepath.Join(top, "b")
	if err := mkdirPrivate(leaf); err != nil {
		t.Fatal(err)
	}
	storetest.AssertOwnerOnlyDir(t, top)
	storetest.AssertOwnerOnlyDir(t, leaf)
	if err := mkdirPrivate(leaf); err != nil {
		t.Fatalf("mkdirPrivate on an existing directory: %v", err)
	}
}

func TestSaveRetriesTheRenameWhileAReaderHoldsTheVault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vault.json")
	if err := Save(path, sealOne(t, "first")); err != nil {
		t.Fatal(err)
	}
	h := holdOpenWithoutShareDelete(t, path)
	released := make(chan struct{})
	go func() {
		time.Sleep(100 * time.Millisecond)
		windows.CloseHandle(h)
		close(released)
	}()
	if err := Save(path, sealOne(t, "second")); err != nil {
		t.Fatalf("Save while a reader held the vault: %v", err)
	}
	<-released
	env, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	accts, err := env.Open("pw")
	if err != nil || len(accts) != 1 || accts[0].Issuer != "second" {
		t.Fatalf("vault after retry = %+v, %v", accts, err)
	}
}

func TestSaveGivesUpWhenTheVaultStaysHeld(t *testing.T) {
	attempts, delay := retryAttempts, retryDelay
	retryAttempts, retryDelay = 3, time.Millisecond
	t.Cleanup(func() { retryAttempts, retryDelay = attempts, delay })

	path := filepath.Join(t.TempDir(), "vault.json")
	if err := Save(path, sealOne(t, "first")); err != nil {
		t.Fatal(err)
	}
	h := holdOpenWithoutShareDelete(t, path)
	defer windows.CloseHandle(h)
	err := Save(path, sealOne(t, "second"))
	if err == nil {
		t.Fatal("Save succeeded while the vault was held open")
	}
	if !isTransient(err) || !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), "3 attempts") {
		t.Errorf("Save error = %v, want a wrapped sharing error naming the path and the attempts", err)
	}
	var ee *ExistsError
	if errors.As(err, &ee) {
		t.Errorf("Save error is an *ExistsError: %v", err)
	}
	assertNoTempFiles(t, filepath.Dir(path))
}
