package exporters

import (
	"strings"
	"testing"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/importers"
)

func TestAppleCSVSkipsCellsASpreadsheetWouldEvaluate(t *testing.T) {
	secret := mustSecret(t, "JBSWY3DPEHPK3PXP")
	mk := func(id, issuer, acct string) account.Account {
		return account.Account{ID: id, Type: account.TOTP, Issuer: issuer, Account: acct,
			Secret: secret, Algorithm: "SHA1", Digits: 6, Period: 30}
	}
	accts := []account.Account{
		mk("ok", "GitHub", "me"),
		mk("eq", "=HYPERLINK(\"http://x\")", "me"),
		mk("plus", "Bank", "+1 555"),
		mk("minus", "-2+3", "me"),
		mk("at", "Shop", "@SUM(A1)"),
		mk("inner", "A=B", "x-y@z"),
	}
	files, skipped, err := appleCSVExporter{}.Render(accts)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if len(skipped) != 4 {
		t.Fatalf("skipped %d accounts, want 4: %+v", len(skipped), skipped)
	}
	for _, s := range skipped {
		if !strings.Contains(s.Reason, "formula") {
			t.Errorf("skip reason should say why: %q", s.Reason)
		}
	}
	if len(files) != 1 {
		t.Fatalf("want 1 file, got %d", len(files))
	}
	data := string(files[0].Data)
	for _, bad := range []string{"HYPERLINK", "+1 555", "-2+3", "@SUM"} {
		if strings.Contains(data, bad) {
			t.Errorf("export still carries %q:\n%s", bad, data)
		}
	}
	got, _, recognized, err := importers.Parse(files[0].Data)
	if !recognized || err != nil {
		t.Fatalf("re-import: recognized=%v err=%v", recognized, err)
	}
	if len(got) != 2 {
		t.Fatalf("re-imported %d accounts, want the 2 safe ones", len(got))
	}
}

func TestAppleCSVWithEveryAccountSkippedWritesNoFile(t *testing.T) {
	a := account.Account{ID: "x", Type: account.TOTP, Issuer: "=1+1", Account: "me",
		Secret: mustSecret(t, "JBSWY3DPEHPK3PXP"), Algorithm: "SHA1", Digits: 6, Period: 30}
	files, skipped, err := appleCSVExporter{}.Render([]account.Account{a})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if len(files) != 0 || len(skipped) != 1 {
		t.Fatalf("files=%d skipped=%d, want 0 and 1", len(files), len(skipped))
	}
}
