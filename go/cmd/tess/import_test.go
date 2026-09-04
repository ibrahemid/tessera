package main

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/base32x"
	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/qrcode"
)

func TestCollectImportFileOtpauth(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "accounts.txt")
	content := "otpauth://totp/GitHub:me?secret=JBSWY3DPEHPK3PXP&issuer=GitHub\n\n" +
		"otpauth://totp/AWS:root?secret=GEZDGNBVGY3TQOJQ&issuer=AWS\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	b := collectImport(nil, []string{path}, nil, nil, nil)
	if len(b.accounts) != 2 {
		t.Fatalf("expected 2 accounts, got %d (%v)", len(b.accounts), b.problems)
	}
	if len(b.problems) != 0 {
		t.Fatalf("unexpected problems: %v", b.problems)
	}
	if b.accounts[0].Issuer != "GitHub" || b.accounts[1].Issuer != "AWS" {
		t.Errorf("unexpected issuers: %s, %s", b.accounts[0].Issuer, b.accounts[1].Issuer)
	}
}

func TestCollectImportGarbageFileZeroImported(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.txt")
	if err := os.WriteFile(path, []byte("not a uri\nalso junk\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	b := collectImport(nil, []string{path}, nil, nil, nil)
	if len(b.accounts) != 0 {
		t.Fatalf("expected 0 accounts, got %d", len(b.accounts))
	}
	if len(b.problems) != 2 {
		t.Fatalf("expected 2 per-item problems, got %d", len(b.problems))
	}
	// Exit is non-zero only when zero imported and there were failures.
	s := &session{}
	added, _, _ := mergeAccounts(s, b.accounts)
	if !(added == 0 && len(b.problems) > 0) {
		t.Fatalf("garbage file should map to non-zero exit (added=%d)", added)
	}
}

func TestCollectImportBatchImageResilient(t *testing.T) {
	dir := t.TempDir()

	// One unreadable "image".
	bad := filepath.Join(dir, "broken.png")
	if err := os.WriteFile(bad, []byte("not a png"), 0o600); err != nil {
		t.Fatal(err)
	}
	// One image holding two QR codes.
	two := filepath.Join(dir, "two.png")
	writeTwoQRPNG(t, two,
		"otpauth://totp/A:one?secret=JBSWY3DPEHPK3PXP&issuer=A",
		"otpauth://totp/B:two?secret=GEZDGNBVGY3TQOJQ&issuer=B")

	b := collectImport([]string{bad, two}, nil, nil, nil, nil)
	if len(b.accounts) != 2 {
		t.Fatalf("expected 2 imported accounts, got %d (%v)", len(b.accounts), b.problems)
	}
	if len(b.problems) != 1 {
		t.Fatalf("expected 1 per-item problem (the unreadable image), got %d: %v", len(b.problems), b.problems)
	}
	s := &session{}
	added, skipped, _ := mergeAccounts(s, b.accounts)
	if added != 2 || skipped != 0 {
		t.Fatalf("added=%d skipped=%d, want 2/0", added, skipped)
	}
	if added == 0 && len(b.problems) > 0 {
		t.Fatal("batch with imported accounts must exit success")
	}
}

func TestDedupeKeyStableAndDistinct(t *testing.T) {
	a := account.Account{Type: account.TOTP, Issuer: "GitHub", Account: "me", Secret: []byte("12345678901234567890")}
	b := a
	if dedupeKey(a) != dedupeKey(b) {
		t.Error("identical accounts should share a dedupe key")
	}
	b.Account = "other"
	if dedupeKey(a) == dedupeKey(b) {
		t.Error("different account labels should differ")
	}
}

func TestMergeAccountsSkipsDuplicates(t *testing.T) {
	existing := account.Account{ID: "x", Type: account.TOTP, Issuer: "GitHub", Account: "me",
		Secret: []byte("12345678901234567890"), Algorithm: "SHA1", Digits: 6, Period: 30}
	s := &session{accounts: []account.Account{existing}}
	dup := existing
	dup.ID = ""
	fresh := account.Account{Type: account.TOTP, Issuer: "AWS", Account: "root",
		Secret: []byte("abcdefghij"), Algorithm: "SHA1", Digits: 6, Period: 30}
	added, skipped, _ := mergeAccounts(s, []account.Account{dup, fresh})
	if added != 1 || skipped != 1 {
		t.Errorf("added=%d skipped=%d, want 1/1", added, skipped)
	}
	if len(s.accounts) != 2 {
		t.Errorf("expected 2 accounts after merge, got %d", len(s.accounts))
	}
}

// TestMergeAccountsReportsValidationFailures pins that an account which parsed
// but cannot be stored is reported with its reason, not counted as a duplicate
// the user already has.
func TestMergeAccountsReportsValidationFailures(t *testing.T) {
	s := &session{}
	bad := account.Account{Type: account.TOTP, Issuer: "Broken", Account: "who",
		Secret: []byte("12345678901234567890"), Algorithm: "MD5", Digits: 6, Period: 30}
	good := account.Account{Type: account.TOTP, Issuer: "AWS", Account: "root",
		Secret: []byte("abcdefghij"), Algorithm: "SHA1", Digits: 6, Period: 30}

	added, skipped, failed := mergeAccounts(s, []account.Account{bad, good})
	if added != 1 {
		t.Errorf("added=%d, want 1", added)
	}
	if skipped != 0 {
		t.Errorf("skipped=%d: a validation failure is not a duplicate", skipped)
	}
	if len(failed) != 1 {
		t.Fatalf("failed=%d, want 1: %v", len(failed), failed)
	}
	if !strings.Contains(failed[0].source, "Broken") {
		t.Errorf("problem should name the account, got %q", failed[0].source)
	}
	if !strings.Contains(failed[0].reason, "algorithm") {
		t.Errorf("problem should give the real reason, got %q", failed[0].reason)
	}
	if strings.Contains(failed[0].reason, "duplicate") {
		t.Errorf("a validation failure must not be reported as a duplicate: %q", failed[0].reason)
	}
}

// TestAddFromArgReportsPerItemFailures pins spec/otpauth.md § partial-failure
// semantics for `tess add <file>`: everything that parses is returned and every
// item that failed comes back as a problem instead of being dropped.
func TestAddFromArgReportsPerItemFailures(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mixed.txt")
	content := "otpauth://totp/GitHub:me?secret=JBSWY3DPEHPK3PXP&issuer=GitHub\n" +
		"not a uri\n" +
		"otpauth://totp/Broken?secret=!!!!\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	accts, problems, err := addFromArg(path, "", "", "SHA1", 6, 30)
	if err != nil {
		t.Fatalf("addFromArg: %v", err)
	}
	if len(accts) != 1 || accts[0].Issuer != "GitHub" {
		t.Fatalf("expected the one good account, got %+v", accts)
	}
	if len(problems) != 2 {
		t.Fatalf("expected 2 per-item problems, got %d: %v", len(problems), problems)
	}
	for _, p := range problems {
		if !strings.Contains(p.source, path) || !strings.Contains(p.source, "line") {
			t.Errorf("problem should name file and line, got %q", p.source)
		}
		if p.reason == "" {
			t.Error("problem should carry a reason")
		}
	}
}

// TestAddCommandListsFailuresAndStillAdds runs the whole command: a batch with
// one bad line imports the good one, lists the bad one, and exits zero.
func TestAddCommandListsFailuresAndStillAdds(t *testing.T) {
	vpath := withVault(t)
	sealVault(t, vpath, nil)

	path := filepath.Join(t.TempDir(), "mixed.txt")
	content := "otpauth://totp/GitHub:me?secret=JBSWY3DPEHPK3PXP&issuer=GitHub\nnot a uri\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	c := newAddCmd()
	c.SetOut(&buf)
	c.SetArgs([]string{path})
	if err := c.Execute(); err != nil {
		t.Fatalf("add: %v", err)
	}
	got := buf.String()
	if !strings.Contains(got, "Added GitHub") {
		t.Errorf("expected the good account to be added:\n%s", got)
	}
	if !strings.Contains(got, "Could not import 1 item(s)") || !strings.Contains(got, "line 2") {
		t.Errorf("expected the failed line to be listed:\n%s", got)
	}

	s, err := openSession()
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	if len(s.accounts) != 1 {
		t.Fatalf("expected 1 stored account, got %d", len(s.accounts))
	}
}

// TestDecodeSteamSecret pins the encoding precedence for `tess add --type steam`.
// spec/otpauth.md § Steam Guard says the secret arrives as base64, so base64
// wins for a string that is valid in both alphabets.
func TestDecodeSteamSecret(t *testing.T) {
	raw := []byte("12345678901234567890")
	padded := base64.StdEncoding.EncodeToString(raw) // ends in '='
	unpadded := base64.RawStdEncoding.EncodeToString(raw)

	got, err := decodeSteamSecret(padded)
	if err != nil || !bytes.Equal(got, raw) {
		t.Errorf("padded base64: got %q, %v", got, err)
	}
	got, err = decodeSteamSecret(unpadded)
	if err != nil || !bytes.Equal(got, raw) {
		t.Errorf("unpadded base64: got %q, %v", got, err)
	}

	// A key handed over in otpauth (base32) form still works: 21 characters is
	// a legal base32 length and an illegal base64 one, so only base32 can decode it.
	const b32Only = "GAYTEMZUGU3DOOBZMFRGG"
	got, err = decodeSteamSecret(b32Only)
	if err != nil {
		t.Fatalf("base32 fallback: %v", err)
	}
	if !bytes.Equal(got, []byte("0123456789abc")) {
		t.Errorf("base32 fallback decoded to %q, want %q", got, "0123456789abc")
	}

	// JBSWY3DPEHPK3PXP is canonical base32 AND valid base64, and the two
	// alphabets give different bytes. base64 wins, per the spec.
	const ambiguous = "JBSWY3DPEHPK3PXP"
	wantB64, err := base64.RawStdEncoding.DecodeString(ambiguous)
	if err != nil {
		t.Fatal(err)
	}
	wantB32, err := base32x.Decode(ambiguous)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(wantB64, wantB32) {
		t.Fatal("fixture is not actually ambiguous")
	}
	got, err = decodeSteamSecret(ambiguous)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, wantB64) {
		t.Errorf("ambiguous secret decoded as base32 (% x), want base64 (% x)", got, wantB64)
	}

	if _, err := decodeSteamSecret(""); err == nil {
		t.Error("an empty steam secret must be rejected")
	}
	if _, err := decodeSteamSecret("!!! not a secret !!!"); err == nil {
		t.Error("garbage must be rejected")
	}
}

// TestManualSteamAccountUsesBase64 checks the production caller wiring.
func TestManualSteamAccountUsesBase64(t *testing.T) {
	raw := []byte("12345678901234567890")
	a, err := manualAccount("Steam", "gamer", base64.StdEncoding.EncodeToString(raw), "steam", "SHA1", 6, 30)
	if err != nil {
		t.Fatalf("manualAccount: %v", err)
	}
	if !bytes.Equal(a.Secret, raw) {
		t.Errorf("secret = % x, want % x", a.Secret, raw)
	}
	if a.Digits != 5 {
		t.Errorf("steam digits = %d, want 5", a.Digits)
	}
}

func TestAddFromArgSetupKey(t *testing.T) {
	accts, _, err := addFromArg("ZB573K4APD63E6RLD3WAHI3QFZ35RLEP", "", "", "SHA1", 6, 30)
	if err != nil {
		t.Fatalf("addFromArg: %v", err)
	}
	if len(accts) != 1 {
		t.Fatalf("expected 1 account, got %d", len(accts))
	}
	a := accts[0]
	if a.Type != account.TOTP || a.Algorithm != "SHA1" || a.Digits != 6 || a.Period != 30 {
		t.Errorf("setup-key account defaults wrong: %+v", a)
	}
	if a.Issuer != "" || a.Account != "" {
		t.Errorf("setup-key issuer/account should be empty, got %q/%q", a.Issuer, a.Account)
	}
	if len(a.Secret) == 0 {
		t.Error("setup-key secret should decode to bytes")
	}
}

func TestAddFromArgSetupKeyOverrides(t *testing.T) {
	accts, _, err := addFromArg("ZB573K4APD63E6RLD3WAHI3QFZ35RLEP", "GitHub", "me", "SHA256", 8, 60)
	if err != nil {
		t.Fatalf("addFromArg: %v", err)
	}
	a := accts[0]
	if a.Issuer != "GitHub" || a.Account != "me" || a.Algorithm != "SHA256" || a.Digits != 8 || a.Period != 60 {
		t.Errorf("overrides not applied: %+v", a)
	}
}

// writeTwoQRPNG composites two QR codes side by side with a white gutter onto a
// single PNG at path.
func writeTwoQRPNG(t *testing.T, path, textA, textB string) {
	t.Helper()
	enc := func(text string) *gozxing.BitMatrix {
		m, err := qrcode.NewQRCodeWriter().Encode(text, gozxing.BarcodeFormat_QR_CODE, 200, 200, nil)
		if err != nil {
			t.Fatalf("encode qr: %v", err)
		}
		return m
	}
	a, b := enc(textA), enc(textB)
	const gutter = 40
	w := a.GetWidth() + gutter + b.GetWidth()
	h := a.GetHeight()
	canvas := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			canvas.Set(x, y, color.White)
		}
	}
	paint := func(m *gozxing.BitMatrix, xOff int) {
		for y := 0; y < m.GetHeight(); y++ {
			for x := 0; x < m.GetWidth(); x++ {
				if m.Get(x, y) {
					canvas.Set(xOff+x, y, color.Black)
				}
			}
		}
	}
	paint(a, 0)
	paint(b, a.GetWidth()+gutter)
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	defer f.Close()
	if err := png.Encode(f, canvas); err != nil {
		t.Fatalf("png encode: %v", err)
	}
}

