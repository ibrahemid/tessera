package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/qr"
	"github.com/ibrahemid/tessera/go/internal/screenshot"
)

// stubScreenCapture replaces the interactive capture for one test.
func stubScreenCapture(t *testing.T, fn func(context.Context, string) (string, error)) {
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

func assertGone(t *testing.T, what, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the %s at %s still exists (%v); it holds a cleartext secret", what, path, err)
	}
}

func assertAdded(t *testing.T, issuer string) {
	t.Helper()
	s, err := openSession()
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	if len(s.accounts) != 1 || s.accounts[0].Issuer != issuer {
		t.Fatalf("account not added: %+v", s.accounts)
	}
}

func TestAddScreenRoutesTheCaptureThroughTheAddPath(t *testing.T) {
	path := withVault(t)
	sealVault(t, path, nil)
	var scratch, capturedTo string
	stubScreenCapture(t, func(_ context.Context, dir string) (string, error) {
		scratch = dir
		capturedTo = filepath.Join(dir, "capture.png")
		return capturedTo, qr.EncodePNG(screenURI, capturedTo, 256)
	})

	out, err := runAdd(t, "--screen")
	if err != nil {
		t.Fatalf("add --screen: %v", err)
	}
	if !strings.Contains(out, "Screen") {
		t.Errorf("output should name the added account, got %q", out)
	}
	assertAdded(t, "Screen")
	assertGone(t, "capture", capturedTo)
	assertGone(t, "scratch directory", scratch)
}

// TestAddScreenRemovesTheCapture covers a capture inside the scratch directory
// and one the desktop portal saved elsewhere, through success, a decode
// failure and a cancel.
func TestAddScreenRemovesTheCapture(t *testing.T) {
	writeQR := func(path string) error { return qr.EncodePNG(screenURI, path, 256) }
	writeJunk := func(path string) error { return os.WriteFile(path, []byte("not an image"), 0o600) }
	cases := []struct {
		name    string
		outside bool
		write   func(string) error
		err     error
		wantErr string
	}{
		{name: "scratch capture decodes", write: writeQR},
		{name: "scratch capture does not decode", write: writeJunk, wantErr: "capture.png"},
		{name: "portal capture decodes", outside: true, write: writeQR},
		{name: "portal capture does not decode", outside: true, write: writeJunk, wantErr: "capture.png"},
		{name: "cancelled", write: writeJunk, err: screenshot.ErrCancelled, wantErr: "capture cancelled"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := withVault(t)
			sealVault(t, path, nil)
			var scratch, capturedTo string
			stubScreenCapture(t, func(_ context.Context, dir string) (string, error) {
				scratch = dir
				capturedTo = filepath.Join(dir, "capture.png")
				if tc.outside {
					capturedTo = filepath.Join(t.TempDir(), "capture.png")
				}
				if err := tc.write(capturedTo); err != nil {
					t.Fatal(err)
				}
				if tc.err != nil {
					return "", tc.err
				}
				return capturedTo, nil
			})

			_, err := runAdd(t, "--screen")
			switch {
			case tc.wantErr == "" && err != nil:
				t.Fatalf("add --screen: %v", err)
			case tc.wantErr != "" && (err == nil || !strings.Contains(userMessage(err), tc.wantErr)):
				t.Fatalf("add --screen error = %v, want one mentioning %q", err, tc.wantErr)
			case tc.wantErr == "":
				assertAdded(t, "Screen")
			}
			if !tc.outside {
				assertGone(t, "capture", capturedTo)
			} else if tc.err == nil {
				assertGone(t, "portal capture", capturedTo)
			}
			assertGone(t, "scratch directory", scratch)
		})
	}
}

