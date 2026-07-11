package main

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/ibrahemid/tessera/go/internal/account"
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
	added, _ := mergeAccounts(s, b.accounts)
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
	added, skipped := mergeAccounts(s, b.accounts)
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
	added, skipped := mergeAccounts(s, []account.Account{dup, fresh})
	if added != 1 || skipped != 1 {
		t.Errorf("added=%d skipped=%d, want 1/1", added, skipped)
	}
	if len(s.accounts) != 2 {
		t.Errorf("expected 2 accounts after merge, got %d", len(s.accounts))
	}
}

func TestAddFromArgSetupKey(t *testing.T) {
	accts, err := addFromArg("ZB573K4APD63E6RLD3WAHI3QFZ35RLEP", "", "", "SHA1", 6, 30)
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
	accts, err := addFromArg("ZB573K4APD63E6RLD3WAHI3QFZ35RLEP", "GitHub", "me", "SHA256", 8, 60)
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
