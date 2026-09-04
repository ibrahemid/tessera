package main

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/qr"
)

// stubScreenCapture replaces the interactive capture for one test.
func stubScreenCapture(t *testing.T, fn func(string) error) {
	t.Helper()
	old := captureScreenPNG
	captureScreenPNG = fn
	t.Cleanup(func() { captureScreenPNG = old })
}

func runAdd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	c := newAddCmd()
	var buf bytes.Buffer
	c.SetOut(&buf)
	c.SetErr(&bytes.Buffer{})
	c.SetArgs(args)
	err := c.Execute()
	return buf.String(), err
}

const screenURI = "otpauth://totp/Screen:me?secret=JBSWY3DPEHPK3PXP&issuer=Screen"

func TestAddScreenRoutesTheCaptureThroughTheAddPath(t *testing.T) {
	path := withVault(t)
	sealVault(t, path, nil)
	var capturedTo string
	stubScreenCapture(t, func(dst string) error {
		capturedTo = dst
		return qr.EncodePNG(screenURI, dst, 256)
	})

	out, err := runAdd(t, "--screen")
	if err != nil {
		t.Fatalf("add --screen: %v", err)
	}
	if !strings.Contains(out, "Screen") {
		t.Errorf("output should name the added account, got %q", out)
	}
	s, err := openSession()
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	if len(s.accounts) != 1 || s.accounts[0].Issuer != "Screen" {
		t.Fatalf("account not added: %+v", s.accounts)
	}
	if _, err := os.Stat(capturedTo); err == nil {
		t.Errorf("the capture at %s still exists; it holds a cleartext secret", capturedTo)
	}
}

// TestAddScreenCancelReportsCancellation: pressing Esc leaves no file behind,
// and that is not a failure worth a stack of decoder errors.
func TestAddScreenCancelReportsCancellation(t *testing.T) {
	path := withVault(t)
	sealVault(t, path, nil)
	stubScreenCapture(t, func(string) error { return errors.New("exit status 1") })

	_, err := runAdd(t, "--screen")
	if err == nil || !strings.Contains(err.Error(), "cancelled") {
		t.Fatalf("expected a cancellation error, got %v", err)
	}
}

// TestAddScreenSurfacesAnUnsupportedPlatform keeps the "run this elsewhere"
// message from being flattened into "capture cancelled".
func TestAddScreenSurfacesAnUnsupportedPlatform(t *testing.T) {
	path := withVault(t)
	sealVault(t, path, nil)
	stubScreenCapture(t, func(string) error { return errScreenCaptureUnsupported })

	_, err := runAdd(t, "--screen")
	if !errors.Is(err, errScreenCaptureUnsupported) {
		t.Fatalf("expected the unsupported-platform error, got %v", err)
	}
}

// TestAddStdinParsesThePipeline covers `... | tess add -`.
func TestAddStdinParsesThePipeline(t *testing.T) {
	path := withVault(t)
	sealVault(t, path, nil)
	stubStdin(t, screenURI+"\n")

	if _, err := runAdd(t, "-"); err != nil {
		t.Fatalf("add -: %v", err)
	}
	s, err := openSession()
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	if len(s.accounts) != 1 || s.accounts[0].Issuer != "Screen" {
		t.Fatalf("stdin account not added: %+v", s.accounts)
	}
}

func TestAddStdinReportsUnparsableInput(t *testing.T) {
	path := withVault(t)
	sealVault(t, path, []account.Account{})
	stubStdin(t, "not an otpauth uri\n")

	out, err := runAdd(t, "-")
	if err == nil {
		t.Fatal("unparsable stdin must not exit zero")
	}
	if !strings.Contains(out, "stdin") {
		t.Errorf("the problem should be attributed to stdin, got %q", out)
	}
}
