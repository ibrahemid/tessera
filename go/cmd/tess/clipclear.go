package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"time"

	"github.com/spf13/cobra"
)

// startClipClear schedules the clipboard wipe, replaceable in tests.
var startClipClear = spawnClipClear

// spawnClipClear launches a detached `tess __clip-clear` so the shell gets its
// prompt back while the wipe waits out its delay.
func spawnClipClear(seconds int, codeText string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate tess: %w", err)
	}
	child := exec.Command(exe, "__clip-clear", strconv.Itoa(seconds), digestCode(codeText))
	child.Stdin, child.Stdout, child.Stderr = nil, nil, nil
	child.SysProcAttr = detachedProcAttr()
	return child.Start()
}

// digestCode fingerprints a code so the waiting child can tell "the clipboard
// still holds what I was spawned for" from "the user copied something else"
// without carrying the code itself.
func digestCode(codeText string) string {
	sum := sha256.Sum256([]byte(codeText))
	return hex.EncodeToString(sum[:])
}

// newClipClearCmd is the detached half of --clear. It is not part of the user
// interface: tess spawns it for itself.
func newClipClearCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "__clip-clear <seconds> <digest>",
		Short:  "Clear the clipboard after a delay (spawned by tess)",
		Hidden: true,
		Args:   cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			seconds, err := strconv.Atoi(args[0])
			if err != nil || seconds < 0 {
				return fmt.Errorf("invalid delay %q", args[0])
			}
			if seconds > 0 {
				sleepFn(time.Duration(seconds) * time.Second)
			}
			return clearClipboardIfUnchanged(args[1])
		},
	}
}

// clearClipboardIfUnchanged wipes the clipboard only while it still holds the
// code this child was spawned for, so whatever the user copied since survives.
func clearClipboardIfUnchanged(digest string) error {
	current, err := readClipboard()
	if err != nil {
		return fmt.Errorf("read clipboard: %w", err)
	}
	if digestCode(current) != digest {
		return nil
	}
	if err := copyToClipboard(""); err != nil {
		return fmt.Errorf("clear clipboard: %w", err)
	}
	return nil
}