// TestAddScreenCancelReportsCancellation keeps the cancel message tess printed
// before captures moved into the screenshot package.
func TestAddScreenCancelReportsCancellation(t *testing.T) {
	path := withVault(t)
	sealVault(t, path, nil)
	stubScreenCapture(t, func(context.Context, string) (string, error) { return "", screenshot.ErrCancelled })

	_, err := runAdd(t, "--screen")
	if !errors.Is(err, screenshot.ErrCancelled) {
		t.Fatalf("expected the cancellation error, got %v", err)
	}
	if got := userMessage(err); got != "capture cancelled" {
		t.Errorf("message = %q, want %q", got, "capture cancelled")
	}
}

// TestAddScreenSurfacesCaptureErrors keeps the errors that say what to install,
// or to pass an image file, from being flattened into a cancel.
func TestAddScreenSurfacesCaptureErrors(t *testing.T) {
	cases := []error{
		screenshot.ErrUnsupported,
		&screenshot.NoCaptureError{},
		&screenshot.NoCaptureError{Session: "wayland", Missing: []string{"grim", "slurp"}},
		&screenshot.NoCaptureError{Session: "x11", Missing: []string{"maim"}},
		&screenshot.CaptureError{Tool: "grim", Detail: "compositor doesn't support wlr-screencopy-unstable-v1"},
	}
	for _, want := range cases {
		t.Run(want.Error(), func(t *testing.T) {
			path := withVault(t)
			sealVault(t, path, nil)
			var scratch string
			stubScreenCapture(t, func(_ context.Context, dir string) (string, error) {
				scratch = dir
				return "", want
			})

			_, err := runAdd(t, "--screen")
			if !errors.Is(err, want) {
				t.Fatalf("expected %v, got %v", want, err)
			}
			if got := userMessage(err); got != want.Error() {
				t.Errorf("message = %q, want %q", got, want.Error())
			}
			assertGone(t, "scratch directory", scratch)
		})
	}
}

// TestAddScreenLeavesALinkedCaptureAlone: only a regular file is deleted, never
// a symlink or the file it points to.
func TestAddScreenLeavesALinkedCaptureAlone(t *testing.T) {
	path := withVault(t)
	sealVault(t, path, nil)
	outside := t.TempDir()
	target := filepath.Join(outside, "kept.png")
	if err := qr.EncodePNG(screenURI, target, 256); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(outside, "capture.png")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink: %v", err)
	}
	stubScreenCapture(t, func(context.Context, string) (string, error) { return link, nil })

	if _, err := runAdd(t, "--screen"); err != nil {
		t.Fatalf("add --screen: %v", err)
	}
	for _, p := range []string{link, target} {
		if _, err := os.Lstat(p); err != nil {
			t.Errorf("%s was removed: %v", p, err)
		}
	}
}

func TestAddScreenPassesTheCommandContext(t *testing.T) {
	path := withVault(t)
	sealVault(t, path, nil)
	type key struct{}
	var got any
	stubScreenCapture(t, func(ctx context.Context, _ string) (string, error) {
		got = ctx.Value(key{})
		return "", screenshot.ErrCancelled
	})
	c := newAddCmd()
	c.SetOut(&bytes.Buffer{})
	c.SetErr(&bytes.Buffer{})
	c.SetArgs([]string{"--screen"})
	_ = c.ExecuteContext(context.WithValue(context.Background(), key{}, "add"))
	if got != "add" {
		t.Errorf("capture context value = %v, want the command's context", got)
	}
}

