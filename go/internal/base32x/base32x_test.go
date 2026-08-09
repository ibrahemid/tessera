package base32x

import (
	"testing"

	"github.com/ibrahemid/tessera/go/internal/spectest"
)

type vectors struct {
	Base32 struct {
		Encode []struct {
			ASCII string `json:"ascii"`
			B32   string `json:"b32"`
		} `json:"encode"`
		DecodeLenient []struct {
			Input string `json:"input"`
			ASCII string `json:"ascii"`
		} `json:"decode_lenient"`
		DecodeReject []struct {
			Input string `json:"input"`
			Why   string `json:"why"`
		} `json:"decode_reject"`
	} `json:"base32"`
}

func TestEncode(t *testing.T) {
	var v vectors
	spectest.Load(t, &v)
	for _, c := range v.Base32.Encode {
		if got := Encode([]byte(c.ASCII)); got != c.B32 {
			t.Errorf("Encode(%q) = %q, want %q", c.ASCII, got, c.B32)
		}
	}
}

func TestDecodeLenient(t *testing.T) {
	var v vectors
	spectest.Load(t, &v)
	for _, c := range v.Base32.DecodeLenient {
		got, err := Decode(c.Input)
		if err != nil {
			t.Errorf("Decode(%q) error: %v", c.Input, err)
			continue
		}
		if string(got) != c.ASCII {
			t.Errorf("Decode(%q) = %q, want %q", c.Input, got, c.ASCII)
		}
	}
}

// TestDecodeRejectVectors runs the shared strictness vectors; the Swift core
// runs the same list, so the two implementations refuse the same secrets.
func TestDecodeRejectVectors(t *testing.T) {
	var v vectors
	spectest.Load(t, &v)
	if len(v.Base32.DecodeReject) == 0 {
		t.Fatal("spec base32.decode_reject is empty")
	}
	for _, c := range v.Base32.DecodeReject {
		if got, err := Decode(c.Input); err == nil {
			t.Errorf("Decode(%q) = %q, want an error (%s)", c.Input, got, c.Why)
		}
	}
}

func TestDecodeRoundTripNoPad(t *testing.T) {
	// The empty string is excluded: an empty secret is rejected, not decoded to
	// zero bytes (see TestDecodeRejectsEmpty).
	for _, s := range []string{"f", "fo", "foo", "foob", "fooba", "foobar"} {
		enc := EncodeNoPad([]byte(s))
		dec, err := Decode(enc)
		if err != nil {
			t.Fatalf("Decode(%q): %v", enc, err)
		}
		if string(dec) != s {
			t.Errorf("roundtrip %q -> %q -> %q", s, enc, dec)
		}
	}
}

func TestDecodeRejectsInvalid(t *testing.T) {
	if _, err := Decode("MZXW6YT!"); err == nil {
		t.Error("expected error for invalid base32 char")
	}
}

// TestDecodeRejectsEmpty pins that empty input is an error rather than an empty
// secret: an account whose secret decoded to nothing would be accepted at the
// import boundary and only fail later, at code generation.
func TestDecodeRejectsEmpty(t *testing.T) {
	for _, in := range []string{"", "   ", "\t\n", "-", "=", "  == -- "} {
		if got, err := Decode(in); err == nil {
			t.Errorf("Decode(%q) = %q, want an error", in, got)
		}
	}
}

// TestDecodeRejectsBadQuantum covers group lengths no byte count can produce.
func TestDecodeRejectsBadQuantum(t *testing.T) {
	for _, in := range []string{"M", "MZX", "MZXW6Y", "MZXW6YTBM", "MZXW6YTBMZX"} {
		if got, err := Decode(in); err == nil {
			t.Errorf("Decode(%q) = %q, want an error (length %%8 = %d)", in, got, len(in)%8)
		}
	}
}

// TestDecodeRejectsNonCanonicalPadBits covers input whose trailing bits past the
// last whole byte are non-zero: it decodes without error under RFC 4648 but does
// not re-encode to itself, so the stored secret is not the one handed over.
func TestDecodeRejectsNonCanonicalPadBits(t *testing.T) {
	// "MY" encodes 'f' (bits 01100110 + 00 padding). "MZ" shares the first byte
	// but sets a padding bit, so it round-trips back to "MY", not "MZ".
	if _, err := Decode("MZ"); err == nil {
		t.Error("Decode(\"MZ\") should be rejected: trailing pad bits are not zero")
	}
	if got, err := Decode("MY"); err != nil || string(got) != "f" {
		t.Errorf("Decode(\"MY\") = %q, %v; want \"f\", nil", got, err)
	}
	// "MZXW6" is the canonical encoding of "foo" (24 bits in 25, one pad bit).
	// "MZXW7" sets that pad bit: same three bytes, different string.
	if got, err := Decode("MZXW6"); err != nil || string(got) != "foo" {
		t.Fatalf("Decode(\"MZXW6\") = %q, %v; want \"foo\", nil", got, err)
	}
	if _, err := Decode("MZXW7"); err == nil {
		t.Error("Decode(\"MZXW7\") should be rejected: it re-encodes to MZXW6")
	}
}
