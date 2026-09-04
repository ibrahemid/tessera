package importers

import (
	"bytes"
	"testing"

	"github.com/ibrahemid/tessera/go/internal/account"
)

func TestParseOTPValueEmptySkips(t *testing.T) {
	for _, v := range []string{"", "   ", "\t\n"} {
		a, ok, err := parseOTPValue(v, "Example", "alice@example.com")
		if err != nil {
			t.Fatalf("%q: unexpected error: %v", v, err)
		}
		if ok {
			t.Fatalf("%q: expected a silent skip, got %+v", v, a)
		}
	}
}

func TestParseOTPValueOTPAuthURI(t *testing.T) {
	a, ok, err := parseOTPValue(
		"otpauth://hotp/Bank:ops?secret="+sampleSecret+"&issuer=Bank&counter=5&digits=8&algorithm=SHA256",
		"ignored title", "ignored user")
	if !ok || err != nil {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if a.Type != account.HOTP || a.Issuer != "Bank" || a.Account != "ops" {
		t.Fatalf("wrong account: %+v", a)
	}
	if a.Digits != 8 || a.Algorithm != "SHA256" || a.Counter != 5 {
		t.Fatalf("wrong params: %+v", a)
	}
	if !bytes.Equal(a.Secret, wantSecret(t)) {
		t.Fatalf("secret mismatch")
	}
}

func TestParseOTPValueSteamURI(t *testing.T) {
	// The account name falls back to the username when the source has one.
	a, ok, err := parseOTPValue("steam://"+sampleSecret, "gabe", "gaben")
	if !ok || err != nil {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if a.Type != account.Steam || a.Issuer != "Steam" || a.Account != "gaben" {
		t.Fatalf("wrong account: %+v", a)
	}
	if a.Digits != 5 || a.Period != 30 || a.Algorithm != "SHA1" {
		t.Fatalf("wrong params: %+v", a)
	}
	if !bytes.Equal(a.Secret, wantSecret(t)) {
		t.Fatalf("secret mismatch")
	}

	// With no username the item name carries the label, so it must not be lost.
	a, ok, err = parseOTPValue("steam://"+sampleSecret+"/", "gabe", "")
	if !ok || err != nil {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if a.Account != "gabe" {
		t.Fatalf("account fallback = %q, want gabe", a.Account)
	}
}

func TestParseOTPValueSteamBadSecret(t *testing.T) {
	_, ok, err := parseOTPValue("steam://STEAMKEY!!", "gabe", "")
	if ok || err == nil {
		t.Fatalf("expected an error, got ok=%v err=%v", ok, err)
	}
	if got := err.Error(); got != "unsupported Steam secret encoding" {
		t.Fatalf("error = %q", got)
	}
}

func TestParseOTPValueBareSeed(t *testing.T) {
	a, ok, err := parseOTPValue(sampleSecret, "Seed Only", "carol@example.com")
	if !ok || err != nil {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if a.Type != account.TOTP || a.Issuer != "Seed Only" || a.Account != "carol@example.com" {
		t.Fatalf("wrong account: %+v", a)
	}
	if a.Digits != 6 || a.Period != 30 || a.Algorithm != "SHA1" {
		t.Fatalf("wrong params: %+v", a)
	}
	if !bytes.Equal(a.Secret, wantSecret(t)) {
		t.Fatalf("secret mismatch")
	}
}

func TestParseOTPValueBadSeed(t *testing.T) {
	_, ok, err := parseOTPValue("not base32!", "Example", "alice")
	if ok || err == nil {
		t.Fatalf("expected an error, got ok=%v err=%v", ok, err)
	}
}

func TestBuildAccountRawKeepsSecretBytes(t *testing.T) {
	raw := wantSecret(t)
	a, err := buildAccountRaw("TOTP", " GitHub ", " john@example.com ", raw, "sha256", 0, 0, 0)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if a.Issuer != "GitHub" || a.Account != "john@example.com" {
		t.Fatalf("labels not trimmed: %+v", a)
	}
	if a.Digits != 6 || a.Period != 30 || a.Algorithm != "SHA256" {
		t.Fatalf("defaults wrong: %+v", a)
	}
	if !bytes.Equal(a.Secret, raw) {
		t.Fatalf("secret mismatch")
	}

	// HOTP keeps a zero period; Steam is forced to five digits.
	a, err = buildAccountRaw("HOTP", "Bank", "ops", raw, "SHA1", 8, 0, 5)
	if err != nil {
		t.Fatalf("build hotp: %v", err)
	}
	if a.Period != 0 || a.Counter != 5 {
		t.Fatalf("hotp wrong: %+v", a)
	}
	a, err = buildAccountRaw("steam", "Steam", "gabe", raw, "SHA1", 8, 0, 0)
	if err != nil {
		t.Fatalf("build steam: %v", err)
	}
	if a.Digits != 5 || a.Period != 30 {
		t.Fatalf("steam wrong: %+v", a)
	}
}
