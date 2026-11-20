package ui

import (
	"errors"
	"image/color"
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
)

func pinColorProfile(t *testing.T, p colorprofile.Profile) {
	t.Helper()
	output.mu.Lock()
	prev := output.profile
	output.profile = p
	output.mu.Unlock()
	t.Cleanup(func() { setOutputProfile(prev) })
}

func pinBackground(t *testing.T, dark bool) {
	t.Helper()
	output.mu.Lock()
	prevDark, prevResolved := output.dark, output.resolved
	output.dark, output.resolved = dark, true
	output.mu.Unlock()
	t.Cleanup(func() {
		output.mu.Lock()
		output.dark, output.resolved = prevDark, prevResolved
		output.mu.Unlock()
	})
}

func forbidBackgroundQuery(t *testing.T) {
	t.Helper()
	prev := queryBackground
	queryBackground = func() (color.Color, error) {
		t.Error("the terminal was asked for its background")
		return nil, errors.New("not allowed")
	}
	t.Cleanup(func() { queryBackground = prev })
	output.mu.Lock()
	prevDark, prevResolved := output.dark, output.resolved
	output.resolved = false
	output.mu.Unlock()
	t.Cleanup(func() {
		output.mu.Lock()
		output.dark, output.resolved = prevDark, prevResolved
		output.mu.Unlock()
	})
}

func pipeWriter(t *testing.T) *os.File {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close(); w.Close() })
	return w
}

func TestDetectProfile(t *testing.T) {
	cases := []struct {
		name string
		env  []string
		want colorprofile.Profile
	}{
		{"pipe with a color terminal", []string{"TERM=xterm-256color", "COLORTERM=truecolor"}, colorprofile.NoTTY},
		{"pipe with NO_COLOR and CLICOLOR_FORCE", []string{"TERM=xterm-256color", "NO_COLOR=1", "CLICOLOR_FORCE=1"}, colorprofile.NoTTY},
		{"forced terminal with NO_COLOR", []string{"TTY_FORCE=1", "TERM=xterm-256color", "NO_COLOR=1"}, colorprofile.NoTTY},
		{"forced terminal with NO_COLOR=yes", []string{"TTY_FORCE=1", "TERM=xterm-256color", "NO_COLOR=yes"}, colorprofile.NoTTY},
		{"forced terminal with CLICOLOR=0", []string{"TTY_FORCE=1", "TERM=xterm-256color", "CLICOLOR=0"}, colorprofile.NoTTY},
		{"dumb terminal", []string{"TTY_FORCE=1", "TERM=dumb"}, colorprofile.NoTTY},
		{"pipe with CLICOLOR_FORCE", []string{"TERM=xterm-256color", "CLICOLOR_FORCE=1"}, colorprofile.ANSI256},
		{"forced 256-color terminal", []string{"TTY_FORCE=1", "TERM=xterm-256color"}, colorprofile.ANSI256},
		{"forced truecolor terminal", []string{"TTY_FORCE=1", "TERM=xterm-256color", "COLORTERM=truecolor"}, colorprofile.TrueColor},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := detectProfile(pipeWriter(t), tc.env); got != tc.want {
				t.Errorf("detectProfile = %v, want %v", got, tc.want)
			}
		})
	}
}

func everyRendering() map[string]string {
	return map[string]string{
		"TitleStyle":   TitleStyle.Render("Tessera"),
		"CodeStyle":    CodeStyle.Render("123 456"),
		"CodeCopied":   CodeCopied.Render("123 456"),
		"IssuerStyle":  IssuerStyle.Render("ACME"),
		"SubtleStyle":  SubtleStyle.Render("x"),
		"AccentStyle":  AccentStyle.Render("x"),
		"WarnStyle":    WarnStyle.Render("x"),
		"SelectedRow":  SelectedRow.Render("x"),
		"SectionLabel": SectionLabel.Render("x"),
		"Handle":       Handle("ACMEx", "acme"),
		"Bar":          Bar(3, 30, 10),
	}
}

