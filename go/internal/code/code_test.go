package code

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/otp"
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

func TestForRefusesUnknownTypes(t *testing.T) {
	for _, typ := range []account.Type{"future-otp", ""} {
		a := account.Account{ID: "x", Type: typ, Secret: []byte("12345678901234567890"), Algorithm: "SHA1", Digits: 6, Period: 30}
		_, err := For(a, time.Unix(59, 0))
		var unknown *UnknownTypeError
		if !errors.As(err, &unknown) || unknown.Type != typ || unknown.ID != "x" {
			t.Fatalf("type %q: err = %v, want an UnknownTypeError naming the account", typ, err)
		}
	}
}

func TestForRefusesACounterOutsideTheRange(t *testing.T) {
	for _, c := range []int64{-1, math.MaxInt64} {
		a := account.Account{ID: "x", Type: account.HOTP, Secret: []byte("12345678901234567890"), Algorithm: "SHA1", Digits: 6, Counter: c}
		if _, err := For(a, time.Now()); !errors.Is(err, account.ErrCounterRange) {
			t.Fatalf("counter %d: err = %v, want ErrCounterRange", c, err)
		}
	}
}

func TestForRefusesATimeBeforeTheEpoch(t *testing.T) {
	for _, typ := range []account.Type{account.TOTP, account.Steam} {
		a := account.Account{ID: "x", Type: typ, Secret: []byte("12345678901234567890"), Algorithm: "SHA1", Digits: 6, Period: 30}
		for _, at := range []time.Time{time.Unix(-1, 0), time.Unix(-30, 0), time.Unix(0, -1), time.Unix(math.MinInt64, 0)} {
			if _, err := For(a, at); !errors.Is(err, otp.ErrTimeRange) {
				t.Fatalf("%s at %d: err = %v, want ErrTimeRange", typ, at.Unix(), err)
			}
		}
		if _, err := For(a, time.Unix(math.MaxInt64, 0)); err != nil {
			t.Fatalf("%s at the largest time: %v", typ, err)
		}
	}
}
