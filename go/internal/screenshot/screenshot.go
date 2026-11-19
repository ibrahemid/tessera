// Package screenshot captures a user-selected region of the screen as a PNG:
// screencapture on macOS; on Linux the desktop screenshot portal, then grim
// and slurp or spectacle under Wayland, or maim, scrot, import,
// gnome-screenshot or spectacle under X11. Every tool runs as a child process
// with the passphrase variables removed from its environment.
package screenshot

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const (
	scratchPrefix = "tessera-screen-"
	captureName   = "capture.png"
	fileHint      = "save the QR to a file and run `tess add <file>`"
)

const (
	sessionWayland = "wayland"
	sessionX11     = "x11"
)

// ErrUnsupported reports a system with no interactive screen capture.
var ErrUnsupported = errors.New("--screen needs macOS or Linux; " + fileHint)

// ErrCancelled reports that the user dismissed the selection.
var ErrCancelled = errors.New("capture cancelled")

// NoCaptureError reports a Linux session with no usable capture method.
type NoCaptureError struct {
	// Session is "wayland", "x11", or "" when neither WAYLAND_DISPLAY nor
	// DISPLAY is set.
	Session string
	// Missing lists the tools that would provide a capture in this session.
	Missing []string
}

func (e *NoCaptureError) Error() string {
	var where string
	switch e.Session {
	case sessionWayland:
		where = "Wayland"
	case sessionX11:
		where = "X11"
	default:
		return "--screen needs a Wayland or X11 session; " + fileHint
	}
	if len(e.Missing) == 0 {
		return fmt.Sprintf("--screen found no capture tool for %s; %s", where, fileHint)
	}
	return fmt.Sprintf("--screen found no capture tool for %s; install %s, or %s", where, andList(e.Missing), fileHint)
}

// Is matches ErrUnsupported.
func (e *NoCaptureError) Is(target error) bool { return target == ErrUnsupported }

// CaptureError reports a capture tool or the screenshot portal failing.
type CaptureError struct {
	// Tool is the program that failed, or "the desktop portal".
	Tool string
	// Detail is the first line the tool wrote to stderr, or what the portal
	// answered.
	Detail string
	Err    error
}

func (e *CaptureError) Error() string {
	msg := "screen capture with " + e.Tool + " failed"
	switch {
	case e.Detail != "":
		msg += ": " + e.Detail
	case e.Err != nil:
		msg += ": " + e.Err.Error()
	}
	return msg + "; " + fileHint
}

func (e *CaptureError) Unwrap() error { return e.Err }

// ScratchDir creates a fresh private directory for one capture. On Linux it
// lives under XDG_RUNTIME_DIR when that directory is absolute, owned by the
// user and mode 0700; otherwise under os.TempDir. The caller removes it with
// os.RemoveAll.
func ScratchDir() (string, error) {
	dir, err := os.MkdirTemp(scratchBase(), scratchPrefix)
	if err != nil {
		return "", fmt.Errorf("screenshot: create scratch directory: %w", err)
	}
	return dir, nil
}

// runtimeDir returns dir when it is an absolute path to a directory, not a
// symlink, owned by uid and mode 0700; otherwise "".
func runtimeDir(dir string, uid int, lstat func(string) (fs.FileInfo, error), ownerOf func(fs.FileInfo) (int, bool)) string {
	if dir == "" || !filepath.IsAbs(dir) {
		return ""
	}
	fi, err := lstat(dir)
	if err != nil || !fi.IsDir() || fi.Mode().Perm() != 0o700 {
		return ""
	}
	if owner, ok := ownerOf(fi); !ok || owner != uid {
		return ""
	}
	return dir
}

func andList(items []string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	}
	return strings.Join(items[:len(items)-1], ", ") + " and " + items[len(items)-1]
}
