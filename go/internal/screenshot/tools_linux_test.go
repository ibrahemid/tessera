//go:build linux

package screenshot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/png"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ibrahemid/tessera/go/internal/qr"
)

const geometry = "10,20 300x400"

type toolCall struct {
	args       []string
	passphrase string
}

// fakeTool writes an executable script that records each call's argv and
// whether TESSERA_PASSPHRASE reached it under dir, then runs body.
func fakeTool(t *testing.T, dir, name, body string) {
	t.Helper()
	rec := shQuote(filepath.Join(dir, name+".call."))
	script := "#!/bin/sh\n" +
		"n=1\n" +
		"while [ -e " + rec + "\"$n.argv\" ]; do n=$((n+1)); done\n" +
		"printf '%s\\0' \"$#\" \"$@\" > " + rec + "\"$n.argv\"\n" +
		"printf '%s' \"${TESSERA_PASSPHRASE-unset}\" > " + rec + "\"$n.env\"\n" +
		body + "\n"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func toolCalls(t *testing.T, dir, name string) []toolCall {
	t.Helper()
	var calls []toolCall
	for n := 1; ; n++ {
		base := filepath.Join(dir, name+".call."+strconv.Itoa(n))
		argv, err := os.ReadFile(base + ".argv")
		if errors.Is(err, os.ErrNotExist) {
			return calls
		}
		if err != nil {
			t.Fatal(err)
		}
		pass, err := os.ReadFile(base + ".env")
		if err != nil {
			t.Fatal(err)
		}
		fields := strings.Split(strings.TrimSuffix(string(argv), "\x00"), "\x00")
		if count, err := strconv.Atoi(fields[0]); err != nil || count != len(fields)-1 {
			t.Fatalf("%s call %d: malformed argv record %q", name, n, argv)
		}
		calls = append(calls, toolCall{args: fields[1:], passphrase: string(pass)})
	}
}

func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

const writeLastArg = `for out; do :; done; printf 'png' > "$out"`

// writerBody is a fake tool that succeed: slurp prints a selection, every
// other tool writes its last argument.
func writerBody(name string) string {
	if name == toolSlurp {
		return "printf '" + geometry + "\\n'"
	}
	return writeLastArg
}

var allTools = []string{toolGrim, toolSlurp, toolSpectacle, toolMaim, toolScrot, toolImport, toolGnomeScreenshot}

// toolDir returns a directory holding the named fake tools plus sleep, and
// makes it the whole PATH so tools installed on the host stay out of the way.
func toolDir(t *testing.T, bodies map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range bodies {
		fakeTool(t, dir, name, body)
	}
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Fatalf("fake tools need sleep: %v", err)
	}
	if err := os.Symlink(sleep, filepath.Join(dir, "sleep")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("TESSERA_PASSPHRASE", "test-passphrase-1")
	return dir
}

func writers(names ...string) map[string]string {
	m := make(map[string]string, len(names))
	for _, n := range names {
		m[n] = writerBody(n)
	}
	return m
}

type portalStub struct {
	calls int
	path  string
	err   error
}

func (p *portalStub) capture(context.Context) (string, error) {
	p.calls++
	return p.path, p.err
}

func noPortal() *portalStub { return &portalStub{err: errNoPortal} }

func fixedEnv(vars map[string]string) func(string) string {
	return func(k string) string { return vars[k] }
}

var (
	waylandVars = map[string]string{"WAYLAND_DISPLAY": "wayland-0"}
	x11Vars     = map[string]string{"DISPLAY": ":0"}
	bothVars    = map[string]string{"WAYLAND_DISPLAY": "wayland-0", "DISPLAY": ":0"}
)

func captureWith(ctx context.Context, t *testing.T, vars map[string]string, portal *portalStub) (string, string, error) {
	t.Helper()
	scratch := t.TempDir()
	path, err := capture(ctx, scratch, env{getenv: fixedEnv(vars), lookPath: exec.LookPath, portal: portal.capture})
	return filepath.Join(scratch, captureName), path, err
}

func TestGrimSlurpGeometryHandOff(t *testing.T) {
	dir := toolDir(t, writers(toolGrim, toolSlurp))
	out, path, err := captureWith(context.Background(), t, waylandVars, noPortal())
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	if path != out {
		t.Fatalf("capture = %s, want %s", path, out)
	}
	slurp := toolCalls(t, dir, toolSlurp)
	if len(slurp) != 1 || len(slurp[0].args) != 0 {
		t.Fatalf("slurp calls = %+v, want one with no arguments", slurp)
	}
	grim := toolCalls(t, dir, toolGrim)
	if want := []string{"-g", geometry, out}; len(grim) != 1 || !slices.Equal(grim[0].args, want) {
		t.Fatalf("grim calls = %+v, want argv %q", grim, want)
	}
	for _, c := range append(slurp, grim...) {
		if c.passphrase != "unset" {
			t.Fatalf("a capture tool inherited TESSERA_PASSPHRASE: %+v", c)
		}
	}
}

type orderCase struct {
	name    string
	vars    map[string]string
	tools   []string
	want    string
	missing *NoCaptureError
}

func TestToolOrderBySession(t *testing.T) {
	selectArgs := map[string]func(string) []string{}
	for _, s := range x11Tools {
		selectArgs[s.name] = s.args
	}
	x11Order := []string{toolMaim, toolScrot, toolImport, toolGnomeScreenshot, toolSpectacle}
	expectArgs := map[string][]string{
		toolSpectacle:       {"-b", "-n", "-r", "-o", "OUT"},
		toolMaim:            {"-s", "OUT"},
		toolScrot:           {"-s", "OUT"},
		toolImport:          {"OUT"},
		toolGnomeScreenshot: {"-a", "-f", "OUT"},
	}

	cases := []orderCase{
		{"wayland prefers grim and slurp", waylandVars, allTools, toolGrim, nil},
		{"xwayland prefers grim and slurp", bothVars, allTools, toolGrim, nil},
		{"wayland without slurp uses spectacle", waylandVars, []string{toolGrim, toolSpectacle, toolMaim}, toolSpectacle, nil},
		{"wayland without grim uses spectacle", waylandVars, []string{toolSlurp, toolSpectacle}, toolSpectacle, nil},
		{"wayland ignores x11 tools", waylandVars, []string{toolSlurp, toolMaim, toolScrot, toolImport, toolGnomeScreenshot}, "",
			&NoCaptureError{Session: sessionWayland, Missing: []string{toolGrim, toolSlurp}}},
		{"xwayland ignores x11 tools", bothVars, []string{toolMaim}, "",
			&NoCaptureError{Session: sessionWayland, Missing: []string{toolGrim, toolSlurp}}},
		{"x11 ignores grim and slurp", x11Vars, []string{toolGrim, toolSlurp}, "",
			&NoCaptureError{Session: sessionX11, Missing: []string{toolMaim}}},
		{"no display", map[string]string{}, allTools, "", &NoCaptureError{}},
	}
	for i, tool := range x11Order {
		cases = append(cases, orderCase{"x11 picks " + tool, x11Vars, append([]string{toolGrim, toolSlurp}, x11Order[i:]...), tool, nil})
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := toolDir(t, writers(c.tools...))
			portal := noPortal()
			out, path, err := captureWith(context.Background(), t, c.vars, portal)
			if c.missing != nil {
				var nc *NoCaptureError
				if !errors.As(err, &nc) || nc.Session != c.missing.Session || !slices.Equal(nc.Missing, c.missing.Missing) {
					t.Fatalf("capture = %q, %v; want %#v", path, err, c.missing)
				}
				if !errors.Is(err, ErrUnsupported) {
					t.Fatalf("%v does not match ErrUnsupported", err)
				}
				if c.missing.Session == "" && portal.calls != 0 {
					t.Fatal("the portal was asked without a display")
				}
			} else if err != nil || path != out {
				t.Fatalf("capture = %q, %v; want %s", path, err, out)
			}
			for _, name := range c.tools {
				calls := toolCalls(t, dir, name)
				used := name == c.want || (c.want == toolGrim && name == toolSlurp)
				if !used {
					if len(calls) != 0 {
						t.Fatalf("%s ran: %+v", name, calls)
					}
					continue
				}
				if len(calls) != 1 {
					t.Fatalf("%s calls = %+v, want one", name, calls)
				}
				if name == toolGrim || name == toolSlurp {
					continue
				}
				wantArgs := slices.Clone(expectArgs[name])
				wantArgs[len(wantArgs)-1] = out
				if !slices.Equal(calls[0].args, wantArgs) {
					t.Fatalf("%s argv = %q, want %q", name, calls[0].args, wantArgs)
				}
				if got := selectArgs[name](out); !slices.Equal(got, wantArgs) {
					t.Fatalf("%s spec argv = %q, want %q", name, got, wantArgs)
				}
			}
		})
	}
}

