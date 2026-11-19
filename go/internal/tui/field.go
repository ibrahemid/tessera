package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/ibrahemid/tessera/go/internal/ui"
)

// field is a one-line text input. A masked field draws each character as a
// dot; a multiline field accepts ctrl+j for a line break and shows it as ↵.
type field struct {
	label     string
	value     []rune
	cursor    int
	masked    bool
	multiline bool
	verbatim  bool
}

func newField(label, value string) field {
	r := []rune(value)
	return field{label: label, value: r, cursor: len(r)}
}

func (f field) String() string { return string(f.value) }

func (f *field) set(s string) {
	f.value = []rune(s)
	f.cursor = len(f.value)
}

func (f *field) insert(s string) {
	r := []rune(s)
	v := make([]rune, 0, len(f.value)+len(r))
	v = append(v, f.value[:f.cursor]...)
	v = append(v, r...)
	v = append(v, f.value[f.cursor:]...)
	f.value = v
	f.cursor += len(r)
}

// paste inserts pasted text. Outside a multiline field, line breaks and tabs
// become spaces so a pasted value stays on one line. A verbatim field keeps
// spaces and stops at the first line break, as a passphrase prompt does.
func (f *field) paste(s string) {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	if f.verbatim {
		if i := strings.IndexAny(s, "\r\n"); i >= 0 {
			s = s[:i]
		}
		f.insert(s)
		return
	}
	if f.multiline {
		s = strings.ReplaceAll(s, "\r", "\n")
	} else {
		s = strings.NewReplacer("\r", " ", "\n", " ", "\t", " ").Replace(s)
		if f.masked {
			s = strings.TrimSpace(s)
		}
	}
	f.insert(s)
}

// handle applies an editing key and reports whether the field used it.
func (f *field) handle(k tea.KeyPressMsg) bool {
	switch k.String() {
	case "left":
		f.cursor = max(f.cursor-1, 0)
	case "right":
		f.cursor = min(f.cursor+1, len(f.value))
	case "home":
		f.cursor = 0
	case "end":
		f.cursor = len(f.value)
	// Terminals that send ^H for backspace report it as ctrl+h.
	case "backspace", "ctrl+h":
		if f.cursor > 0 {
			f.value = append(f.value[:f.cursor-1:f.cursor-1], f.value[f.cursor:]...)
			f.cursor--
		}
	case "delete":
		if f.cursor < len(f.value) {
			f.value = append(f.value[:f.cursor:f.cursor], f.value[f.cursor+1:]...)
		}
	case "ctrl+u":
		f.value = append([]rune{}, f.value[f.cursor:]...)
		f.cursor = 0
	case "ctrl+k":
		f.value = f.value[:f.cursor:f.cursor]
	case "ctrl+j":
		if !f.multiline {
			return false
		}
		f.insert("\n")
	case "space":
		f.insert(" ")
	default:
		if k.Text == "" || k.Mod.Contains(tea.ModCtrl) || k.Mod.Contains(tea.ModAlt) {
			return false
		}
		f.insert(k.Text)
	}
	return true
}

func (f field) glyph(r rune) string {
	switch {
	case f.masked:
		return "•"
	case r == '\n':
		return "↵"
	}
	return ui.Sanitize(string(r))
}

// render draws the value in width cells, scrolled so the cursor stays in view.
// The cursor is a reverse-video cell when the field has focus.
func (f field) render(width int, focused bool, text, cursor lipgloss.Style) string {
	if width <= 0 {
		return ""
	}
	cells := make([]string, len(f.value)+1)
	for i, r := range f.value {
		cells[i] = f.glyph(r)
	}
	cells[len(f.value)] = " "
	last := len(cells) - 1
	if !focused {
		last = len(f.value) - 1
	}
	start := 0
	used := 0
	for i := min(f.cursor, last); i >= 0; i-- {
		w := ansi.StringWidth(cells[i])
		if used+w > width {
			break
		}
		used += w
		start = i
	}
	if !focused {
		start = 0
	}
	var before, after strings.Builder
	at := ""
	used = 0
	for i := start; i <= last; i++ {
		w := ansi.StringWidth(cells[i])
		if used+w > width {
			break
		}
		used += w
		switch {
		case focused && i == f.cursor:
			at = cells[i]
		case focused && i > f.cursor:
			after.WriteString(cells[i])
		default:
			before.WriteString(cells[i])
		}
	}
	var out string
	if before.Len() > 0 {
		out += text.Render(before.String())
	}
	if at != "" {
		out += cursor.Render(at)
	}
	if after.Len() > 0 {
		out += text.Render(after.String())
	}
	return out
}
