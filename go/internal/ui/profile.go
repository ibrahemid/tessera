package ui

import (
	"io"
	"os"
	"strings"
	"sync"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
)

var output struct {
	mu       sync.Mutex
	profile  colorprofile.Profile
	dark     bool
	resolved bool
}

// detectProfile picks the color depth for w. Output that is not a terminal,
// NO_COLOR, CLICOLOR=0 and a terminal with no color support all mean plain
// text with no escape sequences at all.
func detectProfile(w io.Writer, env []string) colorprofile.Profile {
	lookup := func(key string) string {
		for _, kv := range env {
			if k, v, ok := strings.Cut(kv, "="); ok && k == key {
				return v
			}
		}
		return ""
	}
	if lookup("NO_COLOR") != "" {
		return colorprofile.NoTTY
	}
	if lookup("CLICOLOR") == "0" {
		if f := lookup("CLICOLOR_FORCE"); f == "" || f == "0" {
			return colorprofile.NoTTY
		}
	}
	p := colorprofile.Detect(w, env)
	if p <= colorprofile.ASCII {
		return colorprofile.NoTTY
	}
	return p
}

// ViewProfile picks the color depth for a full-screen view on w. It follows
// the rules of plain output, but where those turn color off it keeps bold and
// reverse video, which the view needs to draw its cursor.
func ViewProfile(w io.Writer, env []string) colorprofile.Profile {
	if p := detectProfile(w, env); p > colorprofile.NoTTY {
		return p
	}
	return colorprofile.ASCII
}

func outputProfile() colorprofile.Profile {
	output.mu.Lock()
	defer output.mu.Unlock()
	if output.profile == colorprofile.Unknown {
		output.profile = detectProfile(os.Stdout, os.Environ())
	}
	return output.profile
}

// setOutputProfile replaces the detected color depth of stdout.
func setOutputProfile(p colorprofile.Profile) {
	output.mu.Lock()
	defer output.mu.Unlock()
	output.profile = p
}

func outputDark() bool {
	output.mu.Lock()
	defer output.mu.Unlock()
	if !output.resolved {
		output.dark = resolveDark(currentTerminal(), queryBackground)
		output.resolved = true
	}
	return output.dark
}

// render draws styled text for stdout. Plain output never asks the terminal
// for its background.
func render(draw func(Theme) string) string {
	p := outputProfile()
	if p <= colorprofile.NoTTY {
		return ansi.Strip(draw(darkTheme))
	}
	return downsample(draw(themeFor(outputDark())), p)
}

func downsample(s string, p colorprofile.Profile) string {
	if p == colorprofile.TrueColor {
		return s
	}
	var b strings.Builder
	w := &colorprofile.Writer{Forward: &b, Profile: p}
	if _, err := w.WriteString(s); err != nil {
		return ansi.Strip(s)
	}
	return b.String()
}
