package otp

import (
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/ibrahemid/tessera/go/internal/spectest"
)

type timeRangeVectors struct {
	OTPTimeRange struct {
		SecretB64 string `json:"secret_b64"`
		Algorithm string `json:"algorithm"`
		Digits    int    `json:"digits"`
		Period    int    `json:"period"`
		Accepted  []struct {
			Time  int64  `json:"time"`
			TOTP  string `json:"totp"`
			Steam string `json:"steam"`
		} `json:"accepted"`
		Refused []int64 `json:"refused"`
		Error   string  `json:"error"`
		Message string  `json:"message"`
	} `json:"otp_time_range"`
}

func TestTimeRangeVectors(t *testing.T) {
	var v timeRangeVectors
	spectest.Load(t, &v)
	tr := v.OTPTimeRange
	if len(tr.Accepted) == 0 || len(tr.Refused) == 0 || tr.Error != "time_range" {
		t.Fatal("otp_time_range is incomplete")
	}
	if tr.Message != ErrTimeRange.Error() {
		t.Fatalf("message = %q, want %q", tr.Message, ErrTimeRange.Error())
	}
	secret, err := base64.StdEncoding.DecodeString(tr.SecretB64)
	if err != nil {
		t.Fatal(err)
	}
	alg, err := ParseAlgorithm(tr.Algorithm)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range tr.Accepted {
		at := time.Unix(c.Time, 0)
		if got, err := TOTP(secret, at, tr.Period, tr.Digits, alg); err != nil || got != c.TOTP {
			t.Fatalf("TOTP at %d = %q, %v; want %q", c.Time, got, err, c.TOTP)
		}
		if got, err := Steam(secret, at); err != nil || got != c.Steam {
			t.Fatalf("Steam at %d = %q, %v; want %q", c.Time, got, err, c.Steam)
		}
	}
	for _, at := range tr.Refused {
		if _, err := TOTP(secret, time.Unix(at, 0), tr.Period, tr.Digits, alg); !errors.Is(err, ErrTimeRange) {
			t.Fatalf("TOTP at %d: err = %v, want ErrTimeRange", at, err)
		}
		if _, err := Steam(secret, time.Unix(at, 0)); !errors.Is(err, ErrTimeRange) {
			t.Fatalf("Steam at %d: err = %v, want ErrTimeRange", at, err)
		}
	}
}
