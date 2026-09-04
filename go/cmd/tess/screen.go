package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ibrahemid/tessera/go/internal/account"
)

// errScreenCaptureUnsupported reports a platform with no interactive capture.
var errScreenCaptureUnsupported = errors.New("--screen needs macOS; save the QR to a file and run `tess add <file>`")

// captureScreenPNG is the interactive screen capture, replaceable in tests.
var captureScreenPNG = captureScreen

// addFromScreen grabs a QR code off the screen and decodes every code in the
// selection. The PNG holds a cleartext secret, so it lives in a private
// directory for the length of the call and no longer.
func addFromScreen() ([]account.Account, []importProblem, error) {
	dir, err := os.MkdirTemp("", "tessera-screen-")
	if err != nil {
		return nil, nil, fmt.Errorf("create scratch directory: %w", err)
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "capture.png")

	captureErr := captureScreenPNG(path)
	if errors.Is(captureErr, errScreenCaptureUnsupported) {
		return nil, nil, captureErr
	}
	// screencapture exits non-zero and writes nothing when the user presses
	// Esc, so an absent file is a cancel rather than a failure.
	if _, statErr := os.Stat(path); statErr != nil {
		return nil, nil, fmt.Errorf("capture cancelled")
	}
	if captureErr != nil {
		return nil, nil, captureErr
	}
	return addFromImage(path)
}
