package importers

import (
	"strings"
	"testing"
)

func TestQuoteBounded(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", `""`},
		{"abcdefghijklmnop", `"abcdefghijklmnop"`},
		{"abcdefghijklmnopq", `"abcdefghijklmnop"… (17 bytes)`},
		{"aaaaaaaaaaaaaaa€", `"aaaaaaaaaaaaaaa"… (18 bytes)`},
		{"aaaaaaaaaaaaa😀b", `"aaaaaaaaaaaaa"… (18 bytes)`},
		{strings.Repeat("\x1b", 20), `"` + strings.Repeat(`\x1b`, 16) + `"… (20 bytes)`},
	}
	for _, c := range cases {
		if got := quoteBounded(c.in); got != c.want {
			t.Errorf("quoteBounded(%q) = %s, want %s", c.in, got, c.want)
		}
	}
}

const quoteLong = "XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX"

func TestImporterErrorsBoundQuotedValues(t *testing.T) {
	cases := []struct{ input, want string }{
		{
			`[{"secret":"JBSWY3DPEHPK3PXP","issuer":"i","label":"a","type":"` + quoteLong + `"}]`,
			`andotp entry "i": unsupported account type "XXXXXXXXXXXXXXXX"… (32 bytes)`,
		},
		{
			`[{"secret":"JBSWY3DPEHPK3PXP","issuer":"` + quoteLong + `","label":"a","type":"TOTP","algorithm":"` + quoteLong + `"}]`,
			`andotp entry "XXXXXXXXXXXXXXXX"… (32 bytes): unsupported algorithm "XXXXXXXXXXXXXXXX"… (32 bytes)`,
		},
		{
			`[{"issuer":"i","secret":"JBSWY3DPEHPK3PXP","kind":"TOTP","digits":"` + quoteLong + `"}]`,
			`raivo entry "i": digits: not a number: "XXXXXXXXXXXXXXXX"… (32 bytes)`,
		},
	}
	for _, c := range cases {
		_, _, ok, err := Parse([]byte(c.input))
		if !ok || err == nil || err.Error() != c.want {
			t.Errorf("%s: ok=%v err=%v\nwant %s", c.input, ok, err, c.want)
		}
	}
}

func TestImporterErrorsNeverQuoteTheSecret(t *testing.T) {
	const secret = "JBSWY3DPEHPK3PX1ZZZZ"
	inputs := []string{
		`[{"secret":"` + secret + `","issuer":"i","label":"a","type":"TOTP"}]`,
		`[{"issuer":"i","secret":"` + secret + `","kind":"TOTP"}]`,
		`{"version":1,"db":{"version":3,"entries":[{"type":"totp","name":"a","info":{"secret":"` + secret + `"}}]}}`,
		`{"services":[{"name":"A","secret":"` + secret + `","otp":{"tokenType":"TOTP"}}]}`,
		`{"items":[{"name":"A","login":{"totp":"` + secret + `"}}]}`,
		"Title,Url,Username,Password,OTPAuth,Favorite,Archived,Tags,Notes\nA,,,," + secret + ",,,,\n",
	}
	for _, in := range inputs {
		_, _, ok, err := Parse([]byte(in))
		if !ok || err == nil {
			t.Fatalf("%s: ok=%v err=%v, want a rejection", in, ok, err)
		}
		if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "JBSWY") {
			t.Errorf("error quotes the secret: %v", err)
		}
	}
}
