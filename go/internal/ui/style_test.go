package ui

import (
	"fmt"
	"image/color"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
)

func sameColor(a, b color.Color) bool {
	ar, ag, ab, aa := a.RGBA()
	br, bg, bb, ba := b.RGBA()
	return ar == br && ag == bg && ab == bb && aa == ba
}

func TestAdaptiveColorPicksByBackground(t *testing.T) {
	if !sameColor(Accent.Pick(true), lipgloss.Color("#E3B23C")) {
		t.Errorf("dark accent %v", Accent.Pick(true))
	}
	if !sameColor(Accent.Pick(false), lipgloss.Color("#A9791F")) {
		t.Errorf("light accent %v", Accent.Pick(false))
	}
}

func TestNewThemeFollowsTheBackground(t *testing.T) {
	for _, dark := range []bool{true, false} {
		th := NewTheme(dark)
		if th.Dark != dark {
			t.Fatalf("NewTheme(%v).Dark = %v", dark, th.Dark)
		}
		for name, pair := range map[string]struct {
			got  color.Color
			want AdaptiveColor
		}{
			"Title":    {th.Title.GetForeground(), Accent},
			"Accent":   {th.Accent.GetForeground(), Accent},
			"Warn":     {th.Warn.GetForeground(), Warning},
			"Code":     {th.Code.GetForeground(), Primary},
			"Subtle":   {th.Subtle.GetForeground(), Faint},
			"Selected": {th.Selected.GetBackground(), SelectedBg},
			"Track":    {th.Track().GetForeground(), TrackCol},
		} {
			if !sameColor(pair.got, pair.want.Pick(dark)) {
				t.Errorf("dark=%v %s = %v, want %v", dark, name, pair.got, pair.want.Pick(dark))
			}
		}
		if !th.Title.GetBold() || !th.Code.GetBold() {
			t.Errorf("dark=%v: title and code are not bold", dark)
		}
	}
	if themeFor(true).Dark != true || themeFor(false).Dark != false {
		t.Fatal("themeFor picks the wrong theme")
	}
}

func TestBarFill(t *testing.T) {
	for _, tc := range []struct{ remaining, period, width, want int }{
		{30, 30, 10, 10},
		{15, 30, 10, 5},
		{1, 30, 10, 0},
		{2, 30, 10, 1},
		{0, 30, 10, 0},
		{40, 30, 10, 10},
		{-5, 30, 10, 0},
		{10, 0, 10, 0},
		{10, 30, 0, 0},
		{10, 30, -3, 0},
	} {
		if got := BarFill(tc.remaining, tc.period, tc.width); got != tc.want {
			t.Errorf("BarFill(%d, %d, %d) = %d, want %d", tc.remaining, tc.period, tc.width, got, tc.want)
		}
	}
}

func TestBarWarnsAtFiveSeconds(t *testing.T) {
	th := NewTheme(true)
	if !sameColor(th.BarStyle(6).GetForeground(), Accent.Pick(true)) {
		t.Error("6s left is not the accent")
	}
	if !sameColor(th.BarStyle(5).GetForeground(), Warning.Pick(true)) {
		t.Error("5s left does not warn")
	}
	bar := ansi.Strip(th.Bar(15, 30, 10))
	if bar != "█████░░░░░" {
		t.Errorf("bar %q", bar)
	}
	if got := th.Bar(15, 0, 4); got != "    " {
		t.Errorf("bar without a period %q", got)
	}
	if got := ansi.StringWidth(th.Bar(7, 30, 14)); got != 14 {
		t.Errorf("bar is %d cells, want 14", got)
	}
}

func TestHandleHueIsStablePerAccount(t *testing.T) {
	th := NewTheme(true)
	a, b := th.HandleStyle("acct-1").GetForeground(), th.HandleStyle("acct-1").GetForeground()
	if !sameColor(a, b) {
		t.Fatal("the same account got two hues")
	}
	if !sameColor(a, NewTheme(false).HandleStyle("acct-1").GetForeground()) {
		t.Fatal("the hue changes with the background")
	}
	seen := map[string]bool{}
	for i := range 64 {
		seen[hueFor(fmt.Sprintf("ACME%d", i))] = true
	}
	if len(seen) != len(tileHues) {
		t.Fatalf("64 accounts use %d of %d hues", len(seen), len(tileHues))
	}
	if ansi.Strip(th.Handle("k", "gh  ")) != "gh  " {
		t.Fatal("Handle changes the text")
	}
}

func TestGroupCode(t *testing.T) {
	for in, want := range map[string]string{"123456": "123 456", "12345678": "12345678", "ABC12": "ABC12", "": ""} {
		if got := GroupCode(in); got != want {
			t.Errorf("GroupCode(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestViewProfileKeepsAttributesWhenColorIsOff(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  []string
		want colorprofile.Profile
	}{
		{"NO_COLOR", []string{"TTY_FORCE=1", "TERM=xterm-256color", "COLORTERM=truecolor", "NO_COLOR=1"}, colorprofile.ASCII},
		{"NO_COLOR=yes", []string{"TTY_FORCE=1", "TERM=xterm-256color", "NO_COLOR=yes"}, colorprofile.ASCII},
		{"CLICOLOR=0", []string{"TTY_FORCE=1", "TERM=xterm-256color", "CLICOLOR=0"}, colorprofile.ASCII},
		{"dumb terminal", []string{"TTY_FORCE=1", "TERM=dumb"}, colorprofile.ASCII},
		{"256 colors", []string{"TTY_FORCE=1", "TERM=xterm-256color"}, colorprofile.ANSI256},
		{"true color", []string{"TTY_FORCE=1", "TERM=xterm-256color", "COLORTERM=truecolor"}, colorprofile.TrueColor},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ViewProfile(pipeWriter(t), tc.env); got != tc.want {
				t.Errorf("ViewProfile = %v, want %v", got, tc.want)
			}
		})
	}
}