// TestAddScreenWaylandEndToEnd runs `tess add --screen` in a sway session: the
// QR is the output background, a fake slurp selects it and the real grim
// captures it.
func TestAddScreenWaylandEndToEnd(t *testing.T) {
	if os.Getenv("TESSERA_SCREEN_TEST") != "wayland" {
		t.Skip("set TESSERA_SCREEN_TEST=wayland inside a sway session to run tess add --screen against grim")
	}
	if runtime.GOOS != "linux" || os.Getenv("WAYLAND_DISPLAY") == "" || os.Getenv("SWAYSOCK") == "" {
		t.Fatal("TESSERA_SCREEN_TEST=wayland needs a Linux sway session with WAYLAND_DISPLAY and SWAYSOCK set")
	}
	const uri = "otpauth://totp/Sway:me?secret=JBSWY3DPEHPK3PXP&issuer=Sway"
	const qrSize, selection = 300, 360

	path := withVault(t)
	sealVault(t, path, nil)
	bg := filepath.Join(t.TempDir(), "qr.png")
	if err := qr.EncodePNG(uri, bg, qrSize); err != nil {
		t.Fatal(err)
	}
	geom := showSwayBackground(t, bg, selection)

	fakes := t.TempDir()
	ran := filepath.Join(fakes, "slurp.ran")
	slurp := "#!/bin/sh\n: > " + shellQuote(ran) + "\nprintf '%s\\n' " + shellQuote(geom) + "\n"
	if err := os.WriteFile(filepath.Join(fakes, "slurp"), []byte(slurp), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fakes+string(os.PathListSeparator)+os.Getenv("PATH"))

	var scratches, captures []string
	capture := captureScreenPNG
	stubScreenCapture(t, func(ctx context.Context, dir string) (string, error) {
		scratches = append(scratches, dir)
		p, err := capture(ctx, dir)
		if p != "" {
			captures = append(captures, p)
		}
		return p, err
	})

	deadline := time.Now().Add(20 * time.Second)
	var out string
	var err error
	for {
		out, err = runAdd(t, "--screen")
		if err == nil {
			break
		}
		var captureErr *screenshot.CaptureError
		if errors.Is(err, screenshot.ErrUnsupported) || errors.Is(err, screenshot.ErrCancelled) || errors.As(err, &captureErr) {
			t.Fatalf("add --screen: %v", err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("no capture decoded to the background QR; last error: %v", err)
		}
		time.Sleep(200 * time.Millisecond)
	}
	if _, err := os.Stat(ran); err != nil {
		t.Fatalf("the fake slurp never ran: %v", err)
	}
	if !strings.Contains(out, "Added Sway") {
		t.Errorf("output = %q, want it to name the added account", out)
	}
	assertAdded(t, "Sway")
	for _, p := range captures {
		assertGone(t, "capture", p)
	}
	for _, d := range scratches {
		assertGone(t, "scratch directory", d)
	}
	t.Logf("added the account from a grim capture at %s after %d attempt(s)", geom, len(scratches))
}

// showSwayBackground sets bg as the first output's centred background and
// returns the slurp geometry of a size x size square at its centre.
func showSwayBackground(t *testing.T, bg string, size int) string {
	t.Helper()
	raw, err := exec.Command("swaymsg", "-r", "-t", "get_outputs").Output()
	if err != nil {
		t.Fatalf("swaymsg get_outputs: %v", err)
	}
	var outputs []struct {
		Name   string `json:"name"`
		Active bool   `json:"active"`
		Rect   struct {
			X      int `json:"x"`
			Y      int `json:"y"`
			Width  int `json:"width"`
			Height int `json:"height"`
		} `json:"rect"`
	}
	if err := json.Unmarshal(raw, &outputs); err != nil || len(outputs) == 0 || !outputs[0].Active {
		t.Fatalf("swaymsg get_outputs = %s, %v", raw, err)
	}
	o := outputs[0]
	if out, err := exec.Command("swaymsg", "output", o.Name, "bg", bg, "center", "#ffffff").CombinedOutput(); err != nil {
		t.Fatalf("swaymsg output bg: %v: %s", err, out)
	}
	return fmt.Sprintf("%d,%d %dx%d", o.Rect.X+(o.Rect.Width-size)/2, o.Rect.Y+(o.Rect.Height-size)/2, size, size)
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
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

	c := newAddCmd()
	var stderr bytes.Buffer
	c.SetOut(&bytes.Buffer{})
	c.SetErr(&stderr)
	c.SetArgs([]string{"-"})
	if err := c.Execute(); err == nil {
		t.Fatal("unparsable stdin must not exit zero")
	}
	if !strings.Contains(stderr.String(), "stdin line 1") {
		t.Errorf("the problem should be attributed to stdin on stderr, got %q", stderr.String())
	}
}
