//go:build !darwin && !linux

package screenshot

import "context"

// Capture reports ErrUnsupported: this system has no interactive capture.
func Capture(context.Context, string) (string, error) {
	return "", ErrUnsupported
}

func scratchBase() string { return "" }
