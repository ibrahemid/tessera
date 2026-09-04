package main

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

// stubClipboardPair installs a fake clipboard backed by a string the test owns.
func stubClipboardPair(t *testing.T, initial string) *string {
	t.Helper()
	held := initial
	oldRead, oldWrite := readClipboard, copyToClipboard
	readClipboard = func() (string, error) { return held, nil }
	copyToClipboard = func(s string) error { held = s; return nil }
	t.Cleanup(func() { readClipboard, copyToClipboard = oldRead, oldWrite })
	return &held
}

// runClipClear runs the detached half the way tess spawns it: the delay in
// argv, the digest on stdin.
func runClipClear(t *testing.T, digest string, args ...string) error {
	t.Helper()
	old := sleepFn
	sleepFn = func(time.Duration) {}
	t.Cleanup(func() { sleepFn = old })

	c := newClipClearCmd()
	c.SetIn(strings.NewReader(digest + "\n"))
	c.SetOut(io.Discard)
	c.SetErr(&bytes.Buffer{})
	c.SetArgs(args)
	return c.Execute()
}

func TestClipClearWipesTheCodeItWasSpawnedFor(t *testing.T) {
	held := stubClipboardPair(t, "123456")

	if err := runClipClear(t, digestCode("123456"), "5"); err != nil {
		t.Fatalf("__clip-clear: %v", err)
	}
	if *held != "" {
		t.Errorf("clipboard = %q, want it cleared", *held)
	}
}

// TestClipClearLeavesALaterCopyAlone is the whole point of the digest: the user
// copied something else in the meantime and must not lose it.
func TestClipClearLeavesALaterCopyAlone(t *testing.T) {
	held := stubClipboardPair(t, "a password the user copied since")

	if err := runClipClear(t, digestCode("123456"), "5"); err != nil {
		t.Fatalf("__clip-clear: %v", err)
	}
	if *held != "a password the user copied since" {
		t.Errorf("clipboard = %q, want the user's own copy untouched", *held)
	}
}

func TestClipClearWaitsTheRequestedDelay(t *testing.T) {
	stubClipboardPair(t, "123456")
	var slept time.Duration
	old := sleepFn
	sleepFn = func(d time.Duration) { slept = d }
	t.Cleanup(func() { sleepFn = old })

	c := newClipClearCmd()
	c.SetIn(strings.NewReader(digestCode("123456") + "\n"))
	c.SetOut(io.Discard)
	c.SetArgs([]string{"12"})
	if err := c.Execute(); err != nil {
		t.Fatalf("__clip-clear: %v", err)
	}
	if slept != 12*time.Second {
		t.Errorf("slept %v, want 12s", slept)
	}
}

func TestClipClearReportsAnUnreadableClipboard(t *testing.T) {
	old := readClipboard
	readClipboard = func() (string, error) { return "", errors.New("no clipboard") }
	t.Cleanup(func() { readClipboard = old })

	if err := runClipClear(t, digestCode("123456"), "0"); err == nil {
		t.Fatal("an unreadable clipboard must not exit zero")
	}
}

func TestClipClearRejectsABadDelay(t *testing.T) {
	stubClipboardPair(t, "123456")
	if err := runClipClear(t, digestCode("123456"), "soon"); err == nil {
		t.Fatal("a non-numeric delay must be an error")
	}
}

// TestClipClearRefusesAMissingDigest: an empty stdin must not be read as
// permission to wipe whatever the clipboard happens to hold.
func TestClipClearRefusesAMissingDigest(t *testing.T) {
	held := stubClipboardPair(t, "123456")

	c := newClipClearCmd()
	c.SetIn(strings.NewReader(""))
	c.SetOut(io.Discard)
	c.SetErr(&bytes.Buffer{})
	c.SetArgs([]string{"0"})
	if err := c.Execute(); err == nil {
		t.Fatal("a missing digest must be an error")
	}
	if *held != "123456" {
		t.Errorf("clipboard = %q, want it untouched", *held)
	}
}

// TestClipClearTakesNoDigestArgument pins that the digest never reaches argv,
// where `ps` would hand a 6-digit code to anyone who can brute-force a sha256.
func TestClipClearTakesNoDigestArgument(t *testing.T) {
	stubClipboardPair(t, "123456")
	if err := runClipClear(t, digestCode("123456"), "0", digestCode("123456")); err == nil {
		t.Fatal("__clip-clear must reject a digest passed in argv")
	}
}

// TestClipClearIsHidden: it exists for tess to spawn, not for users to run.
func TestClipClearIsHidden(t *testing.T) {
	if !newClipClearCmd().Hidden {
		t.Error("__clip-clear should not appear in the help")
	}
}
