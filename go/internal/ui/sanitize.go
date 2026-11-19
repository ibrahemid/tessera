package ui

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// isUnsafeRune reports whether r is one of the code points spec/otpauth.md
// § Text fields removes from stored text: C0 controls, DEL, C1 controls, the
// bidirectional embedding, override and isolate controls, and U+FEFF.
func isUnsafeRune(r rune) bool {
	switch {
	case r <= 0x1F, r == 0x7F, r >= 0x80 && r <= 0x9F:
		return true
	case r >= 0x202A && r <= 0x202E, r >= 0x2066 && r <= 0x2069:
		return true
	case r == 0xFEFF:
		return true
	}
	return false
}

// Sanitize returns s with every terminal control, bidi control and byte-order
// mark replaced by a visible escape (`\x1b`, `\u202e`), so text read from a
// vault or an import can be printed without the terminal acting on it. Bytes
// that are not valid UTF-8 are escaped the same way, since some terminals read
// a lone 0x9B as a control sequence introducer.
func Sanitize(s string) string {
	if !needsSanitize(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 8)
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == utf8.RuneError && size <= 1:
			fmt.Fprintf(&b, `\x%02x`, s[i])
		case isUnsafeRune(r) && r <= 0xFF:
			fmt.Fprintf(&b, `\x%02x`, r)
		case isUnsafeRune(r):
			fmt.Fprintf(&b, `\u%04x`, r)
		default:
			b.WriteString(s[i : i+size])
		}
		i += size
	}
	return b.String()
}

func needsSanitize(s string) bool {
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if (r == utf8.RuneError && size <= 1) || isUnsafeRune(r) {
			return true
		}
		i += size
	}
	return false
}
