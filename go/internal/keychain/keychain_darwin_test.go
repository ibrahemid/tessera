//go:build darwin

package keychain

import (
	"os"
	"testing"
)

// TestKeychainRoundTrip exercises the real login keychain. It is skipped unless
// TESSERA_KEYCHAIN_TEST is set (CI has no keychain), uses a throwaway service so
// it never touches real "tessera" entries, and cleans up after itself.
func TestKeychainRoundTrip(t *testing.T) {
	if os.Getenv("TESSERA_KEYCHAIN_TEST") == "" {
		t.Skip("set TESSERA_KEYCHAIN_TEST=1 to run the login keychain integration test")
	}
	old := service
	service = "tessera-test"
	t.Cleanup(func() { service = old })

	account := "/tmp/tessera-test-vault.json"
	pass := `p@ss "with" \slashes and $pecial`
	t.Cleanup(func() { _ = Delete(account) })

	if has, err := Has(account); err != nil || has {
		t.Fatalf("precondition: has=%v err=%v", has, err)
	}
	if err := Store(account, pass); err != nil {
		t.Fatalf("store: %v", err)
	}
	has, err := Has(account)
	if err != nil || !has {
		t.Fatalf("after store: has=%v err=%v", has, err)
	}
	got, ok, err := Lookup(account)
	if err != nil || !ok || got != pass {
		t.Fatalf("lookup: got=%q ok=%v err=%v", got, ok, err)
	}
	if err := Delete(account); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := Delete(account); err != nil {
		t.Fatalf("delete of missing entry must be a no-op: %v", err)
	}
	if _, ok, _ := Lookup(account); ok {
		t.Fatal("entry should be gone after delete")
	}
}
