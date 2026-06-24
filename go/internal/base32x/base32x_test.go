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

func TestDecodeRoundTripNoPad(t *testing.T) {
	for _, s := range []string{"", "f", "fo", "foo", "foob", "fooba", "foobar"} {
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
