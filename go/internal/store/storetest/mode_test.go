package storetest_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/ibrahemid/tessera/go/internal/store"
	"github.com/ibrahemid/tessera/go/internal/store/storetest"
)

type recorder struct {
	testing.TB
	failed bool
}

func (r *recorder) Helper()               {}
func (r *recorder) Errorf(string, ...any) { r.failed = true }
func (r *recorder) Fatal(...any)          { r.failed = true; runtime.Goexit() }
func (r *recorder) Fatalf(string, ...any) { r.failed = true; runtime.Goexit() }

func fails(t *testing.T, check func(testing.TB)) bool {
	t.Helper()
	r := &recorder{TB: t}
	done := make(chan struct{})
	go func() {
		defer close(done)
		check(r)
	}()
	<-done
	return r.failed
}

func TestAssertOwnerOnlyAcceptsWhatTheStoreWrites(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "private")
	if err := store.MkdirPrivate(dir); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "secret.txt")
	if err := store.WriteSecretFile(file, []byte("secret"), false); err != nil {
		t.Fatal(err)
	}
	if fails(t, func(tb testing.TB) { storetest.AssertOwnerOnly(tb, file) }) {
		t.Errorf("AssertOwnerOnly rejected a file written by WriteSecretFile")
	}
	if fails(t, func(tb testing.TB) { storetest.AssertOwnerOnlyDir(tb, dir) }) {
		t.Errorf("AssertOwnerOnlyDir rejected a directory made by MkdirPrivate")
	}
}

func TestAssertOwnerOnlyRejectsSharedAndWrongKinds(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "shared")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "shared.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(file, 0o644); err != nil {
		t.Fatal(err)
	}
	for name, check := range map[string]func(testing.TB){
		"shared file":         func(tb testing.TB) { storetest.AssertOwnerOnly(tb, file) },
		"shared directory":    func(tb testing.TB) { storetest.AssertOwnerOnlyDir(tb, dir) },
		"directory as a file": func(tb testing.TB) { storetest.AssertOwnerOnly(tb, dir) },
		"file as a directory": func(tb testing.TB) { storetest.AssertOwnerOnlyDir(tb, file) },
		"missing file":        func(tb testing.TB) { storetest.AssertOwnerOnly(tb, filepath.Join(dir, "missing")) },
		"missing directory":   func(tb testing.TB) { storetest.AssertOwnerOnlyDir(tb, filepath.Join(dir, "missing")) },
	} {
		if !fails(t, check) {
			t.Errorf("%s passed the owner-only check", name)
		}
	}
}
