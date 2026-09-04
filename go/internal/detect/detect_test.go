package detect

import (
	"archive/zip"
	"bytes"
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
		{"setup-key padded", "\tZB573K4APD63E6RLD3WAHI3QFZ35RLEP\n", SetupKey},
		{"too short", "GEZDGNBV", Invalid},
		{"two tokens", "hello world", Invalid},
		{"otpauth totp", "otpauth://totp/Example:alice@google.com?secret=JBSWY3DPEHPK3PXP&issuer=Example", OTPAuth},
		{"otpauth hotp", "otpauth://hotp/Bank:ops?secret=JBSWY3DPEHPK3PXP&counter=5&digits=8", OTPAuth},
		{"otpauth steam", "otpauth://steam/Steam:me?secret=ONSWG4TFOQ&digits=5", OTPAuth},
		{"migration", "otpauth-migration://offline?data=CjEKCkhlbGxvId6tvu8SGEV4YW1wbGU6YWxpY2VAZ29vZ2xlLmNvbRoHRXhhbXBsZSABKAEwAhABGAEgACjr4JKkBg%3D%3D", Migration},
		{"raivo json", `[{ "issuer": "GitHub", "account": "john@example.com", "secret": "JBSWY3DPEHPK3PXP", "algorithm": "SHA1", "digits": "6", "kind": "TOTP", "timer": "30", "counter": "0" }]`, ExportJSON},
		{"2fas json", `{ "schemaVersion": 4, "services": [{ "name": "GitHub", "secret": "JBSWY3DPEHPK3PXP", "otp": { "tokenType": "TOTP" } }] }`, ExportJSON},
		{"aegis json", `{ "version": 1, "db": { "version": 3, "entries": [] } }`, ExportJSON},
		{"andotp json", `[{ "secret": "JBSWY3DPEHPK3PXP", "issuer": "GitHub", "label": "john@example.com", "digits": 6, "type": "TOTP", "algorithm": "SHA1", "thumbnail": "Default", "last_used": 0, "used_frequency": 0, "period": 30, "tags": [] }]`, ExportJSON},
		{"freeotp+ json", `{ "tokens": [], "tokenOrder": [] }`, ExportJSON},
		{"stratum json", `{ "Authenticators": [], "Categories": [], "AuthenticatorCategories": [], "CustomIcons": [] }`, ExportJSON},
		{"bitwarden authenticator json", `{ "encrypted": false, "items": [] }`, ExportJSON},
		{"bitwarden password manager json", `{ "encrypted": false, "folders": [], "items": [] }`, ExportJSON},
		{"bitwarden encrypted json", `{ "encrypted": true, "passwordProtected": true, "salt": "QUFB", "data": "QkJC" }`, ExportJSON},
		{"proton json", `{ "version": 1, "entries": [] }`, ExportJSON},
		{"proton encrypted json", `{ "version": 1, "salt": "QUFB", "content": "QkJC" }`, ExportJSON},
		{"ente encrypted json", `{ "version": 1, "kdfParams": { "memLimit": 67108864, "opsLimit": 2, "salt": "QUFB" }, "encryptedData": "QkJC", "encryptionNonce": "Q0ND" }`, ExportJSON},
		{"1pux export.data json", `{ "accounts": [ { "vaults": [] } ] }`, ExportJSON},
		{"apple passwords csv", appleCSV, ExportCSV},
		{"1password csv header only", onePasswordCSVHeader, ExportCSV},
		{"csv without an otp column", "Title,URL,Username,Password,Notes", Invalid},
		{"zip archive", "PK\x03\x04\x14\x00\x00\x00", ExportBinary},
		{"stratum encrypted backup", "AUTHENTICATORPRO\x00\x01\x02", ExportBinary},
		{"stratum legacy encrypted backup", "AuthenticatorPro\x00\x01\x02", ExportBinary},
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

// TestParseTextTrimsEachLine pins the spec row "leading/trailing whitespace is
// trimmed before every rule" on the per-line path: an indented setup key
// imports exactly like a bare one.
func TestParseTextTrimsEachLine(t *testing.T) {
	input := "otpauth://totp/A?secret=JBSWY3DPEHPK3PXP\n\tZB573K4APD63E6RLD3WAHI3QFZ35RLEP  \n"
	accts, errs := ParseText(input)
	if len(errs) != 0 {
		t.Fatalf("expected no item errors, got %v", errs)
	}
	if len(accts) != 2 {
		t.Fatalf("expected 2 accounts, got %d", len(accts))
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

func TestParseTextWrappedURIRepair(t *testing.T) {
	// A URI hard-wrapped by a textarea or mail client is one URI, not a batch
	// (spec § input detection, wrapped-URI repair).
	wrapped := "otpauth://totp/Demo:reviewer@example.com?\nsecret=JBSWY3DPEHPK3PXP&issuer=Demo"
	accts, errs := ParseText(wrapped)
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}
	if len(accts) != 1 {
		t.Fatalf("expected 1 account, got %d", len(accts))
	}
	if accts[0].Issuer != "Demo" || accts[0].Account != "reviewer@example.com" {
		t.Errorf("wrong account parsed: %+v", accts[0])
	}

	// Repair only applies when the join actually parses; otherwise per-line
	// semantics are unchanged.
	broken := "otpauth://totp/A?\nhello world"
	accts, errs = ParseText(broken)
	if len(accts) != 0 || len(errs) != 2 {
		t.Fatalf("expected fallback to per-line (0 accounts, 2 errors), got %d/%v", len(accts), errs)
	}

	// Two URIs stay a batch: no repair when the scheme appears more than once.
	batch := "otpauth://totp/A?secret=JBSWY3DPEHPK3PXP\notpauth://totp/B?secret=JBSWY3DPEHPK3PXP"
	accts, errs = ParseText(batch)
	if len(accts) != 2 || len(errs) != 0 {
		t.Fatalf("expected 2 accounts, 0 errors, got %d/%v", len(accts), errs)
	}

	// A complete first line means batch semantics even if the join would
	// parse: URI + bare setup key must stay two accounts.
	uriPlusKey := "otpauth://totp/A?secret=JBSWY3DPEHPK3PXP\nZB573K4APD63E6RLD3WAHI3QFZ35RLEP"
	accts, errs = ParseText(uriPlusKey)
	if len(accts) != 2 || len(errs) != 0 {
		t.Fatalf("expected 2 accounts (uri + setup key), 0 errors, got %d/%v", len(accts), errs)
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

// appleCSV is the §2e Apple Passwords row: the registered header plus one item
// carrying an otpauth URI.
const appleCSV = "Title,URL,Username,Password,Notes,OTPAuth\r\n" +
	"Example,https://e.com,alice@example.com,hunter2,,otpauth://totp/Example:alice@example.com?secret=JBSWY3DPEHPK3PXP\r\n"

// onePasswordCSVHeader is the registered 1Password header with no rows.
const onePasswordCSVHeader = "Title,Url,Username,Password,OTPAuth,Favorite,Archived,Tags,Notes\n"

func TestParseTextAppleCSV(t *testing.T) {
	accts, errs := ParseText(appleCSV)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(accts) != 1 {
		t.Fatalf("expected 1 account, got %d", len(accts))
	}
	if accts[0].Issuer != "Example" || accts[0].Account != "alice@example.com" {
		t.Fatalf("wrong account: %+v", accts[0])
	}
}

// A recognized export holding no OTP entries must name the app instead of
// looking like an empty input (spec/otpauth.md § shared rules).
func TestParseTextRecognizedExportWithoutOTPEntries(t *testing.T) {
	_, errs := ParseText(onePasswordCSVHeader)
	if len(errs) != 1 {
		t.Fatalf("expected 1 item error, got %d: %v", len(errs), errs)
	}
	msg := errs[0].Err.Error()
	if !strings.Contains(msg, "1Password") || !strings.Contains(msg, "no one-time-password entries") {
		t.Fatalf("error = %q", msg)
	}
}

func TestParseTextStratumEncryptedBackup(t *testing.T) {
	backup := "AUTHENTICATORPRO" + strings.Repeat("\x00", 32)
	_, errs := ParseText(backup)
	if len(errs) != 1 {
		t.Fatalf("expected 1 item error, got %d: %v", len(errs), errs)
	}
	if !strings.Contains(errs[0].Err.Error(), "Stratum") {
		t.Fatalf("error = %q", errs[0].Err.Error())
	}
}

func TestParseTextOnePUXArchive(t *testing.T) {
	const exportData = `{ "accounts": [ { "vaults": [ { "items": [ { "details": { "loginFields": [], "sections": [ { "fields": [ { "value": { "totp": "otpauth://totp/Example:alice@example.com?secret=JBSWY3DPEHPK3PXP&issuer=Example" } } ] } ] }, "overview": { "title": "Example" } } ] } ] } ] }`
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("export.data")
	if err != nil {
		t.Fatalf("create zip entry: %v", err)
	}
	if _, err := w.Write([]byte(exportData)); err != nil {
		t.Fatalf("write zip entry: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	accts, errs := ParseText(buf.String())
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(accts) != 1 || accts[0].Issuer != "Example" {
		t.Fatalf("expected 1 Example account, got %+v", accts)
	}
}

// The Ente Auth plain-text export is an ordinary per-line otpauth batch; its
// optional codeDisplay parameter is an unknown query param and is ignored.
func TestParseTextEntePlainExport(t *testing.T) {
	input := strings.Join([]string{
		"otpauth://totp/Example:alice@example.com?algorithm=SHA1&digits=6&issuer=Example&period=30&secret=JBSWY3DPEHPK3PXP",
		"otpauth://steam/Steam:gabe?algorithm=SHA1&digits=5&issuer=Steam&period=30&secret=JBSWY3DPEHPK3PXP",
		"otpauth://hotp/Bank:ops?algorithm=SHA256&counter=5&digits=8&issuer=Bank&secret=JBSWY3DPEHPK3PXP&codeDisplay=%7B%22pinned%22%3Afalse%7D",
	}, "\n")
	for i, line := range strings.Split(input, "\n") {
		if got := Classify(line); got != OTPAuth {
			t.Fatalf("line %d: Classify = %v, want otpauth", i+1, got)
		}
	}
	accts, errs := ParseText(input)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(accts) != 3 {
		t.Fatalf("expected 3 accounts, got %d", len(accts))
	}
	if accts[2].Counter != 5 || accts[2].Digits != 8 {
		t.Fatalf("codeDisplay changed the hotp account: %+v", accts[2])
	}
}