func TestPlainOutputHasNoEscapesAndNeverQueriesTheTerminal(t *testing.T) {
	pinColorProfile(t, colorprofile.NoTTY)
	forbidBackgroundQuery(t)
	for name, got := range everyRendering() {
		if strings.ContainsRune(got, '\x1b') {
			t.Errorf("%s = %q, want no escape sequences", name, got)
		}
	}
	if got := CodeStyle.Render("a\tb"); got != "a    b" {
		t.Errorf("CodeStyle.Render(tab) = %q, want tabs expanded as before", got)
	}
	if got := Bar(15, 30, 4); got != "██░░" {
		t.Errorf("Bar(15, 30, 4) = %q", got)
	}
}

func TestColorOutputFollowsBackground(t *testing.T) {
	pinColorProfile(t, colorprofile.TrueColor)
	pinBackground(t, true)
	if got := AccentStyle.Render("x"); !strings.Contains(got, "38;2;227;178;60") {
		t.Errorf("dark AccentStyle = %q, want the dark accent", got)
	}
	pinBackground(t, false)
	if got := AccentStyle.Render("x"); !strings.Contains(got, "38;2;169;121;31") {
		t.Errorf("light AccentStyle = %q, want the light accent", got)
	}
}

func TestColorOutputDownsamples(t *testing.T) {
	pinBackground(t, true)
	pinColorProfile(t, colorprofile.ANSI256)
	got := AccentStyle.Render("x")
	if !strings.Contains(got, "38;5;") || strings.Contains(got, "38;2;") {
		t.Errorf("256-color AccentStyle = %q", got)
	}
	pinColorProfile(t, colorprofile.ANSI)
	got = AccentStyle.Render("x")
	if !strings.Contains(got, "\x1b[") || strings.Contains(got, "38;5;") || strings.Contains(got, "38;2;") {
		t.Errorf("16-color AccentStyle = %q", got)
	}
}

func TestResolveDark(t *testing.T) {
	env := func(kv ...string) func(string) string {
		m := map[string]string{}
		for i := 0; i+1 < len(kv); i += 2 {
			m[kv[i]] = kv[i+1]
		}
		return func(k string) string { return m[k] }
	}
	safe := terminalFacts{goos: "linux", stdinTTY: true, stdoutTTY: true, foreground: true, getenv: env("TERM", "xterm-256color")}
	light := func() (color.Color, error) { return color.RGBA{0xfa, 0xfa, 0xfa, 0xff}, nil }
	dark := func() (color.Color, error) { return color.RGBA{0x10, 0x10, 0x14, 0xff}, nil }

	if resolveDark(safe, light) {
		t.Error("light background reported dark")
	}
	if !resolveDark(safe, dark) {
		t.Error("dark background reported light")
	}

	noQuery := func() (color.Color, error) {
		t.Error("the terminal was asked for its background")
		return nil, errors.New("not allowed")
	}
	unsafe := map[string]terminalFacts{}
	f := safe
	f.stdoutTTY = false
	unsafe["stdout piped"] = f
	f = safe
	f.stdinTTY = false
	unsafe["stdin piped"] = f
	f = safe
	f.foreground = false
	unsafe["background job"] = f
	f = safe
	f.goos = "windows"
	unsafe["windows"] = f
	for _, name := range []string{"dumb", "", "screen-256color", "tmux-256color"} {
		f = safe
		f.getenv = env("TERM", name)
		unsafe["TERM="+name] = f
	}
	for name, facts := range unsafe {
		if !resolveDark(facts, noQuery) {
			t.Errorf("%s: default should be dark", name)
		}
	}

	failed := func() (color.Color, error) { return nil, errors.New("timeout") }
	f = safe
	f.getenv = env("TERM", "xterm", "COLORFGBG", "0;15")
	if resolveDark(f, failed) {
		t.Error("COLORFGBG 0;15 should be light")
	}
	f.stdoutTTY = false
	f.getenv = env("COLORFGBG", "15;default;0")
	if !resolveDark(f, noQuery) {
		t.Error("COLORFGBG 15;default;0 should be dark")
	}
	f.getenv = env("COLORFGBG", "garbage")
	if !resolveDark(f, noQuery) {
		t.Error("unparseable COLORFGBG should fall back to dark")
	}
}
