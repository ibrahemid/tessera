package screenshot

import (
	"errors"
	"os/exec"
	"strings"
	"testing"
)

func TestNoCaptureErrorMessages(t *testing.T) {
	cases := []struct {
		err  *NoCaptureError
		want string
	}{
		{&NoCaptureError{}, "--screen needs a Wayland or X11 session; save the QR to a file and run `tess add <file>`"},
		{&NoCaptureError{Session: "wayland", Missing: []string{"grim", "slurp"}},
			"--screen found no capture tool for Wayland; install grim and slurp, or save the QR to a file and run `tess add <file>`"},
		{&NoCaptureError{Session: "x11", Missing: []string{"maim"}},
			"--screen found no capture tool for X11; install maim, or save the QR to a file and run `tess add <file>`"},
		{&NoCaptureError{Session: "x11"},
			"--screen found no capture tool for X11; save the QR to a file and run `tess add <file>`"},
	}
	for _, c := range cases {
		if got := c.err.Error(); got != c.want {
			t.Errorf("Error() = %q, want %q", got, c.want)
		}
		if !errors.Is(c.err, ErrUnsupported) {
			t.Errorf("%#v does not match ErrUnsupported", c.err)
		}
		if errors.Is(c.err, ErrCancelled) {
			t.Errorf("%#v matches ErrCancelled", c.err)
		}
	}
}

func TestCaptureErrorMessages(t *testing.T) {
	exit := &exec.ExitError{}
	cases := []struct {
		err  *CaptureError
		want string
	}{
		{&CaptureError{Tool: "grim", Detail: "compositor doesn't support wlr-screencopy", Err: exit},
			"screen capture with grim failed: compositor doesn't support wlr-screencopy; save the QR to a file and run `tess add <file>`"},
		{&CaptureError{Tool: "maim", Err: errors.New("exit status 2")},
			"screen capture with maim failed: exit status 2; save the QR to a file and run `tess add <file>`"},
		{&CaptureError{Tool: "the desktop portal"},
			"screen capture with the desktop portal failed; save the QR to a file and run `tess add <file>`"},
	}
	for _, c := range cases {
		if got := c.err.Error(); got != c.want {
			t.Errorf("Error() = %q, want %q", got, c.want)
		}
	}
	var target *exec.ExitError
	if !errors.As(cases[0].err, &target) || target != exit {
		t.Errorf("CaptureError does not unwrap to its cause")
	}
}

func TestSentinelMessagesNameTheFileFallback(t *testing.T) {
	if !strings.Contains(ErrUnsupported.Error(), "tess add <file>") {
		t.Errorf("ErrUnsupported = %q, want the file fallback", ErrUnsupported)
	}
	if ErrCancelled.Error() != "capture cancelled" {
		t.Errorf("ErrCancelled = %q", ErrCancelled)
	}
}

func TestAndList(t *testing.T) {
	for want, items := range map[string][]string{
		"":               nil,
		"maim":           {"maim"},
		"grim and slurp": {"grim", "slurp"},
		"a, b and c":     {"a", "b", "c"},
	} {
		if got := andList(items); got != want {
			t.Errorf("andList(%q) = %q, want %q", items, got, want)
		}
	}
}
