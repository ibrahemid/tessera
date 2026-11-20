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
	"syscall"
	"time"

	"github.com/ibrahemid/tessera/go/internal/clipboard"
	"github.com/ibrahemid/tessera/go/internal/procenv"
	"github.com/spf13/cobra"
)

// clipClearDefault is the delay, in seconds, before a code copied on a
// terminal is cleared. The Mac app uses the same interval.
const clipClearDefault = 30

// clipClearEnv names the variable that sets the clear delay in seconds.
const clipClearEnv = "TESSERA_CLIP_CLEAR"

// errClipClearUnsupported reports a clipboard tess cannot read back, so it
// cannot tell its own code from text copied since and leaves it in place.
var errClipClearUnsupported = errors.New("this clipboard cannot be read back, so tess will not clear the code")

// clipClearDelayError reports a clear delay that is not a whole number of
// seconds.
type clipClearDelayError struct {
	// source is "--clear" or the environment variable name.
	source string
	value  string
}

func (e *clipClearDelayError) Error() string {
	return fmt.Sprintf("%s %q is not a number of seconds; use 0 to keep the code on the clipboard, or a positive number", e.source, e.value)
}

// clipClearPlan is when a copied code leaves the clipboard.
type clipClearPlan struct {
	seconds int
	// explicit is true when --clear or $TESSERA_CLIP_CLEAR set the delay, and
	// false for the terminal default.
	explicit bool
}

// resolveClipClear picks the delay: --clear when given, then a non-empty
// $TESSERA_CLIP_CLEAR, then the 30-second default when the copy happens on a
// terminal. Zero keeps the code.
func resolveClipClear(flag int, flagSet, terminal bool) (clipClearPlan, error) {
	if flagSet {
		if flag < 0 {
			return clipClearPlan{}, &clipClearDelayError{source: "--clear", value: strconv.Itoa(flag)}
		}
		return clipClearPlan{seconds: flag, explicit: true}, nil
	}
	if raw := os.Getenv(clipClearEnv); strings.TrimSpace(raw) != "" {
		n, err := strconv.Atoi(strings.TrimSpace(raw))
		if err != nil || n < 0 {
			return clipClearPlan{}, &clipClearDelayError{source: clipClearEnv, value: raw}
		}
		return clipClearPlan{seconds: n, explicit: true}, nil
	}
	if terminal {
		return clipClearPlan{seconds: clipClearDefault}, nil
	}
	return clipClearPlan{}, nil
}

// schedule arranges the clear of codeText, which was just copied. A clipboard
// that cannot be read back is never cleared blind: the default is dropped
// without a word, and an explicit delay returns errClipClearUnsupported.
func (p clipClearPlan) schedule(codeText string) error {
	if p.seconds <= 0 {
		return nil
	}
	if _, err := readClipboard(); errors.Is(err, clipboard.ErrReadUnsupported) {
		if p.explicit {
			return errClipClearUnsupported
		}
		return nil
	}
	if err := startClipClear(p.seconds, codeText); err != nil {
		return fmt.Errorf("schedule clipboard clear: %w", err)
	}
	return nil
}

// startClipClear schedules the clipboard wipe, replaceable in tests.
var startClipClear = spawnClipClear

// startProcess starts one child, replaceable in tests.
var startProcess = (*exec.Cmd).Start

// clearClipboard empties the clipboard, replaceable in tests.
var clearClipboard = func() error {
	b, err := detectClipboard()
	if err != nil {
		return err
	}
	return b.Clear()
}

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
	build := func(attr *syscall.SysProcAttr) *exec.Cmd {
		child := exec.Command(exe, "__clip-clear", strconv.Itoa(seconds))
		child.Env = procenv.Child()
		child.Stdin, child.Stdout, child.Stderr = r, nil, nil
		child.SysProcAttr = attr
		return child
	}
	return startDetached(build, detachedProcAttrs(), spawnRetryable)
}

// startDetached starts a child built with each attribute set in turn, moving
// to the next only when retryable accepts the previous failure.
func startDetached(build func(*syscall.SysProcAttr) *exec.Cmd, attrs []*syscall.SysProcAttr, retryable func(error) bool) error {
	if len(attrs) == 0 {
		attrs = []*syscall.SysProcAttr{nil}
	}
	var err error
	for i, attr := range attrs {
		err = startProcess(build(attr))
		if err == nil || i == len(attrs)-1 || !retryable(err) {
			break
		}
	}
	return err
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
	if err := clearClipboard(); err != nil {
		return fmt.Errorf("clear clipboard: %w", err)
	}
	return nil
}
