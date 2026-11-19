package ui

import (
	"errors"
	"fmt"

	"github.com/charmbracelet/colorprofile"
	"golang.org/x/sys/windows"
)

// ConsoleError reports a standard stream that cannot render escape sequences:
// it is not a console, or the console has no virtual terminal processing.
// Stream is "stdout" or "stderr".
type ConsoleError struct {
	Stream string
	Err    error
}

func (e *ConsoleError) Error() string {
	return fmt.Sprintf("color output is off for %s: %v", e.Stream, e.Err)
}

func (e *ConsoleError) Unwrap() error { return e.Err }

// EnableConsole turns on virtual terminal processing for stdout and stderr
// where they are consoles, so styled output renders as color. When stdout
// cannot render escape sequences, styling falls back to plain text. The error
// joins one *ConsoleError per stream that was left unchanged; restore puts
// back every console mode that was changed.
func EnableConsole() (restore func(), err error) {
	return enableConsole(windows.Stdout, windows.Stderr)
}

func enableConsole(stdout, stderr windows.Handle) (func(), error) {
	var errs []error
	restoreStdout, err := enableVirtualTerminal(stdout)
	if err != nil {
		errs = append(errs, &ConsoleError{Stream: "stdout", Err: err})
		setOutputProfile(colorprofile.NoTTY)
	}
	restoreStderr, err := enableVirtualTerminal(stderr)
	if err != nil {
		errs = append(errs, &ConsoleError{Stream: "stderr", Err: err})
	}
	restore := func() {
		restoreStderr()
		restoreStdout()
	}
	return restore, errors.Join(errs...)
}

func enableVirtualTerminal(h windows.Handle) (restore func(), err error) {
	restore = func() {}
	var mode uint32
	if err := windows.GetConsoleMode(h, &mode); err != nil {
		return restore, err
	}
	if mode&windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING != 0 {
		return restore, nil
	}
	if err := windows.SetConsoleMode(h, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING); err != nil {
		return restore, err
	}
	return func() { _ = windows.SetConsoleMode(h, mode) }, nil
}
