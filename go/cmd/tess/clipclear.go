package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// startClipClear schedules the clipboard wipe, replaceable in tests.
var startClipClear = spawnClipClear

// spawnClipClear launches a detached `tess __clip-clear` so the shell gets its
// prompt back while the wipe waits out its delay. The digest travels down the
// child's stdin rather than argv, which anyone running `ps` can read.
func spawnClipClear(seconds int, codeText string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate tess: %w", err)
	}
	r, w, err := os.Pipe()
	if err != nil {
		return fmt.Errorf("open digest pipe: %w", err)
	}
	defer r.Close()
	// The digest fits the pipe buffer many times over, so this write completes
	// before there is a child to read it.
	if _, err := fmt.Fprintln(w, digestCode(codeText)); err != nil {
		w.Close()
		return fmt.Errorf("write digest: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("write digest: %w", err)
	}
	child := exec.Command(exe, "__clip-clear", strconv.Itoa(seconds))
	child.Stdin, child.Stdout, child.Stderr = r, nil, nil
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
// interface: tess spawns it for itself and writes the code's digest, as one
// line, into the child's stdin.
func newClipClearCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "__clip-clear <seconds>",
		Short:  "Clear the clipboard after a delay (spawned by tess)",
		Hidden: true,
		Args:   cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			seconds, err := strconv.Atoi(args[0])
			if err != nil || seconds < 0 {
				return fmt.Errorf("invalid delay %q", args[0])
			}
			digest, err := readDigest(cmd.InOrStdin())
			if err != nil {
				return err
			}
			if seconds > 0 {
				sleepFn(time.Duration(seconds) * time.Second)
			}
			return clearClipboardIfUnchanged(digest)
		},
	}
}

// readDigest takes the one line the spawning tess wrote. A missing digest is an
// error: it must never be read as "clear whatever is on the clipboard".
func readDigest(r io.Reader) (string, error) {
	line, err := bufio.NewReader(r).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("read digest: %w", err)
	}
	digest := strings.TrimSpace(line)
	if digest == "" {
		return "", errors.New("read digest: no digest on stdin")
	}
	return digest, nil
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
