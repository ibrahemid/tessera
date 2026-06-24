// Package otp implements RFC 4226 (HOTP), RFC 6238 (TOTP), and the Steam Guard
// variant from the Go standard library only. Keeping the security-critical code
// dependency-free lets it be pinned byte-for-byte against the Swift core via the
// shared interop vectors.
package otp

import (
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"hash"
	"math"
	"strings"
	"time"
)

// Algorithm selects the HMAC hash used by HOTP/TOTP.
type Algorithm int

const (
	SHA1 Algorithm = iota
	SHA256
	SHA512
)

func (a Algorithm) String() string {
	switch a {
	case SHA1:
		return "SHA1"
	case SHA256:
		return "SHA256"
	case SHA512:
		return "SHA512"
	default:
		return "UNKNOWN"
	}
}

func (a Algorithm) new() func() hash.Hash {
	switch a {
	case SHA256:
		return sha256.New
	case SHA512:
		return sha512.New
	default:
		return sha1.New
	}
}

// ParseAlgorithm maps an otpauth algorithm string (case-insensitive) to an Algorithm.
func ParseAlgorithm(s string) (Algorithm, error) {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "", "SHA1":
		return SHA1, nil
	case "SHA256":
		return SHA256, nil
	case "SHA512":
		return SHA512, nil
	default:
		return 0, fmt.Errorf("otp: unsupported algorithm %q", s)
	}
}

// dt performs RFC 4226 dynamic truncation, returning the 31-bit integer.
func dt(secret []byte, counter uint64, alg Algorithm) []byte {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], counter)
	mac := hmac.New(alg.new(), secret)
	mac.Write(buf[:])
	return mac.Sum(nil)
}

func truncate(sum []byte) uint32 {
	offset := sum[len(sum)-1] & 0x0f
	return (uint32(sum[offset]&0x7f) << 24) |
		(uint32(sum[offset+1]) << 16) |
		(uint32(sum[offset+2]) << 8) |
		uint32(sum[offset+3])
}

// HOTP computes the RFC 4226 code for the given counter. digits must be 6-8.
func HOTP(secret []byte, counter uint64, digits int, alg Algorithm) (string, error) {
	if len(secret) == 0 {
		return "", fmt.Errorf("otp: empty secret")
	}
	if digits < 6 || digits > 8 {
		return "", fmt.Errorf("otp: digits must be 6-8, got %d", digits)
	}
	bin := truncate(dt(secret, counter, alg))
	mod := uint32(math.Pow10(digits))
	return fmt.Sprintf("%0*d", digits, bin%mod), nil
}

// TOTP computes the RFC 6238 code at time t with T0=0.
func TOTP(secret []byte, t time.Time, period, digits int, alg Algorithm) (string, error) {
	if period <= 0 {
		return "", fmt.Errorf("otp: period must be positive, got %d", period)
	}
	counter := uint64(t.Unix() / int64(period))
	return HOTP(secret, counter, digits, alg)
}

// RemainingSeconds returns how many seconds the current TOTP step is still valid.
func RemainingSeconds(t time.Time, period int) int {
	if period <= 0 {
		return 0
	}
	return period - int(t.Unix()%int64(period))
}

const steamAlphabet = "23456789BCDFGHJKMNPQRTVWXY"

// DecodeSteamSecret decodes a Steam shared_secret (base64) to raw key bytes.
func DecodeSteamSecret(b64 string) ([]byte, error) {
	b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(b64))
	if err != nil {
		return nil, fmt.Errorf("otp: steam secret base64: %w", err)
	}
	return b, nil
}

// Steam computes the 5-character Steam Guard code (HMAC-SHA1, period 30).
func Steam(secret []byte, t time.Time) (string, error) {
	if len(secret) == 0 {
		return "", fmt.Errorf("otp: empty secret")
	}
	counter := uint64(t.Unix() / 30)
	full := truncate(dt(secret, counter, SHA1))
	var b [5]byte
	for i := 0; i < 5; i++ {
		b[i] = steamAlphabet[full%uint32(len(steamAlphabet))]
		full /= uint32(len(steamAlphabet))
	}
	return string(b[:]), nil
}
