package tui

import (
	"errors"
	"flag"
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/ibrahemid/tessera/go/internal/account"
)

var update = flag.Bool("update", false, "rewrite the golden frames in testdata")

type goldenFrame struct {
	name  string
	accts func() []account.Account
	setup func(h *harness)
}

func goldenFrames() []goldenFrame {
	return []goldenFrame{
		{"list", fixture, func(h *harness) {}},
		{"search", fixture, func(h *harness) { h.press("/"); h.typeText("work") }},
		{"keys", fixture, func(h *harness) { h.press("?") }},
		{"folders", fixture, func(h *harness) { h.press("f") }},
		{"add", fixture, func(h *harness) { h.press("a"); h.paste("otpauth://totp/Mail?secret=JBSWY3DPEHPK3PXP") }},
		{"edit", fixture, func(h *harness) { h.press("e", "tab", "tab", "tab") }},
		{"trash", fixture, func(h *harness) { h.selectID("e"); h.press("d", "T") }},
		{"purge", fixture, func(h *harness) { h.press("T", "d") }},
		{"hidden", fixture, func(h *harness) { h.press("h", "down") }},
		{"hotp", fixture, func(h *harness) { h.selectID("c"); h.press("n") }},
		{"copied-next", fixture, func(h *harness) { h.at(28); h.press("enter") }},
		{"locked", fixture, func(h *harness) { h.m.opts.LockAfter = time.Minute; h.at(70) }},
		{"rekeyed", fixture, func(h *harness) {
			h.reloadFn = func() ([]account.Account, bool, error) { return nil, false, ErrKeyReplaced }
			h.send(pollMsg(h.now))
			h.press("x")
			h.typeText("pass")
		}},
		{"error", fixture, func(h *harness) {
			h.mutateErr = errors.New("enter an issuer or an account name")
			h.press("e", "ctrl+u", "tab", "ctrl+u", "enter")
		}},
		{"empty", func() []account.Account { return nil }, func(h *harness) {}},
		{"long", func() []account.Account { return many(40) }, func(h *harness) { h.press("G", "up", "up") }},
	}
}

func TestGoldenFrames(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {40, 12}} {
		for _, theme := range []string{"plain", "dark", "light"} {
			if theme == "light" && size[0] != 80 {
				continue
			}
			name := fmt.Sprintf("%dx%d-%s", size[0], size[1], theme)
			t.Run(name, func(t *testing.T) {
				var b strings.Builder
				for _, f := range goldenFrames() {
					h := newSizedHarness(t, f.accts(), size[0], size[1])
					if theme == "light" {
						h.send(tea.BackgroundColorMsg{Color: color.White})
					}
					f.setup(h)
					frame := h.m.View().Content
					checkFrameSize(t, frame, size[0], size[1], f.name)
					if theme == "plain" {
						frame = ansi.Strip(frame)
					}
					fmt.Fprintf(&b, "=== %s\n%s\n", f.name, frame)
				}
				compareGolden(t, filepath.Join("testdata", name+".golden"), b.String())
			})
		}
	}
}

func checkFrameSize(t *testing.T, frame string, width, height int, name string) {
	t.Helper()
	lines := strings.Split(frame, "\n")
	if len(lines) != height {
		t.Errorf("%s: %d lines, want %d", name, len(lines), height)
	}
	for i, l := range lines {
		if w := ansi.StringWidth(l); w > width {
			t.Errorf("%s: line %d is %d cells, over %d", name, i, w, width)
		}
	}
}

func compareGolden(t *testing.T, path, got string) {
	t.Helper()
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run go test -run TestGoldenFrames -update)", err)
	}
	if string(want) == got {
		return
	}
	wl, gl := strings.Split(string(want), "\n"), strings.Split(got, "\n")
	for i := range max(len(wl), len(gl)) {
		var w, g string
		if i < len(wl) {
			w = wl[i]
		}
		if i < len(gl) {
			g = gl[i]
		}
		if w != g {
			t.Fatalf("%s differs at line %d:\nwant %q\ngot  %q", path, i+1, w, g)
		}
	}
}
