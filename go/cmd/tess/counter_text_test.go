package main

import (
	"bytes"
	"errors"
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/tui"
)

// captureWatchAdvance runs tess watch and returns the counter advance it hands
// to the view.
func captureWatchAdvance(t *testing.T) tui.AdvanceFunc {
	t.Helper()
	var advance tui.AdvanceFunc
	old := runWatchView
	runWatchView = func(_ []account.Account, fn tui.AdvanceFunc, _ tui.ClearFunc) error {
		advance = fn
		return nil
	}
	t.Cleanup(func() { runWatchView = old })
	if err := runWatchCmd(t, ""); err != nil {
		t.Fatalf("watch: %v", err)
	}
	return advance
}

func TestWatchAdvanceChecksTheStoredCounter(t *testing.T) {
	path := withVault(t)
	h := hotpAccount(5)
	m := hotpAccount(account.MaxCounter)
	m.ID, m.Issuer, m.Handle = "m", "Max", "ma"
	sealVault(t, path, []account.Account{h, m})
	advance := captureWatchAdvance(t)

	if err := advance("a", 6); err != nil {
		t.Fatalf("advance: %v", err)
	}
	if a := ids(openVaultFile(t, path, "testpass123"))["a"]; a.Counter != 6 {
		t.Fatalf("counter = %d", a.Counter)
	}
	if err := advance("a", 6); err == nil || err.Error() != "the counter changed in another tess process; restart tess watch" {
		t.Fatalf("a stale advance: %v", err)
	}
	if err := advance("m", math.MaxInt64); !errors.Is(err, account.ErrCounterRange) {
		t.Fatalf("advance past the maximum: %v", err)
	}
	if err := advance("zz", 1); err == nil || err.Error() != "account is no longer in the vault" {
		t.Fatalf("a missing account: %v", err)
	}
	if _, _, err := runRootStreams(t, "rm", "gi"); err != nil {
		t.Fatalf("rm: %v", err)
	}
	before := readVaultBytes(t, path)
	if err := advance("a", 7); err == nil || err.Error() != "account is in the Trash" {
		t.Fatalf("a trashed account: %v", err)
	}
	if !bytes.Equal(before, readVaultBytes(t, path)) {
		t.Fatal("a rejected advance rewrote the vault")
	}
}

func TestCodeNextStopsAtTheMaximumCounter(t *testing.T) {
	path := withVault(t)
	sealVault(t, path, []account.Account{hotpAccount(account.MaxCounter)})
	before := readVaultBytes(t, path)
	_, stderr, err := runRootStreams(t, "code", "gi", "--next", "--now")
	if !errors.Is(err, account.ErrCounterRange) {
		t.Fatalf("err = %v", err)
	}
	if !strings.HasPrefix(stderr, "error: cannot advance the counter of GitHub (me) (") ||
		!strings.HasSuffix(stderr, "; `tess edit gi --counter N` sets a new one\n") {
		t.Errorf("stderr = %q", stderr)
	}
	if !bytes.Equal(before, readVaultBytes(t, path)) {
		t.Fatal("the vault was rewritten")
	}

	sealVault(t, path, []account.Account{hotpAccount(account.MaxCounter - 1)})
	if _, _, err := runRootStreams(t, "code", "gi", "--next", "--now"); err != nil {
		t.Fatalf("the last counter value: %v", err)
	}
	if a := ids(openVaultFile(t, path, "testpass123"))["a"]; a.Counter != account.MaxCounter {
		t.Fatalf("counter = %d", a.Counter)
	}
}

func TestAddCleansTypedText(t *testing.T) {
	path := withVault(t)
	sealVault(t, path, nil)
	_, _, err := runRootStreams(t, "add", "JBSWY3DPEHPK3PXP",
		"--issuer", " \x1b]8;;x\x07Acme\u202e ", "--account", "me\x00\t", "--folder", "\u2066work\x7f")
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	got := openVaultFile(t, path, "testpass123")
	if len(got) != 1 || got[0].Issuer != "]8;;xAcme" || got[0].Account != "me" || got[0].Folder != "work" {
		t.Fatalf("added = %+v", got)
	}
}

func TestAddCleansThePromptedIssuer(t *testing.T) {
	path := withVault(t)
	sealVault(t, path, nil)
	stubStdinTTY(t, true)
	stubPrompts(t, nil, []string{" \x1b[31mBank\x07 "})
	if _, _, err := runRootStreams(t, "add", "JBSWY3DPEHPK3PXP"); err != nil {
		t.Fatalf("add: %v", err)
	}
	if got := openVaultFile(t, path, "testpass123"); len(got) != 1 || got[0].Issuer != "[31mBank" {
		t.Fatalf("added = %+v", got)
	}
}

func TestMoveAndTagCleanTheirValue(t *testing.T) {
	path := sealGitHub(t)
	if _, _, err := runRootStreams(t, "move", "gi", "\x1b[2Jwork\t"); err != nil {
		t.Fatalf("move: %v", err)
	}
	if _, _, err := runRootStreams(t, "tag", "gi", "x\u202ey"); err != nil {
		t.Fatalf("tag: %v", err)
	}
	a := ids(openVaultFile(t, path, "testpass123"))["a"]
	if a.Folder != "[2Jwork" || !slices.Equal(a.Tags, []string{"xy"}) {
		t.Fatalf("account = %+v", a)
	}
}

func TestShowNamesTheAlias(t *testing.T) {
	sealGitHub(t)
	stdout, _, err := runRootStreams(t, "show", "gi")
	if err != nil {
		t.Fatalf("show: %v", err)
	}
	if !strings.Contains(stdout, "\nalias      gi\n") && !strings.HasPrefix(stdout, "alias      gi\n") {
		t.Fatalf("show lacks the alias row: %q", stdout)
	}
	if strings.Contains(stdout, "handle") {
		t.Errorf("show still says handle: %q", stdout)
	}
}
