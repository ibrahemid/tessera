//go:build darwin

package main

import (
	"fmt"
	"os/exec"

	"github.com/ibrahemid/tessera/go/internal/procenv"
)

// captureScreen runs the macOS interactive selection capture, writing a PNG at
// path. -i is the drag-to-select UI, -x silences the shutter sound.
func captureScreen(path string) error {
	capture := exec.Command("screencapture", "-i", "-x", path)
	capture.Env = procenv.Child()
	if err := capture.Run(); err != nil {
		return fmt.Errorf("screencapture: %w", err)
	}
	return nil
}
