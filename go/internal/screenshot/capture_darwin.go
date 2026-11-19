//go:build darwin

package screenshot

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/ibrahemid/tessera/go/internal/procenv"
)

// Capture runs the macOS interactive selection capture and returns the PNG it
// wrote inside scratchDir. -i is the drag-to-select UI, -x silences the
// shutter sound.
func Capture(ctx context.Context, scratchDir string) (string, error) {
	path := filepath.Join(scratchDir, captureName)
	cmd := exec.CommandContext(ctx, "screencapture", "-i", "-x", path)
	cmd.Env = procenv.Child()
	runErr := cmd.Run()
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("screenshot: %w", err)
	}
	// screencapture exits non-zero and writes nothing when the user presses
	// Esc, so an absent file is a cancel rather than a failure.
	if _, err := os.Stat(path); err != nil {
		return "", ErrCancelled
	}
	if runErr != nil {
		return "", &CaptureError{Tool: "screencapture", Err: runErr}
	}
	return path, nil
}

func scratchBase() string { return "" }
