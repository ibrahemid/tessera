// Package clipboard copies text to the system clipboard through the tools the
// platform provides: pbcopy and NSPasteboard on macOS; wl-clipboard, xclip or
// xsel on Linux and other Unix systems, with OSC 52 through the terminal as the
// last resort. Every tool runs as a child process with the passphrase
// variables removed from its environment.
package clipboard

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// Clipboard is one system clipboard backend.
type Clipboard interface {
	// Name identifies the backend: "pbcopy", "wl-clipboard", "xclip", "xsel",
	// "OSC 52" or "Windows clipboard".
	Name() string
	// Write replaces the clipboard contents with text. sensitive asks the
	// backend to keep the text out of clipboard history where it can.
	Write(text string, sensitive bool) error
	// Read returns the clipboard text; an empty clipboard, or one holding no
	// text, reads as "".
	Read() (string, error)
	// Clear empties the clipboard.
	Clear() error
}

// OverrideEnv names the variable that forces a backend: wayland, x11 or osc52.
const OverrideEnv = "TESSERA_CLIPBOARD"

const (
	overrideWayland = "wayland"
	overrideX11     = "x11"
	overrideOSC52   = "osc52"
)

// ErrReadUnsupported reports a backend that cannot read the clipboard (OSC 52).
var ErrReadUnsupported = errors.New("clipboard: this clipboard cannot be read back")

// ErrClearUnsupported reports a backend that cannot clear the clipboard (OSC 52).
var ErrClearUnsupported = errors.New("clipboard: this clipboard cannot be cleared")

// NoClipboardError reports that no clipboard backend is usable here.
type NoClipboardError struct {
	// Session is "wayland", "x11", "osc52" (forced, with no terminal) or ""
	// when no display is set.
	Session string
	// Missing lists the tools that would provide a clipboard in this session.
	Missing []string
}

func (e *NoClipboardError) Error() string {
	if len(e.Missing) == 0 {
		if e.Session == overrideOSC52 {
			return "clipboard: OSC 52 needs a terminal; run tess in one, or use --no-copy"
		}
		return "clipboard: no clipboard on this system; use --no-copy"
	}
	where := ""
	switch e.Session {
	case overrideWayland:
		where = " for the Wayland session"
	case overrideX11:
		where = " for the X11 session"
	}
	return fmt.Sprintf("clipboard: no clipboard tool found%s; install %s, or use --no-copy", where, orList(e.Missing))
}

// InvalidOverrideError reports a TESSERA_CLIPBOARD value that names no backend.
type InvalidOverrideError struct {
	Value string
}

func (e *InvalidOverrideError) Error() string {
	return fmt.Sprintf("clipboard: %s=%q is not wayland, x11 or osc52; set one of those or unset it", OverrideEnv, e.Value)
}

// OpError reports a clipboard operation that failed.
type OpError struct {
	// Backend is the Name of the clipboard.
	Backend string
	// Op is "write", "read" or "clear".
	Op string
	// Detail is the first line the tool wrote to stderr, if any.
	Detail string
	Err    error
}

func (e *OpError) Error() string {
	msg := fmt.Sprintf("clipboard: %s %s failed", e.Backend, e.Op)
	if e.Detail != "" {
		return msg + ": " + e.Detail
	}
	if e.Err != nil {
		return msg + ": " + e.Err.Error()
	}
	return msg
}

func (e *OpError) Unwrap() error { return e.Err }

// Detect picks the clipboard backend for this process's environment.
func Detect() (Clipboard, error) {
	return platformDetect(systemEnv())
}

type env struct {
	getenv   func(string) string
	lookPath func(string) (string, error)
	openTTY  func() (io.WriteCloser, error)
}

func systemEnv() env {
	return env{getenv: os.Getenv, lookPath: exec.LookPath, openTTY: openDevTTY}
}

func openDevTTY() (io.WriteCloser, error) {
	return os.OpenFile("/dev/tty", os.O_WRONLY, 0)
}

// override returns the backend TESSERA_CLIPBOARD forces, or "" when unset.
func override(e env) (string, error) {
	raw := e.getenv(OverrideEnv)
	v := strings.ToLower(strings.TrimSpace(raw))
	switch v {
	case "", overrideWayland, overrideX11, overrideOSC52:
		return v, nil
	}
	return "", &InvalidOverrideError{Value: raw}
}

func orList(items []string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	}
	return strings.Join(items[:len(items)-1], ", ") + " or " + items[len(items)-1]
}
