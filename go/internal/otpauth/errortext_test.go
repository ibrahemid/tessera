package otpauth

import (
	"strconv"
	"strings"
	"testing"
)

func TestQuoteValue(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"", `""`},
		{"sha3", `"sha3"`},
		{"0123456789abcdef", `"0123456789abcdef"`},
		{"0123456789abcdefg", `"0123456789abcdef"… (17 bytes)`},
		{strings.Repeat("a", 15) + "€x", `"aaaaaaaaaaaaaaa"… (19 bytes)`},
		{strings.Repeat("a", 13) + "€x", `"aaaaaaaaaaaaa€"… (17 bytes)`},
		{"tot\xa9", `"tot\xa9"`},
		{strings.Repeat("\xff", 20), `"` + strings.Repeat(`\xff`, 16) + `"… (20 bytes)`},
		{"a\"b\\c\n", strconv.Quote("a\"b\\c\n")},
		{"\xef\xbf\xbd\xed\xa0\x80", "\"\ufffd\\xed\\xa0\\x80\""},
	} {
		if got := QuoteValue(c.in); got != c.want {
			t.Errorf("QuoteValue(%q) = %s, want %s", c.in, got, c.want)
		}
	}
}

func TestParseErrorQuotesNoLongValueAndNoSecret(t *testing.T) {
	const second = "KRSXG5CTMVRXEZLU"
	for _, c := range []struct{ uri, want string }{
		{
			"otpauth://hotp/A:b?secret=JBSWY3DPEHPK3PXP&counter=0otpauth://hotp/A:c?secret=" + second,
			`otpauth: invalid counter "0otpauth://hotp/"… (43 bytes)`,
		},
		{
			"otpauth://totp/A:b?secret=JBSWY3DPEHPK3PXP&issuer=Aotpauth://totp/A:c?secret=" + second,
			`otpauth: issuer mismatch (label "A", param "Aotpauth://totp/"… (43 bytes))`,
		},
		{
			"otpauth://totp/A:b?secret=JBSWY3DPEHPK3PXP&digits=6otpauth://totp/A:c?secret=" + second,
			`otpauth: invalid digits "6otpauth://totp/"… (43 bytes)`,
		},
		{
			"otpauth://totp/A:b?secret=JBSWY3DPEHPK3PXP&period=30otpauth://totp/A:c?secret=" + second,
			`otpauth: invalid period "30otpauth://totp"… (44 bytes)`,
		},
		{
			"otpauth://totp/A:b?secret=JBSWY3DPEHPK3PXP&algorithm=SHA1otpauth://totp/A:c?secret=" + second,
			`otpauth: unsupported algorithm "SHA1otpauth://to"… (46 bytes)`,
		},
		{
			"otpauth://hotp/Bank:ops?secret=JBSWY3DPEHPK3PXP&counter=9223372036854775807",
			`otpauth: invalid counter "9223372036854775"… (19 bytes)`,
		},
	} {
		_, err := Parse(c.uri)
		if err == nil {
			t.Fatalf("%s: parsed", c.uri)
		}
		if err.Error() != c.want {
			t.Errorf("error = %s, want %s", err, c.want)
		}
		if strings.Contains(err.Error(), second) {
			t.Errorf("the error quotes the second secret: %s", err)
		}
	}
}

func TestParseErrorQuotesInvalidBytesAsGoDoes(t *testing.T) {
	for _, c := range []struct{ uri, want string }{
		{"otpauth://tot%A9/?secret=JBSWY3DPEHPK3PXP", `otpauth: unsupported type "tot\xa9"`},
		{"otpauth://totp/A:b?secret=JBSWY3DPEHPK3PXP&algorithm=SH%FFA", `otpauth: unsupported algorithm "SH\xffA"`},
		{"otpauth://totp/A:b?secret=JBSWY3DPEHPK3PXP&digits=%A96", `otpauth: invalid digits "\xa96"`},
		{"otpauth://totp/A:b?secret=JBSWY3DPEHPK3PXP&period=%FF", `otpauth: invalid period "\xff"`},
		{"otpauth://hotp/A:b?secret=JBSWY3DPEHPK3PXP&counter=x7", `otpauth: invalid counter "x7"`},
	} {
		if _, err := Parse(c.uri); err == nil || err.Error() != c.want {
			t.Errorf("error = %v, want %s", err, c.want)
		}
	}
	a, err := Parse("otpauth://totp/A:b?secret=JBSWY3DPEHPK3PXP&issuer=A&%FF=1&period=60")
	if err != nil || a.Period != 60 {
		t.Errorf("period = %d, err = %v", a.Period, err)
	}
}
