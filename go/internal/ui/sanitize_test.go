package ui

import (
	"strings"
	"testing"
)

func TestSanitizeEscapesEveryControlClass(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"plain", "GitHub", "GitHub"},
		{"osc52", "a\x1b]52;c;SGVsbG8=\x07b", `a\x1b]52;c;SGVsbG8=\x07b`},
		{"c1 csi", "a\u009b31mb", `a\x9b31mb`},
		{"del", "a\x7fb", `a\x7fb`},
		{"newline and tab", "a\nb\tc", `a\x0ab\x09c`},
		{"nul", "a\x00b", `a\x00b`},
		{"rlo", "pay\u202eflow", `pay\u202eflow`},
		{"isolate", "a\u2066b\u2069", `a\u2066b\u2069`},
		{"bom", "\ufeffACME", `\ufeffACME`},
		{"invalid utf8 csi byte", "a\x9b31m", `a\x9b31m`},
		{"arabic with rlm kept", "بنك\u200fX", "بنك\u200fX"},
		{"zwj kept", "a\u200db", "a\u200db"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Sanitize(c.in); got != c.want {
				t.Errorf("Sanitize(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestSanitizeLeavesNoRawControl(t *testing.T) {
	var all strings.Builder
	for r := rune(0); r <= 0x9F; r++ {
		all.WriteRune(r)
	}
	for _, r := range []rune{0x202A, 0x202B, 0x202C, 0x202D, 0x202E, 0x2066, 0x2067, 0x2068, 0x2069, 0xFEFF} {
		all.WriteRune(r)
	}
	got := Sanitize(all.String())
	for _, r := range got {
		if isUnsafeRune(r) {
			t.Fatalf("Sanitize left %U in %q", r, got)
		}
	}
}
