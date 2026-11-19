//go:build unix && !darwin

package clipboard

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ibrahemid/tessera/go/internal/procenv"
)

type fakeTTY struct {
	writes [][]byte
	n      int
}

func (f *fakeTTY) Write(p []byte) (int, error) {
	f.writes = append(f.writes, bytes.Clone(p))
	if f.n > 0 {
		return f.n, nil
	}
	return len(p), nil
}

func (f *fakeTTY) Close() error { return nil }

func ttyOK() (io.WriteCloser, error) { return &fakeTTY{}, nil }

func ttyMissing() (io.WriteCloser, error) { return nil, os.ErrNotExist }

func onPath(tools ...string) func(string) (string, error) {
	return func(name string) (string, error) {
		if slices.Contains(tools, name) {
			return "/usr/bin/" + name, nil
		}
		return "", exec.ErrNotFound
	}
}

func TestPlatformDetect(t *testing.T) {
	wayland := map[string]string{"WAYLAND_DISPLAY": "wayland-0"}
	x11 := map[string]string{"DISPLAY": ":0"}
	both := map[string]string{"WAYLAND_DISPLAY": "wayland-0", "DISPLAY": ":0"}
	none := map[string]string{}
	wl := wlClipboard{copyPath: "/usr/bin/wl-copy", pastePath: "/usr/bin/wl-paste"}
	xc := xclip{path: "/usr/bin/xclip"}
	xs := xsel{path: "/usr/bin/xsel"}
	all := []string{"wl-copy", "wl-paste", "xclip", "xsel"}

	cases := []struct {
		name    string
		vars    map[string]string
		tools   []string
		tty     func() (io.WriteCloser, error)
		want    Clipboard
		wantErr *NoClipboardError
	}{
		{"wayland with wl-clipboard", wayland, all, ttyOK, wl, nil},
		{"wayland without wl-paste falls to terminal", wayland, []string{"wl-copy"}, ttyOK, osc52Clipboard{}, nil},
		{"wayland with xwayland and xclip", both, []string{"xclip"}, ttyOK, xc, nil},
		{"wayland prefers wl-clipboard over xclip", both, all, ttyOK, wl, nil},
		{"wayland without tools uses terminal", wayland, nil, ttyOK, osc52Clipboard{}, nil},
		{"x11 prefers xclip", x11, all, ttyOK, xc, nil},
		{"x11 with xsel only", x11, []string{"xsel"}, ttyOK, xs, nil},
		{"x11 ignores wl-clipboard", x11, []string{"wl-copy", "wl-paste"}, ttyOK, osc52Clipboard{}, nil},
		{"no display uses terminal", none, all, ttyOK, osc52Clipboard{}, nil},
		{"no display no terminal", none, all, ttyMissing, nil,
			&NoClipboardError{Missing: []string{"wl-clipboard", "xclip", "xsel"}}},
		{"wayland nothing usable", wayland, nil, ttyMissing, nil,
			&NoClipboardError{Session: "wayland", Missing: []string{"wl-clipboard"}}},
		{"x11 nothing usable", x11, []string{"wl-copy", "wl-paste"}, ttyMissing, nil,
			&NoClipboardError{Session: "x11", Missing: []string{"xclip", "xsel"}}},
		{"wayland and x11 nothing usable", both, nil, ttyMissing, nil,
			&NoClipboardError{Session: "wayland", Missing: []string{"wl-clipboard", "xclip", "xsel"}}},
		{"forced wayland without display", map[string]string{OverrideEnv: "wayland"}, all, ttyOK, wl, nil},
		{"forced wayland missing tool", map[string]string{OverrideEnv: "wayland", "DISPLAY": ":0"}, []string{"xclip"}, ttyOK, nil,
			&NoClipboardError{Session: "wayland", Missing: []string{"wl-clipboard"}}},
		{"forced x11 over wayland", map[string]string{OverrideEnv: "x11", "WAYLAND_DISPLAY": "wayland-0"}, all, ttyOK, xc, nil},
		{"forced x11 xsel only", map[string]string{OverrideEnv: "x11"}, []string{"xsel"}, ttyOK, xs, nil},
		{"forced x11 missing tools", map[string]string{OverrideEnv: "x11"}, []string{"wl-copy", "wl-paste"}, ttyOK, nil,
			&NoClipboardError{Session: "x11", Missing: []string{"xclip", "xsel"}}},
		{"forced osc52 over wayland", map[string]string{OverrideEnv: "osc52", "WAYLAND_DISPLAY": "wayland-0"}, all, ttyOK, osc52Clipboard{}, nil},
		{"forced osc52 without terminal", map[string]string{OverrideEnv: "osc52"}, all, ttyMissing, nil,
			&NoClipboardError{Session: "osc52"}},
		{"screen session", map[string]string{"STY": "1234.pts-0.host"}, nil, ttyOK, osc52Clipboard{screen: true}, nil},
		{"tmux inside screen stays plain", map[string]string{"STY": "1234.pts-0.host", "TMUX": "/tmp/tmux-1000/default,1,0"}, nil, ttyOK, osc52Clipboard{}, nil},
		{"tmux session", map[string]string{"TMUX": "/tmp/tmux-1000/default,1,0"}, nil, ttyOK, osc52Clipboard{}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := platformDetect(env{getenv: fixedEnv(c.vars), lookPath: onPath(c.tools...), openTTY: c.tty})
			if c.wantErr != nil {
				var nc *NoClipboardError
				if !errors.As(err, &nc) || nc.Session != c.wantErr.Session || !slices.Equal(nc.Missing, c.wantErr.Missing) {
					t.Fatalf("got %#v, %v; want %#v", got, err, c.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if want, ok := c.want.(osc52Clipboard); ok {
				o, isOSC := got.(osc52Clipboard)
				if !isOSC || o.screen != want.screen || o.openTTY == nil || o.Name() != "OSC 52" {
					t.Fatalf("got %#v, want OSC 52 with screen=%v", got, want.screen)
				}
				return
			}
			if got != c.want {
				t.Fatalf("got %#v, want %#v", got, c.want)
			}
		})
	}
}

func TestPlatformDetectRejectsUnknownOverride(t *testing.T) {
	_, err := platformDetect(env{getenv: fixedEnv(map[string]string{OverrideEnv: "klipper"}), lookPath: onPath(), openTTY: ttyOK})
	var inv *InvalidOverrideError
	if !errors.As(err, &inv) {
		t.Fatalf("err = %v, want *InvalidOverrideError", err)
	}
}

func TestBackendNames(t *testing.T) {
	for want, c := range map[string]Clipboard{
		"wl-clipboard": wlClipboard{}, "xclip": xclip{}, "xsel": xsel{}, "OSC 52": osc52Clipboard{},
	} {
		if c.Name() != want {
			t.Errorf("Name() = %q, want %q", c.Name(), want)
		}
	}
}

// detectFromPath runs detection with PATH set to dir alone, so tools
// installed on the host stay out of the way.
func detectFromPath(t *testing.T, dir string, vars map[string]string) Clipboard {
	t.Helper()
	for _, util := range []string{"cat", "sleep", "env"} {
		if _, err := os.Lstat(filepath.Join(dir, util)); err == nil {
			continue
		}
		p, err := exec.LookPath(util)
		if err != nil {
			t.Fatalf("fake tools need %s: %v", util, err)
		}
		if err := os.Symlink(p, filepath.Join(dir, util)); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
	c, err := platformDetect(env{getenv: fixedEnv(vars), lookPath: exec.LookPath, openTTY: ttyMissing})
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	return c
}

func wlFakes(t *testing.T, version, pasteBody string) (string, Clipboard) {
	t.Helper()
	dir := t.TempDir()
	fakeTool(t, dir, "wl-copy", `if [ "$1" = --version ]; then printf 'wl-clipboard `+version+`\nCopyright (C) 2018-2026 Sergey Bugaev\n'; fi`)
	fakeTool(t, dir, "wl-paste", pasteBody)
	return dir, detectFromPath(t, dir, map[string]string{"WAYLAND_DISPLAY": "wayland-0", "DISPLAY": ":0"})
}

func argvs(calls []toolCall) [][]string {
	out := make([][]string, len(calls))
	for i, c := range calls {
		out[i] = c.args
	}
	return out
}

func TestWlClipboardArgv(t *testing.T) {
	cases := []struct {
		version   string
		sensitive bool
		want      [][]string
	}{
		{"2.3.0", true, [][]string{{"--version"}, {"--type", "text/plain", "--sensitive"}}},
		{"2.4.1", true, [][]string{{"--version"}, {"--type", "text/plain", "--sensitive"}}},
		{"3.0", true, [][]string{{"--version"}, {"--type", "text/plain", "--sensitive"}}},
		{"2.2.1", true, [][]string{{"--version"}, {"--type", "text/plain"}}},
		{"1.9.9", true, [][]string{{"--version"}, {"--type", "text/plain"}}},
		{"2.3.0", false, [][]string{{"--type", "text/plain"}}},
	}
	for _, c := range cases {
		dir, cb := wlFakes(t, c.version, "")
		if cb.Name() != "wl-clipboard" {
			t.Fatalf("detected %s, want wl-clipboard", cb.Name())
		}
		if err := cb.Write("123456", c.sensitive); err != nil {
			t.Fatalf("%s sensitive=%v: Write: %v", c.version, c.sensitive, err)
		}
		calls := toolCalls(t, dir, "wl-copy")
		if got := argvs(calls); !slices.EqualFunc(got, c.want, slices.Equal) {
			t.Fatalf("%s sensitive=%v: wl-copy argv = %q, want %q", c.version, c.sensitive, got, c.want)
		}
		if last := calls[len(calls)-1]; last.stdin != "123456" {
			t.Fatalf("wl-copy stdin = %q, want the text", last.stdin)
		}
	}
}

func TestWlCopyVersionProbeRunsOnce(t *testing.T) {
	dir, cb := wlFakes(t, "2.3.0", "")
	for range 3 {
		if err := cb.Write("123456", true); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}
	probes := 0
	for _, c := range toolCalls(t, dir, "wl-copy") {
		if slices.Equal(c.args, []string{"--version"}) {
			probes++
		}
	}
	if probes != 1 {
		t.Fatalf("wl-copy --version ran %d times, want 1", probes)
	}
}

func TestWlCopyFailedProbeOmitsSensitive(t *testing.T) {
	dir := t.TempDir()
	fakeTool(t, dir, "wl-copy", `if [ "$1" = --version ]; then exit 1; fi`)
	fakeTool(t, dir, "wl-paste", "")
	cb := detectFromPath(t, dir, map[string]string{"WAYLAND_DISPLAY": "wayland-0"})
	if err := cb.Write("123456", true); err != nil {
		t.Fatalf("Write: %v", err)
	}
	calls := toolCalls(t, dir, "wl-copy")
	if got := calls[len(calls)-1].args; slices.Contains(got, "--sensitive") {
		t.Fatalf("wl-copy argv = %q after a failed probe", got)
	}
}

func TestWlClipboardReadAndClearArgv(t *testing.T) {
	dir, cb := wlFakes(t, "2.3.0", "printf '123456'")
	got, err := cb.Read()
	if err != nil || got != "123456" {
		t.Fatalf("Read() = %q, %v", got, err)
	}
	if err := cb.Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if got, want := argvs(toolCalls(t, dir, "wl-paste")), [][]string{{"--no-newline", "--type", "text/plain"}}; !slices.EqualFunc(got, want, slices.Equal) {
		t.Fatalf("wl-paste argv = %q, want %q", got, want)
	}
	if got, want := argvs(toolCalls(t, dir, "wl-copy")), [][]string{{"--clear"}}; !slices.EqualFunc(got, want, slices.Equal) {
		t.Fatalf("wl-copy argv = %q, want %q", got, want)
	}
}

func TestWlClipboardWriteReturnsWhileServerHoldsStderr(t *testing.T) {
	dir := t.TempDir()
	fakeTool(t, dir, "wl-copy", `if [ "$1" = --version ]; then echo 'wl-clipboard 2.3.0'; exit 0; fi
(sleep 5) &`)
	fakeTool(t, dir, "wl-paste", "")
	cb := detectFromPath(t, dir, map[string]string{"WAYLAND_DISPLAY": "wayland-0"})
	start := time.Now()
	if err := cb.Write("123456", true); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("Write took %s; it waited for the forked server", elapsed)
	}
}

func TestReadMapsNothingCopiedToEmpty(t *testing.T) {
	cases := []struct {
		tool, body string
		vars       map[string]string
	}{
		{"wl-paste", "echo 'Nothing is copied' >&2; exit 1", map[string]string{"WAYLAND_DISPLAY": "wayland-0"}},
		{"wl-paste", `printf 'Clipboard content is not available as requested type "text/plain"\nUse "wl-paste --list-types" to view available types.\n' >&2; exit 1`, map[string]string{"WAYLAND_DISPLAY": "wayland-0"}},
		{"wl-paste", "echo 'No suitable type of content copied' >&2; exit 1", map[string]string{"WAYLAND_DISPLAY": "wayland-0"}},
		{"xclip", "echo 'Error: target STRING not available' >&2; exit 1", map[string]string{"DISPLAY": ":0"}},
		{"xsel", "", map[string]string{"DISPLAY": ":0"}},
	}
	for _, c := range cases {
		dir := t.TempDir()
		fakeTool(t, dir, c.tool, c.body)
		if c.tool == "wl-paste" {
			fakeTool(t, dir, "wl-copy", "")
		}
		got, err := detectFromPath(t, dir, c.vars).Read()
		if err != nil || got != "" {
			t.Errorf("%s %q: Read() = %q, %v; want empty", c.tool, c.body, got, err)
		}
	}
}

func TestReadReportsRealFailures(t *testing.T) {
	cases := []struct {
		tool, body, detail string
		vars               map[string]string
	}{
		{"wl-paste", "echo 'Failed to connect to a Wayland server' >&2; exit 1", "Failed to connect to a Wayland server", map[string]string{"WAYLAND_DISPLAY": "wayland-0"}},
		{"xclip", "echo \"Error: Can't open display: :0\" >&2; exit 1", "Error: Can't open display: :0", map[string]string{"DISPLAY": ":0"}},
		{"xsel", "echo \"xsel: Can't open display: :0\" >&2; exit 1", "xsel: Can't open display: :0", map[string]string{"DISPLAY": ":0"}},
	}
	for _, c := range cases {
		dir := t.TempDir()
		fakeTool(t, dir, c.tool, c.body)
		if c.tool == "wl-paste" {
			fakeTool(t, dir, "wl-copy", "")
		}
		cb := detectFromPath(t, dir, c.vars)
		_, err := cb.Read()
		var op *OpError
		if !errors.As(err, &op) || op.Op != "read" || op.Backend != cb.Name() || op.Detail != c.detail {
			t.Errorf("%s: err = %v, want read *OpError with %q", c.tool, err, c.detail)
		}
	}
}

func TestXclipArgv(t *testing.T) {
	dir := t.TempDir()
	fakeTool(t, dir, "xclip", `case " $* " in *" -out "*) printf 'abc';; esac`)
	cb := detectFromPath(t, dir, map[string]string{"DISPLAY": ":0"})
	if cb.Name() != "xclip" {
		t.Fatalf("detected %s, want xclip", cb.Name())
	}
	if err := cb.Write("123456", true); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, err := cb.Read()
	if err != nil || got != "abc" {
		t.Fatalf("Read() = %q, %v", got, err)
	}
	if err := cb.Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	want := []toolCall{
		{args: []string{"-selection", "clipboard"}, stdin: "123456"},
		{args: []string{"-selection", "clipboard", "-out"}, stdin: ""},
		{args: []string{"-selection", "clipboard"}, stdin: ""},
	}
	assertCalls(t, toolCalls(t, dir, "xclip"), want)
}

func TestXselArgv(t *testing.T) {
	dir := t.TempDir()
	fakeTool(t, dir, "xsel", `case " $* " in *" --output "*) printf 'abc';; esac`)
	cb := detectFromPath(t, dir, map[string]string{"DISPLAY": ":0"})
	if cb.Name() != "xsel" {
		t.Fatalf("detected %s, want xsel", cb.Name())
	}
	if err := cb.Write("123456", true); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, err := cb.Read()
	if err != nil || got != "abc" {
		t.Fatalf("Read() = %q, %v", got, err)
	}
	if err := cb.Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	want := []toolCall{
		{args: []string{"--clipboard", "--input"}, stdin: "123456"},
		{args: []string{"--clipboard", "--output"}, stdin: ""},
		{args: []string{"--clipboard", "--clear"}, stdin: ""},
	}
	assertCalls(t, toolCalls(t, dir, "xsel"), want)
}

func assertCalls(t *testing.T, got, want []toolCall) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("calls = %+v, want %+v", got, want)
	}
	for i := range want {
		if !slices.Equal(got[i].args, want[i].args) || got[i].stdin != want[i].stdin {
			t.Fatalf("call %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestWriteFailureIsOpError(t *testing.T) {
	dir := t.TempDir()
	fakeTool(t, dir, "xclip", "echo \"Error: Can't open display: :9\" >&2; exit 1")
	err := detectFromPath(t, dir, map[string]string{"DISPLAY": ":9"}).Write("123456", true)
	var op *OpError
	if !errors.As(err, &op) || op.Backend != "xclip" || op.Op != "write" || op.Detail != "Error: Can't open display: :9" {
		t.Fatalf("err = %v", err)
	}
}

func TestVersionAtLeast(t *testing.T) {
	floor := [3]int{2, 3, 0}
	cases := map[string]bool{
		"wl-clipboard 2.3.0\nCopyright": true,
		"wl-clipboard 2.3":              true,
		"wl-clipboard 2.3.0-rc1":        true,
		"wl-clipboard 2.10.0":           true,
		"wl-clipboard 10.0.0":           true,
		"wl-clipboard 2.2.1":            false,
		"wl-clipboard 2.2.99":           false,
		"wl-clipboard 1.9":              false,
		"wl-clipboard":                  false,
		"":                              false,
		"wl-clipboard 2.\n":             false,
		"\nwl-clipboard 2.3.0":          false,
	}
	for in, want := range cases {
		if got := versionAtLeast(in, floor); got != want {
			t.Errorf("versionAtLeast(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestOSC52Bytes(t *testing.T) {
	long := strings.Repeat("x", 120)
	b64 := base64.StdEncoding.EncodeToString([]byte(long))
	cases := []struct {
		name string
		vars map[string]string
		text string
		want string
	}{
		{"plain", nil, "hello", "\x1b]52;c;aGVsbG8=\x07"},
		{"tmux", map[string]string{"TMUX": "/tmp/tmux-1000/default,1,0"}, "hello", "\x1b]52;c;aGVsbG8=\x07"},
		{"screen", map[string]string{"STY": "1234.pts-0.host"}, "hello", "\x1bP\x1b]52;c;aGVsbG8=\x07\x1b\\"},
		{"screen chunks", map[string]string{"STY": "1234.pts-0.host"}, long,
			"\x1bP\x1b]52;c;" + b64[:76] + "\x1b\\\x1bP" + b64[76:152] + "\x1b\\\x1bP" + b64[152:] + "\x07\x1b\\"},
		{"empty", nil, "", "\x1b]52;c;\x07"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tty := &fakeTTY{}
			open := func() (io.WriteCloser, error) { return tty, nil }
			cb, err := platformDetect(env{getenv: fixedEnv(c.vars), lookPath: onPath(), openTTY: open})
			if err != nil {
				t.Fatal(err)
			}
			tty.writes = nil
			if err := cb.Write(c.text, true); err != nil {
				t.Fatalf("Write: %v", err)
			}
			if len(tty.writes) != 1 {
				t.Fatalf("Write issued %d writes, want 1", len(tty.writes))
			}
			if got := string(tty.writes[0]); got != c.want {
				t.Fatalf("bytes = %q, want %q", got, c.want)
			}
		})
	}
}

func TestOSC52Failures(t *testing.T) {
	short := osc52Clipboard{openTTY: func() (io.WriteCloser, error) { return &fakeTTY{n: 3}, nil }}
	if err := short.Write("hello", true); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("short write: err = %v", err)
	}
	gone := osc52Clipboard{openTTY: ttyMissing}
	err := gone.Write("hello", true)
	var op *OpError
	if !errors.As(err, &op) || op.Backend != "OSC 52" || !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing tty: err = %v", err)
	}
	if _, err := gone.Read(); !errors.Is(err, ErrReadUnsupported) {
		t.Fatalf("Read: err = %v", err)
	}
	if err := gone.Clear(); !errors.Is(err, ErrClearUnsupported) {
		t.Fatalf("Clear: err = %v", err)
	}
}

// TestRealClipboardRoundTrip drives the system clipboard; it runs only when
// TESSERA_CLIPBOARD_TEST names the session (wayland or x11).
func TestRealClipboardRoundTrip(t *testing.T) {
	session := os.Getenv("TESSERA_CLIPBOARD_TEST")
	if session == "" {
		t.Skip("set TESSERA_CLIPBOARD_TEST=wayland or TESSERA_CLIPBOARD_TEST=x11 to run against a real clipboard")
	}
	cb, err := Detect()
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	wantNames := map[string][]string{"wayland": {"wl-clipboard"}, "x11": {"xclip", "xsel"}}[session]
	if !slices.Contains(wantNames, cb.Name()) {
		t.Fatalf("detected %s for %s, want one of %q", cb.Name(), session, wantNames)
	}
	t.Logf("backend: %s", cb.Name())

	nonce := make([]byte, 8)
	if _, err := rand.Read(nonce); err != nil {
		t.Fatal(err)
	}
	texts := []struct {
		text      string
		sensitive bool
	}{
		{"tess-" + hex.EncodeToString(nonce), true},
		{"código ✓ 123 456", false},
		{"line one\nline two\n", true},
	}
	for _, tc := range texts {
		if err := cb.Write(tc.text, tc.sensitive); err != nil {
			t.Fatalf("Write(%q): %v", tc.text, err)
		}
		waitForRead(t, cb, tc.text)
	}

	if w, ok := cb.(wlClipboard); ok {
		if err := cb.Write("123456", true); err != nil {
			t.Fatalf("Write: %v", err)
		}
		waitForRead(t, cb, "123456")
		list := exec.Command(w.pastePath, "--list-types")
		list.Env = procenv.Child()
		out, err := list.Output()
		if err != nil {
			t.Fatalf("wl-paste --list-types: %v", err)
		}
		hinted := strings.Contains(string(out), "x-kde-passwordManagerHint")
		if supported := wlCopyHasSensitive(w.copyPath); hinted != supported {
			t.Fatalf("password manager hint offered = %v, wl-copy supports --sensitive = %v", hinted, supported)
		}
		t.Logf("sensitive hint offered: %v", hinted)
	}

	if err := cb.Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	waitForRead(t, cb, "")
}

func waitForRead(t *testing.T, cb Clipboard, want string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		got, err := cb.Read()
		if err == nil && got == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("Read() = %q, %v; want %q", got, err, want)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
