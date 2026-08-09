// Package base32x implements RFC 4648 base32. Decoding is lenient about how a
// key is written down (case-insensitive, optional padding, embedded whitespace
// and dashes tolerated) and strict about what it means: input that does not
// re-encode to itself is rejected rather than turned into a secret the user was
// never given. The vault stores raw key bytes; base32 is used only when
// parsing/emitting otpauth:// URIs.
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

// badQuantum holds the group lengths that no byte count can produce: a base32
// quantum of 8 characters encodes 5 bytes, so a trailing group of 1, 3, or 6
// characters is always truncated input.
var badQuantum = map[int]bool{1: true, 3: true, 6: true}

// Decode parses base32: whitespace, '-' and '=' padding are stripped, input is
// uppercased, and padding is restored before decoding. Input that cannot be
// re-encoded to itself is rejected — an empty secret, a truncated final group,
// or non-zero bits past the last whole byte all yield a key that does not round
// -trip, which would silently produce wrong codes forever.
func Decode(s string) ([]byte, error) {
	var sb strings.Builder
	sb.Grow(len(s))
	for _, r := range s {
		switch r {
		case ' ', '\t', '\n', '\r', '-', '=':
			continue
		}
		sb.WriteRune(r)
	}
	clean := strings.ToUpper(sb.String())
	if clean == "" {
		return nil, ErrInvalid
	}
	if badQuantum[len(clean)%8] {
		return nil, ErrInvalid
	}
	padded := clean
	if pad := len(padded) % 8; pad != 0 {
		padded += strings.Repeat("=", 8-pad)
	}
	out, err := base32.StdEncoding.DecodeString(padded)
	if err != nil {
		return nil, ErrInvalid
	}
	// Non-canonical trailing bits decode without error but do not re-encode to
	// the input; such a secret is not the secret the user was given.
	if EncodeNoPad(out) != clean {
		return nil, ErrInvalid
	}
	return out, nil
}
