//go:build linux

package screenshot

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

// Capture asks the desktop screenshot portal for an interactive capture and
// falls back to the capture tools for the session. It returns the PNG path:
// inside scratchDir for a tool, wherever the portal saved it otherwise. The
// caller removes it.
func Capture(ctx context.Context, scratchDir string) (string, error) {
	return capture(ctx, scratchDir, systemEnv())
}

type env struct {
	getenv   func(string) string
	lookPath func(string) (string, error)
	portal   func(context.Context) (string, error)
}

func systemEnv() env {
	return env{getenv: os.Getenv, lookPath: exec.LookPath, portal: capturePortal}
}

func capture(ctx context.Context, scratchDir string, e env) (string, error) {
	session := sessionType(e)
	if session == "" {
		return "", &NoCaptureError{}
	}
	path, err := e.portal(ctx)
	if !errors.Is(err, errNoPortal) {
		return path, err
	}
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("screenshot: %w", err)
	}
	t, ok := findTool(e, session)
	if !ok {
		return "", &NoCaptureError{Session: session, Missing: missingTools(session)}
	}
	out := filepath.Join(scratchDir, captureName)
	if err := t.capture(ctx, out); err != nil {
		return "", err
	}
	return out, nil
}

func sessionType(e env) string {
	switch {
	case e.getenv("WAYLAND_DISPLAY") != "":
		return sessionWayland
	case e.getenv("DISPLAY") != "":
		return sessionX11
	}
	return ""
}

func scratchBase() string {
	return runtimeDir(os.Getenv("XDG_RUNTIME_DIR"), os.Getuid(), os.Lstat, fileOwner)
}

func fileOwner(fi fs.FileInfo) (int, bool) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return int(st.Uid), true
}
