//go:build !darwin

package main

func captureScreen(path string) error {
	return errScreenCaptureUnsupported
}
