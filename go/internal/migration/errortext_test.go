package migration

import (
	"bytes"
	"encoding/base64"
	"errors"
	"io"
	"net/url"
	"strings"
	"testing"
)

func TestUnexpectedHostErrorQuotesNoPartOfTheLink(t *testing.T) {
	const data = "Ci0KCkhlbGxvId6tvu8SDkV4YW1wbGU6YWxpY2Ug"
	const want = "migration: unexpected host; the link must start with otpauth-migration://offline?"
	for _, uri := range []string{
		"otpauth-migration://offlinedata=" + data,
		"otpauth-migration://offline&data=" + data,
		"otpauth-migration://online?data=" + data,
	} {
		_, err := Parse(uri)
		if err == nil {
			t.Fatalf("%s: parsed", uri)
		}
		if err.Error() != want {
			t.Errorf("%s: error = %s, want %s", uri, err, want)
		}
		if strings.Contains(err.Error(), data[:8]) {
			t.Errorf("the error quotes the payload: %s", err)
		}
	}
}

func TestSchemeErrorQuotesABoundedValue(t *testing.T) {
	_, err := Parse("otpauth-migrationx://offline?data=AA%3D%3D")
	want := `migration: not an otpauth-migration uri (scheme "otpauth-migratio"… (18 bytes))`
	if err == nil || err.Error() != want {
		t.Fatalf("error = %v, want %s", err, want)
	}
}

func payloadURI(payload []byte) string {
	return "otpauth-migration://offline?data=" + url.QueryEscape(base64.StdEncoding.EncodeToString(payload))
}

func TestProtobufFailuresHaveFixedTexts(t *testing.T) {
	overflow := append([]byte{0x10}, bytes.Repeat([]byte{0xff}, 9)...)
	overflow = append(overflow, 0x02)
	for _, c := range []struct {
		name    string
		payload []byte
		want    string
	}{
		{"truncated", []byte{0x0a, 0x05}, "migration: malformed otp_parameters: unexpected EOF"},
		{"field number", []byte{0x00}, "migration: malformed payload tag: proto: invalid field number"},
		{"overflow", overflow, "migration: malformed payload field 2: proto: variable length integer overflow"},
		{"reserved", []byte{0x36}, "migration: malformed unknown field 6: proto: cannot parse reserved wire type"},
		{"end group", []byte{0x34}, "migration: malformed unknown field 6: proto: mismatching end group marker"},
		{"nesting", bytes.Repeat([]byte{0x33}, 20000), "migration: malformed unknown field 6: proto: parse error"},
	} {
		_, err := Parse(payloadURI(c.payload))
		if err == nil {
			t.Fatalf("%s: parsed", c.name)
		}
		if err.Error() != c.want {
			t.Errorf("%s: error = %q, want %q", c.name, err, c.want)
		}
	}
	_, err := Parse("otpauth-migration://offline?data=AA%3D%3D")
	if err == nil || err.Error() != "migration: malformed payload tag: proto: invalid field number" {
		t.Errorf("error = %v", err)
	}
	if _, err := Parse(payloadURI([]byte{0x0a, 0x05})); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("a truncated payload does not match io.ErrUnexpectedEOF: %v", err)
	}
}

func TestEveryWireFailureMapsToAFixedText(t *testing.T) {
	fixed := map[string]bool{wireParseText: true}
	causes := map[error]bool{}
	for _, w := range wireErrors {
		if w.cause == nil || causes[w.cause] {
			t.Fatalf("wire cause for %q is nil or shared", w.text)
		}
		causes[w.cause] = true
		fixed[w.text] = true
	}
	for n := -1; n >= -16; n-- {
		if text := wireFailure(n).Error(); !fixed[text] {
			t.Errorf("code %d maps to %q", n, text)
		}
	}
}
