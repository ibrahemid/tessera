// Package base32x implements RFC 4648 base32 with the lenient decoding the
// otpauth import boundary requires: case-insensitive, optional padding, and
// embedded whitespace tolerated. The vault stores raw key bytes; base32 is used
// only when parsing/emitting otpauth:// URIs.
package base32x

import (
	"encoding/base32"
	"errors"
	"strings"
)

// ErrInvalid reports malformed base32 input.
var ErrInvalid = errors.New("base32x: invalid base32 input")

// Encode returns standard RFC 4648 base32 with '=' padding.
func Encode(b []byte) string {
	return base32.StdEncoding.EncodeToString(b)
}

// EncodeNoPad returns standard RFC 4648 base32 without padding (otpauth secrets).
func EncodeNoPad(b []byte) string {
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b)
}

// Decode parses base32 leniently: whitespace is stripped, input is
// uppercased, and missing '=' padding is restored before decoding.
func Decode(s string) ([]byte, error) {
	var sb strings.Builder
	sb.Grow(len(s))
	for _, r := range s {
		switch r {
		case ' ', '\t', '\n', '\r', '-':
			continue
		}
		sb.WriteRune(r)
	}
	clean := strings.ToUpper(sb.String())
	if clean == "" {
		return []byte{}, nil
	}
	if pad := len(clean) % 8; pad != 0 {
		clean += strings.Repeat("=", 8-pad)
	}
	out, err := base32.StdEncoding.DecodeString(clean)
	if err != nil {
		return nil, ErrInvalid
	}
	return out, nil
}
