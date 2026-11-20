package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/detect"
	"github.com/ibrahemid/tessera/go/internal/qr"
	"github.com/ibrahemid/tessera/go/internal/screenshot"
)

// captureScreenPNG is the interactive screen capture, replaceable in tests.
var captureScreenPNG = screenshot.Capture

var (
	errNoQROnScreen     = errors.New("no QR code found in the selection; select the whole QR code and try again")
	errScreenTooLarge   = errors.New("the selection is too large to read; select only the QR code")
	errScreenUnreadable = errors.New("the screen capture could not be read; try again")
)

const (
	screenSourceName      = "screen"
	screenSourceNameMulti = "screen (QR %d)"
)

// screenReadError is a capture tess could not read a QR code from. Its message
// says what to do next and leaves out the capture's path and decoder details.
type screenReadError struct {
	reason error
	err    error
}

func (e *screenReadError) Error() string { return e.reason.Error() }

func (e *screenReadError) Unwrap() []error { return []error{e.reason, e.err} }

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
	payloads, err := qr.DecodeFileAll(path)
	if err != nil {
		return nil, nil, &screenReadError{reason: screenReadReason(err), err: err}
	}
	var accts []account.Account
	var problems []importProblem
	for i, payload := range payloads {
		src := screenSourceName
		if len(payloads) > 1 {
			src = fmt.Sprintf(screenSourceNameMulti, i+1)
		}
		parsed, errs := detect.ParseText(payload)
		accts = append(accts, parsed...)
		problems = append(problems, itemProblems(src, errs)...)
	}
	return accts, problems, nil
}

func screenReadReason(err error) error {
	switch {
	case errors.Is(err, qr.ErrNoQRCode):
		return errNoQROnScreen
	case errors.Is(err, qr.ErrTooManyPixels), errors.Is(err, qr.ErrImageFileTooLarge):
		return errScreenTooLarge
	}
	return errScreenUnreadable
}

// removeCapture deletes the capture at path when it is a regular file, never
// what a symlink there points to. A capture left behind holds the secret, so
// a failed delete is a warning naming the file.
func removeCapture(path string) {
	fi, err := os.Lstat(path)
	if err != nil || !fi.Mode().IsRegular() {
		return
	}
	if err := os.Remove(path); err != nil {
		reason := err
		var pathErr *os.PathError
		if errors.As(err, &pathErr) {
			reason = pathErr.Err
		}
		warn(warnOut, fmt.Sprintf("the screen capture at %s holds the secret and could not be deleted (%v); delete it", path, reason))
	}
}
