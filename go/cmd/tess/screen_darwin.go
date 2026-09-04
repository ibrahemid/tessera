//go:build darwin

package main

import (
	"fmt"
	"os/exec"
)

// captureScreen runs the macOS interactive selection capture, writing a PNG at
// path. -i is the drag-to-select UI, -x silences the shutter sound.
func captureScreen(path string) error {
	if err := exec.Command("screencapture", "-i", "-x", path).Run(); err != nil {
		return fmt.Errorf("screencapture: %w", err)
	}
	return nil
}