// stubStdin replaces the standard input source for one test.
func stubStdin(t *testing.T, text string) {
	t.Helper()
	old := stdinReader
	stdinReader = strings.NewReader(text)
	t.Cleanup(func() { stdinReader = old })
}

// TestImportStdinReadsThePipeline covers `... | tess import -`, including the
// dash arriving among ordinary paths.
func TestImportStdinReadsThePipeline(t *testing.T) {
	stubStdin(t, "otpauth://totp/Pipe:me?secret=JBSWY3DPEHPK3PXP&issuer=Pipe\n")

	b := collectImport(nil, nil, nil, nil, []string{"-"})
	if len(b.accounts) != 1 || b.accounts[0].Issuer != "Pipe" {
		t.Fatalf("stdin not parsed: %+v (%v)", b.accounts, b.problems)
	}
}

func TestImportStdinAttributesFailuresToStdin(t *testing.T) {
	stubStdin(t, "otpauth://totp/Pipe:me?secret=JBSWY3DPEHPK3PXP&issuer=Pipe\nnot a uri\n")

	b := collectImport(nil, nil, nil, nil, []string{"-"})
	if len(b.accounts) != 1 {
		t.Fatalf("the good line should still import: %+v", b.accounts)
	}
	if len(b.problems) != 1 || !strings.HasPrefix(b.problems[0].source, "stdin") {
		t.Fatalf("problem should name stdin, got %+v", b.problems)
	}
}