func TestPortalAnswerSkipsTools(t *testing.T) {
	failure := &CaptureError{Tool: portalTool, Detail: "the request ended with response code 2"}
	cases := []struct {
		name   string
		portal *portalStub
		path   string
		err    error
	}{
		{"file", &portalStub{path: "/home/me/Pictures/Screenshot.png"}, "/home/me/Pictures/Screenshot.png", nil},
		{"cancel", &portalStub{err: ErrCancelled}, "", ErrCancelled},
		{"failure", &portalStub{err: failure}, "", failure},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := toolDir(t, writers(allTools...))
			_, path, err := captureWith(context.Background(), t, waylandVars, c.portal)
			if path != c.path || !errors.Is(err, c.err) || (c.err == nil && err != nil) {
				t.Fatalf("capture = %q, %v; want %q, %v", path, err, c.path, c.err)
			}
			if c.portal.calls != 1 {
				t.Fatalf("portal asked %d times, want 1", c.portal.calls)
			}
			for _, name := range allTools {
				if calls := toolCalls(t, dir, name); len(calls) != 0 {
					t.Fatalf("%s ran after the portal answered: %+v", name, calls)
				}
			}
		})
	}
}

func TestSelectToolOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		tool   string
		body   string
		cancel bool
		detail string
	}{
		{"exit zero without image is cancel", toolSpectacle, "exit 0", true, ""},
		{"empty image is cancel", toolGnomeScreenshot, `for out; do :; done; : > "$out"`, true, ""},
		{"maim cancel", toolMaim, "echo 'Selection was cancelled by keystroke or right-click.' >&2; exit 1", true, ""},
		{"scrot cancel", toolScrot, "echo 'scrot: Key was pressed, aborting selection' >&2; exit 1", true, ""},
		{"import failure", toolImport, "echo 'import: unable to open X server' >&2; exit 1", false, "import: unable to open X server"},
		{"silent failure", toolMaim, "exit 3", false, ""},
		{"failure after writing", toolMaim, writeLastArg + "; echo 'cancelled late' >&2; exit 1", false, "cancelled late"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			toolDir(t, map[string]string{c.tool: c.body})
			_, path, err := captureWith(context.Background(), t, x11Vars, noPortal())
			if path != "" {
				t.Fatalf("capture returned %q on failure", path)
			}
			if c.cancel {
				if !errors.Is(err, ErrCancelled) {
					t.Fatalf("capture = %v, want ErrCancelled", err)
				}
				return
			}
			var ce *CaptureError
			if !errors.As(err, &ce) || ce.Tool != c.tool || ce.Detail != c.detail || ce.Err == nil {
				t.Fatalf("capture = %#v, want a %s CaptureError with detail %q", err, c.tool, c.detail)
			}
		})
	}
}

