package tui

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
)

func TestRunRefusesWithoutATerminal(t *testing.T) {
	inR, inW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { inR.Close(); inW.Close(); outR.Close() })
	if err := run(Options{Accounts: fixture()}, inR, outW, []string{"TERM=xterm-256color"}); !errors.Is(err, ErrNoTerminal) {
		t.Fatalf("run = %v, want ErrNoTerminal", err)
	}
	outW.Close()
	if b, _ := io.ReadAll(outR); len(b) != 0 {
		t.Fatalf("wrote %q to a pipe", b)
	}
}

var colorSGR = regexp.MustCompile(`\x1b\[[0-9;:]*(?:3[0-9]|4[0-9]|9[0-7]|10[0-7])[;:m]`)

func runProgram(t *testing.T, profile colorprofile.Profile) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var out bytes.Buffer
	env := []string{"TERM=xterm-256color", "NO_COLOR=1"}
	opts := append(programOptions(strings.NewReader(""), &out, env),
		tea.WithContext(ctx), tea.WithWindowSize(80, 24), tea.WithoutSignalHandler())
	if profile != colorprofile.Unknown {
		opts = append(opts, tea.WithColorProfile(profile))
	}
	m := New(Options{Accounts: fixture(), Now: func() time.Time { return base.Add(10 * time.Second) }})
	p := tea.NewProgram(m, opts...)
	go func() {
		time.Sleep(300 * time.Millisecond)
		p.Send(key("q"))
	}()
	if _, err := p.Run(); err != nil {
		t.Fatalf("program: %v", err)
	}
	return out.String()
}

func TestProgramHonoursNoColor(t *testing.T) {
	got := runProgram(t, colorprofile.Unknown)
	if !strings.Contains(got, "Tessera") || !strings.Contains(got, "GitHub") {
		t.Fatalf("the view did not draw: %q", got)
	}
	if m := colorSGR.FindString(got); m != "" {
		t.Fatalf("NO_COLOR output has a color sequence %q", m)
	}
	if colorSGR.FindString(runProgram(t, colorprofile.TrueColor)) == "" {
		t.Fatal("a color terminal got no color, so the check above proves nothing")
	}
}
