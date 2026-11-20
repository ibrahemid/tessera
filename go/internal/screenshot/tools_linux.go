//go:build linux

package screenshot

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/ibrahemid/tessera/go/internal/procenv"
)

const (
	toolGrim            = "grim"
	toolSlurp           = "slurp"
	toolSpectacle       = "spectacle"
	toolMaim            = "maim"
	toolScrot           = "scrot"
	toolImport          = "import"
	toolGnomeScreenshot = "gnome-screenshot"
)

// pipeGrace keeps Wait from blocking on a pipe that a forked helper holds open.
const pipeGrace = 100 * time.Millisecond

const maxOutput = 4 << 10

type captureTool interface {
	capture(ctx context.Context, out string) error
}

type selectSpec struct {
	name          string
	args          func(out string) []string
	cancelMarkers []string
}

var (
	spectacleSpec = selectSpec{name: toolSpectacle, args: func(out string) []string { return []string{"-b", "-n", "-r", "-o", out} }}
	waylandTools  = []selectSpec{spectacleSpec}
	x11Tools      = []selectSpec{
		{name: toolMaim, args: func(out string) []string { return []string{"-s", out} }, cancelMarkers: []string{"cancelled"}},
		{name: toolScrot, args: func(out string) []string { return []string{"-s", out} }, cancelMarkers: []string{"aborting selection"}},
		{name: toolImport, args: func(out string) []string { return []string{out} }},
		{name: toolGnomeScreenshot, args: func(out string) []string { return []string{"-a", "-f", out} }},
		spectacleSpec,
	}
)

func findTool(e env, session string) (captureTool, bool) {
	specs := x11Tools
	if session == sessionWayland {
		grim, grimErr := e.lookPath(toolGrim)
		slurp, slurpErr := e.lookPath(toolSlurp)
		if grimErr == nil && slurpErr == nil {
			return grimSlurp{grim: grim, slurp: slurp}, true
		}
		specs = waylandTools
	}
	for _, s := range specs {
		if p, err := e.lookPath(s.name); err == nil {
			return selectTool{spec: s, path: p}, true
		}
	}
	return nil, false
}

func missingTools(session string) []string {
	if session == sessionWayland {
		return []string{toolGrim, toolSlurp}
	}
	return []string{toolMaim}
}

// selectTool runs one program that shows its own selection UI and writes the
// PNG to the path its arguments name.
type selectTool struct {
	spec selectSpec
	path string
}

func (t selectTool) capture(ctx context.Context, out string) error {
	detail, runErr := runTool(ctx, t.path, t.spec.args(out), nil)
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("screenshot: %w", err)
	}
	written := wroteFile(out)
	switch {
	case written && runErr == nil:
		return nil
	case runErr == nil:
		return ErrCancelled
	case !written && hasMarker(detail, t.spec.cancelMarkers):
		return ErrCancelled
	}
	return &CaptureError{Tool: t.spec.name, Detail: detail, Err: runErr}
}

// grimSlurp runs slurp for the selection, then grim for that geometry.
type grimSlurp struct {
	grim  string
	slurp string
}

func (t grimSlurp) capture(ctx context.Context, out string) error {
	geometry := &cappedBuffer{max: maxOutput}
	detail, err := runTool(ctx, t.slurp, nil, geometry)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return fmt.Errorf("screenshot: %w", ctxErr)
	}
	if err != nil {
		if hasMarker(detail, []string{"cancelled"}) {
			return ErrCancelled
		}
		return &CaptureError{Tool: toolSlurp, Detail: detail, Err: err}
	}
	g := firstLine(geometry.buf)
	if g == "" {
		return &CaptureError{Tool: toolSlurp, Detail: "no selection was returned"}
	}
	detail, err = runTool(ctx, t.grim, []string{"-g", g, out}, nil)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return fmt.Errorf("screenshot: %w", ctxErr)
	}
	if err != nil {
		return &CaptureError{Tool: toolGrim, Detail: detail, Err: err}
	}
	if !wroteFile(out) {
		return &CaptureError{Tool: toolGrim, Detail: "no image was written"}
	}
	return nil
}

// runTool runs one capture program with stdin on the null device and returns
// the first line it wrote to stderr.
func runTool(ctx context.Context, path string, args []string, stdout io.Writer) (string, error) {
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = procenv.Child()
	cmd.Stdout = stdout
	stderr := &cappedBuffer{max: maxOutput}
	cmd.Stderr = stderr
	cmd.WaitDelay = pipeGrace
	err := cmd.Run()
	if errors.Is(err, exec.ErrWaitDelay) {
		err = nil
	}
	return firstLine(stderr.buf), err
}

func wroteFile(path string) bool {
	fi, err := os.Lstat(path)
	return err == nil && fi.Mode().IsRegular() && fi.Size() > 0
}

func hasMarker(detail string, markers []string) bool {
	d := strings.ToLower(detail)
	for _, m := range markers {
		if strings.Contains(d, m) {
			return true
		}
	}
	return false
}

type cappedBuffer struct {
	buf []byte
	max int
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	if room := b.max - len(b.buf); room > 0 {
		b.buf = append(b.buf, p[:min(len(p), room)]...)
	}
	return len(p), nil
}

func firstLine(b []byte) string {
	for line := range bytes.Lines(b) {
		if s := string(bytes.TrimSpace(line)); s != "" {
			return s
		}
	}
	return ""
}
