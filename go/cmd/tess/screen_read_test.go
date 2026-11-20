package main

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ibrahemid/tessera/go/internal/qr"
)

func writeBlankPNG(t *testing.T, path string) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 120, 120))
	for y := range 120 {
		for x := range 120 {
			img.Set(x, y, color.White)
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

func TestAddScreenWithNoQRSaysSoPlainly(t *testing.T) {
	path := withVault(t)
	sealVault(t, path, nil)
	var capturedTo string
	stubScreenCapture(t, func(_ context.Context, dir string) (string, error) {
		capturedTo = filepath.Join(dir, "capture.png")
		writeBlankPNG(t, capturedTo)
		return capturedTo, nil
	})

	_, err := runAdd(t, "--screen")
	if !errors.Is(err, errNoQROnScreen) || !errors.Is(err, qr.ErrNoQRCode) {
		t.Fatalf("err = %v, want errNoQROnScreen", err)
	}
	msg := userMessage(err)
	if msg != errNoQROnScreen.Error() {
		t.Fatalf("message = %q, want %q", msg, errNoQROnScreen.Error())
	}
	if strings.Contains(msg, capturedTo) || strings.Contains(msg, filepath.Dir(capturedTo)) {
		t.Fatalf("the message names the capture: %q", msg)
	}
	assertGone(t, "capture", capturedTo)
}

func TestScreenReadReason(t *testing.T) {
	cases := []struct {
		err  error
		want error
	}{
		{fmt.Errorf("decode: %w", qr.ErrNoQRCode), errNoQROnScreen},
		{fmt.Errorf("decode: %w", qr.ErrTooManyPixels), errScreenTooLarge},
		{fmt.Errorf("decode: %w", qr.ErrImageFileTooLarge), errScreenTooLarge},
		{errors.New("image: unknown format"), errScreenUnreadable},
	}
	for _, c := range cases {
		if got := screenReadReason(c.err); got != c.want {
			t.Errorf("screenReadReason(%v) = %v, want %v", c.err, got, c.want)
		}
	}
}

func TestAddScreenNamesEachQRInTheSelection(t *testing.T) {
	path := withVault(t)
	sealVault(t, path, nil)
	stubScreenCapture(t, func(_ context.Context, dir string) (string, error) {
		p := filepath.Join(dir, "capture.png")
		writeTwoQRPNG(t, p, screenURI, "not an account")
		return p, nil
	})

	accts, problems, err := addFromScreen(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(accts) != 1 || accts[0].Issuer != "Screen" {
		t.Fatalf("accounts = %+v", accts)
	}
	if len(problems) != 1 {
		t.Fatalf("problems = %+v", problems)
	}
	if src := problems[0].source; src != "screen (QR 1) line 1" && src != "screen (QR 2) line 1" {
		t.Fatalf("problem source = %q, want the QR code's place in the selection", src)
	}
}

func TestAddScreenWarnsWhenTheCaptureCannotBeDeleted(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a directory the test user cannot write")
	}
	path := withVault(t)
	sealVault(t, path, nil)
	portal := t.TempDir()
	capturedTo := filepath.Join(portal, "capture.png")
	stubScreenCapture(t, func(context.Context, string) (string, error) {
		if err := qr.EncodePNG(screenURI, capturedTo, 256); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(portal, 0o500); err != nil {
			t.Fatal(err)
		}
		return capturedTo, nil
	})
	t.Cleanup(func() { _ = os.Chmod(portal, 0o700) })
	warnings := captureWarnings(t)

	if _, err := runAdd(t, "--screen"); err != nil {
		t.Fatalf("add --screen: %v", err)
	}
	want := "warning: the screen capture at " + capturedTo + " holds the secret and could not be deleted (permission denied); delete it\n"
	if warnings.String() != want {
		t.Fatalf("warning = %q, want %q", warnings.String(), want)
	}
	assertAdded(t, "Screen")
}