func TestGrimSlurpOutcomes(t *testing.T) {
	cases := []struct {
		name     string
		slurp    string
		grim     string
		cancel   bool
		tool     string
		detail   string
		grimRuns bool
	}{
		{"slurp cancel", "echo 'selection cancelled' >&2; exit 1", writeLastArg, true, "", "", false},
		{"slurp failure", "echo 'compositor doesn'\\''t support wlr-layer-shell-unstable-v1' >&2; exit 1", writeLastArg, false, toolSlurp,
			"compositor doesn't support wlr-layer-shell-unstable-v1", false},
		{"slurp prints nothing", "exit 0", writeLastArg, false, toolSlurp, "no selection was returned", false},
		{"grim failure", "printf '" + geometry + "\\n'", "echo 'grim: invalid geometry' >&2; exit 1", false, toolGrim, "grim: invalid geometry", true},
		{"grim writes nothing", "printf '" + geometry + "\\n'", "exit 0", false, toolGrim, "no image was written", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := toolDir(t, map[string]string{toolSlurp: c.slurp, toolGrim: c.grim, toolSpectacle: writeLastArg})
			_, path, err := captureWith(context.Background(), t, waylandVars, noPortal())
			if path != "" {
				t.Fatalf("capture returned %q on failure", path)
			}
			if c.cancel {
				if !errors.Is(err, ErrCancelled) {
					t.Fatalf("capture = %v, want ErrCancelled", err)
				}
			} else {
				var ce *CaptureError
				if !errors.As(err, &ce) || ce.Tool != c.tool || ce.Detail != c.detail {
					t.Fatalf("capture = %#v, want a %s CaptureError with detail %q", err, c.tool, c.detail)
				}
			}
			if ran := len(toolCalls(t, dir, toolGrim)) > 0; ran != c.grimRuns {
				t.Fatalf("grim ran = %v, want %v", ran, c.grimRuns)
			}
			if calls := toolCalls(t, dir, toolSpectacle); len(calls) != 0 {
				t.Fatalf("spectacle ran after grim and slurp: %+v", calls)
			}
		})
	}
}

