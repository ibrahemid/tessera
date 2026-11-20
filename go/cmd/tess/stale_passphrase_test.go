package main

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/secretstore"
	"github.com/ibrahemid/tessera/go/internal/secretstore/secretstoretest"
)

// readOnlyStore finds entries but refuses every write.
type readOnlyStore struct{ *secretstoretest.Fake }

func (readOnlyStore) Store(context.Context, string, string, time.Duration) error {
	return errors.New("this store is read-only")
}

func unlockTwice(t *testing.T, path string) (prompts int, questions []string) {
	t.Helper()
	for run := 0; run < 2; run++ {
		p := interactiveUnlock(path)
		p.prompt = func(string) (string, error) { prompts++; return "testpass123", nil }
		p.confirm = func(q string) bool { questions = append(questions, q); return true }
		if _, err := unlockSession(path, p); err != nil {
			t.Fatalf("run %d: %v", run, err)
		}
	}
	return prompts, questions
}

func TestStaleStoredPassphraseIsReplacedWhereItLives(t *testing.T) {
	path := withVault(t)
	sealVault(t, path, []account.Account{totp("a", "ACME", "x", 1)})
	t.Setenv("TESSERA_PASSPHRASE", "")
	first := &secretstoretest.Fake{BackendID: "first", BackendName: "first store"}
	second := &secretstoretest.Fake{BackendID: "second", BackendName: "second store"}
	first.Put(path, "passphrase-before-rekey")
	t.Cleanup(secretstore.SwapForTest(first, second))
	t.Setenv("TESSERA_SECRET_BACKEND", "second")
	stubStdinTTY(t, true)

	prompts, questions := unlockTwice(t, path)
	if prompts != 1 {
		t.Fatalf("%d prompts in two unlocks, want 1; questions: %q", prompts, questions)
	}
	want := []string{"Stored passphrase no longer opens the vault. Update the first store entry? [y/N] "}
	if !slices.Equal(questions, want) {
		t.Fatalf("questions = %q, want %q", questions, want)
	}
	if got, _ := first.Secret(path); got != "testpass123" {
		t.Fatalf("first store holds %q, want the typed passphrase", got)
	}
	if _, ok := second.Secret(path); ok {
		t.Fatal("the second store gained an entry")
	}
}

func TestStaleEntryInAStoreThatRefusesWritesMovesToTheChosenStore(t *testing.T) {
	path := withVault(t)
	sealVault(t, path, []account.Account{totp("a", "ACME", "x", 1)})
	t.Setenv("TESSERA_PASSPHRASE", "")
	first := readOnlyStore{&secretstoretest.Fake{BackendID: "first", BackendName: "first store"}}
	second := &secretstoretest.Fake{BackendID: "second", BackendName: "second store"}
	first.Put(path, "passphrase-before-rekey")
	t.Cleanup(secretstore.SwapForTest(first, second))
	t.Setenv("TESSERA_SECRET_BACKEND", "second")
	stubStdinTTY(t, true)

	prompts, questions := unlockTwice(t, path)
	if prompts != 1 {
		t.Fatalf("%d prompts in two unlocks, want 1; questions: %q", prompts, questions)
	}
	if _, ok := first.Secret(path); ok {
		t.Fatal("the stale entry was left in the first store")
	}
	if got, _ := second.Secret(path); got != "testpass123" {
		t.Fatalf("second store holds %q, want the typed passphrase", got)
	}
}
