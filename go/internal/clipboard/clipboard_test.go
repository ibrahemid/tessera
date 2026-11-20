package clipboard

import (
	"errors"
	"io"
	"testing"
)

func TestNoClipboardErrorNamesWhatToInstall(t *testing.T) {
	cases := []struct {
		err  NoClipboardError
		want string
	}{
		{
			NoClipboardError{Session: "wayland", Missing: []string{"wl-clipboard"}},
			"clipboard: no clipboard tool found for the Wayland session; install wl-clipboard, or use --no-copy",
		},
		{
			NoClipboardError{Session: "x11", Missing: []string{"xclip", "xsel"}},
			"clipboard: no clipboard tool found for the X11 session; install xclip or xsel, or use --no-copy",
		},
		{
			NoClipboardError{Missing: []string{"wl-clipboard", "xclip", "xsel"}},
			"clipboard: no clipboard tool found; install wl-clipboard, xclip or xsel, or use --no-copy",
		},
		{
			NoClipboardError{Session: "osc52"},
			"clipboard: OSC 52 needs a terminal; run tess in one, or use --no-copy",
		},
		{
			NoClipboardError{},
			"clipboard: no clipboard on this system; use --no-copy",
		},
	}
	for _, c := range cases {
		if got := c.err.Error(); got != c.want {
			t.Errorf("%+v: Error() = %q, want %q", c.err, got, c.want)
		}
	}
}

func TestOverrideAcceptsKnownBackends(t *testing.T) {
	cases := map[string]string{
		"":          "",
		"wayland":   "wayland",
		" X11 ":     "x11",
		"OSC52":     "osc52",
		"osc52\n":   "osc52",
		"Wayland  ": "wayland",
	}
	for raw, want := range cases {
		got, err := override(env{getenv: fixedEnv(map[string]string{OverrideEnv: raw})})
		if err != nil || got != want {
			t.Errorf("override(%q) = %q, %v; want %q", raw, got, err, want)
		}
	}
}

func TestOverrideRejectsUnknownBackend(t *testing.T) {
	_, err := override(env{getenv: fixedEnv(map[string]string{OverrideEnv: "pbcopy"})})
	var inv *InvalidOverrideError
	if !errors.As(err, &inv) || inv.Value != "pbcopy" {
		t.Fatalf("err = %v, want *InvalidOverrideError for pbcopy", err)
	}
	if got, want := err.Error(), `clipboard: TESSERA_CLIPBOARD="pbcopy" is not wayland, x11 or osc52; set one of those or unset it`; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
}

func TestDetectRejectsUnknownOverride(t *testing.T) {
	t.Setenv(OverrideEnv, "clipboard-manager")
	c, err := Detect()
	var inv *InvalidOverrideError
	if c != nil || !errors.As(err, &inv) {
		t.Fatalf("Detect() = %v, %v; want *InvalidOverrideError", c, err)
	}
}

func TestOpErrorFormatsAndUnwraps(t *testing.T) {
	cause := errors.New("exit status 1")
	cases := []struct {
		err  OpError
		want string
	}{
		{OpError{Backend: "xclip", Op: "write", Detail: "Error: Can't open display: (null)", Err: cause}, "clipboard: xclip write failed: Error: Can't open display: (null)"},
		{OpError{Backend: "xsel", Op: "clear", Err: cause}, "clipboard: xsel clear failed: exit status 1"},
		{OpError{Backend: "OSC 52", Op: "write"}, "clipboard: OSC 52 write failed"},
	}
	for _, c := range cases {
		if got := c.err.Error(); got != c.want {
			t.Errorf("Error() = %q, want %q", got, c.want)
		}
	}
	err := error(&OpError{Backend: "xclip", Op: "read", Err: io.ErrUnexpectedEOF})
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatal("OpError does not unwrap to its cause")
	}
}

func TestSystemEnvReadsProcessEnvironment(t *testing.T) {
	t.Setenv(OverrideEnv, "x11")
	e := systemEnv()
	if e.getenv(OverrideEnv) != "x11" || e.lookPath == nil || e.openTTY == nil {
		t.Fatal("systemEnv is not wired to the process environment")
	}
	if _, err := e.lookPath("tessera-no-such-tool"); err == nil {
		t.Fatal("lookPath found a tool that does not exist")
	}
}

func TestOrList(t *testing.T) {
	cases := map[string][]string{
		"":          nil,
		"a":         {"a"},
		"a or b":    {"a", "b"},
		"a, b or c": {"a", "b", "c"},
	}
	for want, in := range cases {
		if got := orList(in); got != want {
			t.Errorf("orList(%q) = %q, want %q", in, got, want)
		}
	}
}

func fixedEnv(vars map[string]string) func(string) string {
	return func(k string) string { return vars[k] }
}
