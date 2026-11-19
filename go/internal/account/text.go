package account

import (
	"strings"
	"unicode"
)

func isRemovedTextRune(r rune) bool {
	switch {
	case r <= 0x1F, r == 0x7F, r >= 0x80 && r <= 0x9F:
		return true
	case r >= 0x202A && r <= 0x202E, r >= 0x2066 && r <= 0x2069, r == 0xFEFF:
		return true
	}
	return false
}

func CleanText(s string) string {
	return cleanRunes(s, false)
}

func CleanNote(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return cleanRunes(s, true)
}

func cleanRunes(s string, keepLineBreaks bool) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if isRemovedTextRune(r) && !(keepLineBreaks && (r == '\n' || r == '\t')) {
			continue
		}
		b.WriteRune(r)
	}
	return strings.TrimFunc(b.String(), unicode.IsSpace)
}
