package main

import (
	"context"
	"os"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/screenshot"
)

// captureScreenPNG is the interactive screen capture, replaceable in tests.
var captureScreenPNG = screenshot.Capture

// addFromScreen grabs a QR code off the screen and decodes every code in the
// selection. The PNG holds a cleartext secret, so it lives for the length of
// the call and no longer: the scratch directory is removed, and so is a
// capture the desktop portal saved elsewhere.
func addFromScreen(ctx context.Context) ([]account.Account, []importProblem, error) {
	dir, err := screenshot.ScratchDir()
	if err != nil {
		return nil, nil, err
	}
	defer os.RemoveAll(dir)

	path, err := captureScreenPNG(ctx, dir)
	if err != nil {
		return nil, nil, err
	}
	defer removeCapture(path)
	return addFromImage(path)
}

// removeCapture deletes the capture at path when it is a regular file, never
// what a symlink there points to.
func removeCapture(path string) {
	if fi, err := os.Lstat(path); err == nil && fi.Mode().IsRegular() {
		os.Remove(path)
	}
}
