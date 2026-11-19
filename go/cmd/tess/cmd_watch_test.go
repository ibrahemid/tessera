package main

import (
	"bytes"
	"errors"
	"testing"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/tui"
)

// stubWatchView captures the clear hook watch hands the view instead of
// opening it.
func stubWatchView(t *testing.T) *tui.ClearFunc {
	t.Helper()
	var got tui.ClearFunc
	old := runWatchView
	runWatchView = func(_ []account.Account, _ tui.AdvanceFunc, clearCopy tui.ClearFunc) error {
		got = clearCopy
		return nil
	}
	t.Cleanup(func() { runWatchView = old })
	return &got
}

func runWatchCmd(t *testing.T, env string) error {
	t.Helper()
	t.Setenv(clipClearEnv, env)
	c := newWatchCmd()
	c.SetOut(&bytes.Buffer{})
	c.SetErr(&bytes.Buffer{})
	c.SetArgs(nil)
	return c.Execute()
}

func TestWatchClearsEachCopyAfterThirtySeconds(t *testing.T) {
	path := withVault(t)
	sealVault(t, path, []account.Account{totp("a", "ACME", "x", 1)})
	stubBackend(t, &fakeClipboard{text: "123456"})
	calls := recordClipClear(t)
	clearCopy := stubWatchView(t)

	if err := runWatchCmd(t, ""); err != nil {
		t.Fatalf("watch: %v", err)
	}
	if *clearCopy == nil {
		t.Fatal("watch must hand the view a clear hook")
	}
	if err := (*clearCopy)("123456"); err != nil {
		t.Fatalf("clear hook: %v", err)
	}
	if len(*calls) != 1 || (*calls)[0] != (clipClearCall{30, "123456"}) {
		t.Errorf("scheduled %+v, want one clear after 30s", *calls)
	}
}

func TestWatchClearEnvironmentZeroKeepsTheCode(t *testing.T) {
	path := withVault(t)
	sealVault(t, path, []account.Account{totp("a", "ACME", "x", 1)})
	stubBackend(t, &fakeClipboard{text: "123456"})
	calls := recordClipClear(t)
	clearCopy := stubWatchView(t)

	if err := runWatchCmd(t, "0"); err != nil {
		t.Fatalf("watch: %v", err)
	}
	if err := (*clearCopy)("123456"); err != nil {
		t.Fatalf("clear hook: %v", err)
	}
	if len(*calls) != 0 {
		t.Errorf("scheduled %+v, want nothing", *calls)
	}
}

func TestWatchRejectsABadClearDelayBeforeOpening(t *testing.T) {
	path := withVault(t)
	sealVault(t, path, []account.Account{totp("a", "ACME", "x", 1)})
	clearCopy := stubWatchView(t)

	err := runWatchCmd(t, "soon")
	var delayErr *clipClearDelayError
	if !errors.As(err, &delayErr) {
		t.Fatalf("err = %v, want a clipClearDelayError", err)
	}
	if *clearCopy != nil {
		t.Error("the view must not open")
	}
}
