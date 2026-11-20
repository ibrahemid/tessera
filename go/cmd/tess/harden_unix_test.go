//go:build unix

package main

import (
	"testing"

	"golang.org/x/sys/unix"
)

func TestHardenProcessTurnsOffCoreDumps(t *testing.T) {
	hardenProcess()
	var lim unix.Rlimit
	if err := unix.Getrlimit(unix.RLIMIT_CORE, &lim); err != nil {
		t.Fatal(err)
	}
	if lim.Cur != 0 || lim.Max != 0 {
		t.Fatalf("core limit = %d/%d, want 0/0", lim.Cur, lim.Max)
	}
}
