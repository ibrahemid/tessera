package detect

import (
	"strings"
	"testing"
)

// TestClassifyTable ports every row of the spec "Detection test table"
// (spec/otpauth.md). Each input is classified independently; multiline inputs
// are split and each line classified by the same precedence.
func TestClassifyTable(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want Kind
	}{
		{"setup-key upper", "ZB573K4APD63E6RLD3WAHI3QFZ35RLEP", SetupKey},
		{"setup-key spaces", "zb573k4a pd63e6rl d3wahi3q fz35rlep", SetupKey},
		{"setup-key dashes", "zb573k4a-pd63e6rl-d3wahi3q-fz35rlep", SetupKey},
		{"too short", "GEZDGNBV", Invalid},
		{"two tokens", "hello world", Invalid},
		{"otpauth totp", "otpauth://totp/Example:alice@google.com?secret=JBSWY3DPEHPK3PXP&issuer=Example", OTPAuth},
		{"otpauth hotp", "otpauth://hotp/Bank:ops?secret=JBSWY3DPEHPK3PXP&counter=5&digits=8", OTPAuth},
		{"otpauth steam", "otpauth://steam/Steam:me?secret=ONSWG4TFOQ&digits=5", OTPAuth},
		{"migration", "otpauth-migration://offline?data=CjEKCkhlbGxvId6tvu8SGEV4YW1wbGU6YWxpY2VAZ29vZ2xlLmNvbRoHRXhhbXBsZSABKAEwAhABGAEgACjr4JKkBg%3D%3D", Migration},
		{"raivo json", `[{ "issuer": "GitHub", "account": "john@example.com", "secret": "JBSWY3DPEHPK3PXP", "algorithm": "SHA1", "digits": "6", "kind": "TOTP", "timer": "30", "counter": "0" }]`, ExportJSON},
		{"2fas json", `{ "schemaVersion": 4, "services": [{ "name": "GitHub", "secret": "JBSWY3DPEHPK3PXP", "otp": { "tokenType": "TOTP" } }] }`, ExportJSON},
		{"aegis json", `{ "version": 1, "db": { "version": 3, "entries": [] } }`, ExportJSON},
		{"empty", "", Invalid},
		{"whitespace only", "   \t  ", Invalid},
		{"base64 not base32", "SGVsbG8gd29ybGQhISE=", Invalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Classify(tc.in); got != tc.want {
				t.Fatalf("Classify(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// TestClassifyMultilinePerLine ports the multiline row: line 1 otpauth, line 2
// invalid, line 3 setup-key.
func TestClassifyMultilinePerLine(t *testing.T) {
	lines := []string{
		"otpauth://totp/A?secret=JBSWY3DPEHPK3PXP",
		"hello world",
		"ZB573K4APD63E6RLD3WAHI3QFZ35RLEP",
	}
	want := []Kind{OTPAuth, Invalid, SetupKey}
	for i, line := range lines {
		if got := Classify(line); got != want[i] {
			t.Fatalf("line %d Classify(%q) = %v, want %v", i+1, line, got, want[i])
		}
	}
}

func TestParseTextMixedValidInvalid(t *testing.T) {
	input := strings.Join([]string{
		"otpauth://totp/A?secret=JBSWY3DPEHPK3PXP",
		"hello world",
		"ZB573K4APD63E6RLD3WAHI3QFZ35RLEP",
	}, "\n")

	accts, errs := ParseText(input)
	if len(accts) != 2 {
		t.Fatalf("expected 2 accounts, got %d", len(accts))
	}
	if accts[0].Account != "A" {
		t.Errorf("account 0: got account %q, want A", accts[0].Account)
	}
	// Setup key: TOTP with SHA1/6/30 defaults and empty issuer/account.
	sk := accts[1]
	if sk.Type != "totp" || sk.Algorithm != "SHA1" || sk.Digits != 6 || sk.Period != 30 {
		t.Errorf("setup-key account defaults wrong: %+v", sk)
	}
	if sk.Issuer != "" || sk.Account != "" {
		t.Errorf("setup-key issuer/account should be empty, got %q/%q", sk.Issuer, sk.Account)
	}
	if len(errs) != 1 {
		t.Fatalf("expected 1 item error, got %d: %v", len(errs), errs)
	}
	if errs[0].Line != 2 {
		t.Errorf("item error line = %d, want 2", errs[0].Line)
	}
	if strings.Contains(errs[0].Input, "world") {
		t.Errorf("item error must not echo the raw input: %q", errs[0].Input)
	}
}

func TestParseTextWholeJSONExport(t *testing.T) {
	pretty := `{
  "version": 1,
  "db": {
    "version": 3,
    "entries": [
      { "type": "totp", "name": "me", "issuer": "GitHub",
        "info": { "secret": "JBSWY3DPEHPK3PXP", "algo": "SHA1", "digits": 6, "period": 30 } }
    ]
  }
}`
	accts, errs := ParseText(pretty)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(accts) != 1 || accts[0].Issuer != "GitHub" {
		t.Fatalf("expected 1 GitHub account, got %+v", accts)
	}
}

func TestParseTextEmpty(t *testing.T) {
	accts, errs := ParseText("   \n\t ")
	if len(accts) != 0 {
		t.Fatalf("expected no accounts, got %d", len(accts))
	}
	if len(errs) != 1 {
		t.Fatalf("expected 1 invalid item, got %d", len(errs))
	}
}

func TestIsLikelyBase32Secret(t *testing.T) {
	if !IsLikelyBase32Secret("ZB573K4APD63E6RLD3WAHI3QFZ35RLEP") {
		t.Error("valid 32-char key should qualify")
	}
	if IsLikelyBase32Secret("GEZDGNBV") {
		t.Error("8-char key is below the 16 minimum")
	}
	if IsLikelyBase32Secret("hello world") {
		t.Error("prose should not qualify")
	}
	if IsLikelyBase32Secret("SGVsbG8gd29ybGQhISE=") {
		t.Error("base64 (has '=' and chars outside A-Z2-7) should not qualify")
	}
}
