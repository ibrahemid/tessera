package ui

import (
	"image/color"
	"os"
	"runtime"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"golang.org/x/term"
)

// terminalFacts is what decides whether asking the terminal for its background
// is safe: a query written to a pipe, a backgrounded job or a multiplexer that
// does not answer either blocks or leaves the reply in the user's input.
type terminalFacts struct {
	goos       string
	stdinTTY   bool
	stdoutTTY  bool
	foreground bool
	getenv     func(string) string
}

func currentTerminal() terminalFacts {
	in, out := int(os.Stdin.Fd()), int(os.Stdout.Fd())
	stdinTTY, stdoutTTY := term.IsTerminal(in), term.IsTerminal(out)
	return terminalFacts{
		goos:       runtime.GOOS,
		stdinTTY:   stdinTTY,
		stdoutTTY:  stdoutTTY,
		foreground: stdinTTY && isForeground(in),
		getenv:     os.Getenv,
	}
}

var queryBackground = func() (color.Color, error) {
	return lipgloss.BackgroundColor(os.Stdin, os.Stdout)
}

func (t terminalFacts) canQuery() bool {
	if t.goos == "windows" || !t.stdinTTY || !t.stdoutTTY || !t.foreground {
		return false
	}
	name := t.getenv("TERM")
	switch {
	case name == "", name == "dumb":
		return false
	case strings.HasPrefix(name, "screen"), strings.HasPrefix(name, "tmux"):
		return false
	}
	return true
}

// resolveDark reports whether the terminal background is dark: from the
// terminal's answer when asking is safe, else from $COLORFGBG, else dark.
func resolveDark(t terminalFacts, query func() (color.Color, error)) bool {
	if t.canQuery() {
		if c, err := query(); err == nil && c != nil {
			return isDarkColor(c)
		}
	}
	if dark, ok := colorFGBGDark(t.getenv("COLORFGBG")); ok {
		return dark
	}
	return true
}

func colorFGBGDark(v string) (dark, ok bool) {
	if v == "" {
		return false, false
	}
	fields := strings.Split(v, ";")
	n, err := strconv.Atoi(fields[len(fields)-1])
	if err != nil || n < 0 || n > 255 {
		return false, false
	}
	return isDarkColor(ansi.IndexedColor(n)), true
}

func isDarkColor(c color.Color) bool {
	r, g, b, _ := c.RGBA()
	hi := max(r, g, b)
	lo := min(r, g, b)
	return float64(hi+lo)/2/0xffff < 0.5
}