func TestToolCaptureHonoursContext(t *testing.T) {
	for _, c := range []struct {
		name  string
		vars  map[string]string
		tools map[string]string
	}{
		{"slurp", waylandVars, map[string]string{toolSlurp: "exec sleep 30", toolGrim: writeLastArg}},
		{"grim", waylandVars, map[string]string{toolSlurp: "printf '" + geometry + "\\n'", toolGrim: "exec sleep 30"}},
		{"maim", x11Vars, map[string]string{toolMaim: "exec sleep 30"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			toolDir(t, c.tools)
			ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			defer cancel()
			start := time.Now()
			_, _, err := captureWith(ctx, t, c.vars, noPortal())
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("capture = %v, want context.DeadlineExceeded", err)
			}
			if elapsed := time.Since(start); elapsed > 5*time.Second {
				t.Fatalf("capture returned after %v", elapsed)
			}
		})
	}
}

type swayRect struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

// TestRealWaylandCapture shows a QR as the sway background, selects it with a
// fake slurp and captures it with the real grim. Run it inside a sway session
// with swaymsg, swaybg and grim installed and SWAYSOCK set.
func TestRealWaylandCapture(t *testing.T) {
	if os.Getenv("TESSERA_SCREEN_TEST") != "wayland" {
		t.Skip("set TESSERA_SCREEN_TEST=wayland inside a sway session to capture the screen")
	}
	if os.Getenv("WAYLAND_DISPLAY") == "" {
		t.Fatal("TESSERA_SCREEN_TEST=wayland needs WAYLAND_DISPLAY")
	}
	const uri = "otpauth://totp/Sway:me?secret=JBSWY3DPEHPK3PXP&issuer=Sway"
	const qrSize, selection = 300, 360

	bg := filepath.Join(t.TempDir(), "qr.png")
	if err := qr.EncodePNG(uri, bg, qrSize); err != nil {
		t.Fatal(err)
	}
	raw, err := exec.Command("swaymsg", "-r", "-t", "get_outputs").Output()
	if err != nil {
		t.Fatalf("swaymsg get_outputs: %v", err)
	}
	var outputs []struct {
		Name   string   `json:"name"`
		Active bool     `json:"active"`
		Rect   swayRect `json:"rect"`
	}
	if err := json.Unmarshal(raw, &outputs); err != nil || len(outputs) == 0 || !outputs[0].Active {
		t.Fatalf("swaymsg get_outputs = %s, %v", raw, err)
	}
	o := outputs[0]
	if out, err := exec.Command("swaymsg", "output", o.Name, "bg", bg, "center", "#ffffff").CombinedOutput(); err != nil {
		t.Fatalf("swaymsg output bg: %v: %s", err, out)
	}
	geom := fmt.Sprintf("%d,%d %dx%d", o.Rect.X+(o.Rect.Width-selection)/2, o.Rect.Y+(o.Rect.Height-selection)/2, selection, selection)

	fakes := t.TempDir()
	fakeTool(t, fakes, toolSlurp, "printf '%s\\n' "+shQuote(geom))
	t.Setenv("PATH", fakes+string(os.PathListSeparator)+os.Getenv("PATH"))
	e := env{getenv: os.Getenv, lookPath: exec.LookPath, portal: noPortal().capture}

	deadline := time.Now().Add(20 * time.Second)
	var last string
	for time.Now().Before(deadline) {
		scratch, err := ScratchDir()
		if err != nil {
			t.Fatal(err)
		}
		path, err := capture(context.Background(), scratch, e)
		if err != nil {
			os.RemoveAll(scratch)
			t.Fatalf("capture: %v", err)
		}
		cfg, cfgErr := pngConfig(path)
		payloads, decodeErr := qr.DecodeFileAll(path)
		os.RemoveAll(scratch)
		if cfgErr != nil {
			t.Fatalf("captured image: %v", cfgErr)
		}
		if cfg.Width != selection || cfg.Height != selection {
			t.Fatalf("captured %dx%d, want the %dx%d selection", cfg.Width, cfg.Height, selection, selection)
		}
		if decodeErr == nil && slices.Contains(payloads, uri) {
			if calls := toolCalls(t, fakes, toolSlurp); len(calls) == 0 {
				t.Fatal("the fake slurp never ran")
			}
			t.Logf("decoded the background QR from a %dx%d grim capture at %s", cfg.Width, cfg.Height, geom)
			return
		}
		last = fmt.Sprint(payloads, decodeErr)
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("no capture decoded to the background QR; last result: %s", last)
}

func pngConfig(path string) (image.Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return image.Config{}, err
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	return cfg, err
}
