package code

import (
	"testing"
	"time"

	"github.com/ibrahemid/tessera/go/internal/account"
)

func TestForTOTPMatchesRFC(t *testing.T) {
	// RFC 6238 SHA1 seed, T=59 -> 8 digits 94287082; with 6 digits -> 287082.
	a := account.Account{
		Type: account.TOTP, Secret: []byte("12345678901234567890"),
		Algorithm: "SHA1", Digits: 6, Period: 30,
	}
	got, err := For(a, time.Unix(59, 0))
	if err != nil {
		t.Fatal(err)
	}
	if got != "287082" {
		t.Errorf("For TOTP = %s, want 287082", got)
	}
}

func TestForHOTP(t *testing.T) {
	a := account.Account{
		Type: account.HOTP, Secret: []byte("12345678901234567890"),
		Algorithm: "SHA1", Digits: 6, Counter: 0,
	}
	got, _ := For(a, time.Now())
	if got != "755224" {
		t.Errorf("For HOTP c=0 = %s, want 755224", got)
	}
}

func TestRemaining(t *testing.T) {
	a := account.Account{Type: account.TOTP, Period: 30}
	if r := Remaining(a, time.Unix(10, 0)); r != 20 {
		t.Errorf("Remaining = %d, want 20", r)
	}
	hotp := account.Account{Type: account.HOTP}
	if r := Remaining(hotp, time.Now()); r != 0 {
		t.Errorf("HOTP Remaining = %d, want 0", r)
	}
}
