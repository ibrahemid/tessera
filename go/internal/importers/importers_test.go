package importers

import (
	"bytes"
	"testing"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/base32x"
)

const sampleSecret = "JBSWY3DPEHPK3PXP"

func wantSecret(t *testing.T) []byte {
	t.Helper()
	b, err := base32x.Decode(sampleSecret)
	if err != nil {
		t.Fatalf("decode sample secret: %v", err)
	}
	return b
}

const aegisPlain = `{
  "version": 1,
  "header": { "slots": null, "params": null },
  "db": {
    "version": 3,
    "entries": [
      { "type": "totp", "name": "john@example.com", "issuer": "GitHub",
        "info": { "secret": "JBSWY3DPEHPK3PXP", "algo": "SHA1", "digits": 6, "period": 30 } },
      { "type": "hotp", "name": "ops", "issuer": "Bank",
        "info": { "secret": "JBSWY3DPEHPK3PXP", "algo": "SHA256", "digits": 8, "counter": 5 } }
    ]
  }
}`

const aegisEncrypted = `{ "version": 1, "header": { "slots": [{}], "params": {} }, "db": "BASE64CIPHERTEXT==" }`

const twofasPlain = `{
  "schemaVersion": 4,
  "services": [
    { "name": "GitHub", "secret": "JBSWY3DPEHPK3PXP",
      "otp": { "account": "john@example.com", "issuer": "GitHub", "digits": 6, "period": 30, "algorithm": "SHA1", "tokenType": "TOTP" } }
  ]
}`

const twofasEncrypted = `{ "schemaVersion": 4, "services": [], "servicesEncrypted": "deadbeef:cafe:1" }`

const raivoPlain = `[
  { "issuer": "GitHub", "account": "john@example.com", "secret": "JBSWY3DPEHPK3PXP",
    "algorithm": "SHA1", "digits": "6", "kind": "TOTP", "timer": "30", "counter": "0" }
]`

func TestParseAegisPlain(t *testing.T) {
	accts, source, ok, err := Parse([]byte(aegisPlain))
	if !ok || err != nil {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if source != "Aegis" {
		t.Fatalf("source = %q", source)
	}
	if len(accts) != 2 {
		t.Fatalf("got %d accounts", len(accts))
	}
	a := accts[0]
	if a.Type != account.TOTP || a.Issuer != "GitHub" || a.Account != "john@example.com" {
		t.Fatalf("entry 0 wrong: %+v", a)
	}
	if !bytes.Equal(a.Secret, wantSecret(t)) {
		t.Fatalf("secret mismatch")
	}
	if accts[1].Type != account.HOTP || accts[1].Counter != 5 || accts[1].Digits != 8 || accts[1].Algorithm != "SHA256" {
		t.Fatalf("entry 1 wrong: %+v", accts[1])
	}
}

func TestParse2FASPlain(t *testing.T) {
	accts, source, ok, err := Parse([]byte(twofasPlain))
	if !ok || err != nil {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if source != "2FAS" || len(accts) != 1 {
		t.Fatalf("source=%q n=%d", source, len(accts))
	}
	a := accts[0]
	if a.Issuer != "GitHub" || a.Account != "john@example.com" || a.Period != 30 {
		t.Fatalf("wrong: %+v", a)
	}
	if !bytes.Equal(a.Secret, wantSecret(t)) {
		t.Fatalf("secret mismatch")
	}
}

func TestParseRaivoPlain(t *testing.T) {
	accts, source, ok, err := Parse([]byte(raivoPlain))
	if !ok || err != nil {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if source != "Raivo" || len(accts) != 1 {
		t.Fatalf("source=%q n=%d", source, len(accts))
	}
	if accts[0].Period != 30 || accts[0].Digits != 6 {
		t.Fatalf("string-number parse wrong: %+v", accts[0])
	}
}

func TestEncryptedDetected(t *testing.T) {
	for _, tc := range []struct {
		name, data, source string
	}{
		{"aegis", aegisEncrypted, "Aegis"},
		{"2fas", twofasEncrypted, "2FAS"},
	} {
		_, source, ok, err := Parse([]byte(tc.data))
		if !ok {
			t.Fatalf("%s: expected recognized export", tc.name)
		}
		if err == nil {
			t.Fatalf("%s: expected encryption error, got nil", tc.name)
		}
		if source != tc.source {
			t.Fatalf("%s: source = %q", tc.name, source)
		}
	}
}

func TestUnsupportedTypeRejected(t *testing.T) {
	data := `{
	  "db": { "entries": [
	    { "type": "yandex", "name": "x", "issuer": "Yandex",
	      "info": { "secret": "JBSWY3DPEHPK3PXP", "algo": "SHA1", "digits": 6, "period": 30 } }
	  ] }
	}`
	_, _, ok, err := Parse([]byte(data))
	if !ok || err == nil {
		t.Fatalf("expected recognized export with error, got ok=%v err=%v", ok, err)
	}
}

func TestUnsupportedAlgoRejected(t *testing.T) {
	data := `{
	  "db": { "entries": [
	    { "type": "totp", "name": "x", "issuer": "Old",
	      "info": { "secret": "JBSWY3DPEHPK3PXP", "algo": "MD5", "digits": 6, "period": 30 } }
	  ] }
	}`
	_, _, ok, err := Parse([]byte(data))
	if !ok || err == nil {
		t.Fatalf("expected recognized export with error, got ok=%v err=%v", ok, err)
	}
}

func TestSteamImportForcesFiveDigits(t *testing.T) {
	data := `{
	  "db": { "entries": [
	    { "type": "steam", "name": "gabe", "issuer": "Steam",
	      "info": { "secret": "JBSWY3DPEHPK3PXP", "algo": "SHA1", "digits": 5, "period": 30 } }
	  ] }
	}`
	accts, _, ok, err := Parse([]byte(data))
	if !ok || err != nil || len(accts) != 1 {
		t.Fatalf("ok=%v err=%v n=%d", ok, err, len(accts))
	}
	if accts[0].Type != account.Steam || accts[0].Digits != 5 {
		t.Fatalf("wrong: %+v", accts[0])
	}
}

func TestRaivoErrorsSurface(t *testing.T) {
	for name, data := range map[string]string{
		"missing secret": `[{ "issuer": "GitHub", "account": "x", "secret": "",
			"algorithm": "SHA1", "digits": "6", "kind": "TOTP", "timer": "30", "counter": "0" }]`,
		"bad digits": `[{ "issuer": "GitHub", "account": "x", "secret": "JBSWY3DPEHPK3PXP",
			"algorithm": "SHA1", "digits": "six", "kind": "TOTP", "timer": "30", "counter": "0" }]`,
	} {
		_, source, ok, err := Parse([]byte(data))
		if !ok || err == nil {
			t.Fatalf("%s: expected recognized Raivo export with error, got ok=%v err=%v", name, ok, err)
		}
		if source != "Raivo" {
			t.Fatalf("%s: source = %q", name, source)
		}
	}
}

func TestUnrecognizedFallsThrough(t *testing.T) {
	for _, data := range []string{
		"otpauth://totp/GitHub:me?secret=JBSWY3DPEHPK3PXP",
		"",
		"   ",
		`{"foo":"bar"}`,
	} {
		if _, _, ok, _ := Parse([]byte(data)); ok {
			t.Fatalf("expected ok=false for %q", data)
		}
	}
}
