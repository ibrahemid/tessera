package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ibrahemid/tessera/go/internal/account"
)

func TestImportFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "accounts.txt")
	content := `# comment
otpauth://totp/GitHub:me?secret=JBSWY3DPEHPK3PXP&issuer=GitHub

otpauth://totp/AWS:root?secret=GEZDGNBVGY3TQOJQ&issuer=AWS
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	accts, err := importFromFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(accts) != 2 {
		t.Fatalf("expected 2 accounts, got %d", len(accts))
	}
	if accts[0].Issuer != "GitHub" || accts[1].Issuer != "AWS" {
		t.Errorf("unexpected issuers: %s, %s", accts[0].Issuer, accts[1].Issuer)
	}
}

func TestImportFromFileRejectsGarbage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.txt")
	os.WriteFile(path, []byte("not a uri\n"), 0o600)
	if _, err := importFromFile(path); err == nil {
		t.Error("expected error on non-URI line")
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
