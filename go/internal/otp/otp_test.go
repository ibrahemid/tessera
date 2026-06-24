package otp

import (
	"testing"
	"time"

	"github.com/ibrahemid/tessera/go/internal/spectest"
)

type vectors struct {
	HOTP struct {
		SecretASCII string   `json:"_secret_ascii"`
		Digits      int      `json:"digits"`
		Algorithm   string   `json:"algorithm"`
		Codes       []string `json:"codes_by_counter"`
	} `json:"hotp_rfc4226"`
	TOTP struct {
		Cases []struct {
			Algorithm   string   `json:"algorithm"`
			SecretASCII string   `json:"secret_ascii"`
			Times       []int64  `json:"times"`
			Codes       []string `json:"codes"`
		} `json:"cases"`
	} `json:"totp_rfc6238"`
	Steam struct {
		Cases []struct {
			SecretB64 string `json:"secret_b64"`
			Time      int64  `json:"time"`
			Code      string `json:"code"`
		} `json:"cases"`
	} `json:"steam"`
}

func load(t *testing.T) vectors {
	var v vectors
	spectest.Load(t, &v)
	return v
}

func TestHOTP_RFC4226(t *testing.T) {
	v := load(t)
	secret := []byte(v.HOTP.SecretASCII)
	for counter, want := range v.HOTP.Codes {
		got, err := HOTP(secret, uint64(counter), v.HOTP.Digits, SHA1)
		if err != nil {
			t.Fatalf("counter %d: %v", counter, err)
		}
		if got != want {
			t.Errorf("HOTP counter %d = %s, want %s", counter, got, want)
		}
	}
}

func TestTOTP_RFC6238(t *testing.T) {
	v := load(t)
	for _, c := range v.TOTP.Cases {
		alg, err := ParseAlgorithm(c.Algorithm)
		if err != nil {
			t.Fatal(err)
		}
		secret := []byte(c.SecretASCII)
		for i, ts := range c.Times {
			got, err := TOTP(secret, time.Unix(ts, 0), 30, 8, alg)
			if err != nil {
				t.Fatalf("%s t=%d: %v", c.Algorithm, ts, err)
			}
			if got != c.Codes[i] {
				t.Errorf("TOTP %s t=%d = %s, want %s", c.Algorithm, ts, got, c.Codes[i])
			}
		}
	}
}

func TestSteam(t *testing.T) {
	v := load(t)
	if len(v.Steam.Cases) == 0 {
		t.Skip("steam vectors not yet generated")
	}
	for _, c := range v.Steam.Cases {
		secret, err := DecodeSteamSecret(c.SecretB64)
		if err != nil {
			t.Fatal(err)
		}
		got, err := Steam(secret, time.Unix(c.Time, 0))
		if err != nil {
			t.Fatal(err)
		}
		if got != c.Code {
			t.Errorf("Steam t=%d = %s, want %s", c.Time, got, c.Code)
		}
		if len(got) != 5 {
			t.Errorf("steam code %q not 5 chars", got)
		}
	}
}
