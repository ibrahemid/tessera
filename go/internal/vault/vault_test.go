package vault

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ibrahemid/tessera/go/internal/account"
)

func accounts() []account.Account {
	return []account.Account{
		{ID: "1", Type: account.TOTP, Issuer: "ACME", Account: "a@x.com",
			Secret: []byte("12345678901234567890"), Algorithm: "SHA1", Digits: 6, Period: 30},
		{ID: "2", Type: account.HOTP, Issuer: "Ex", Account: "b@x.com",
			Secret: []byte("abcdefghij"), Algorithm: "SHA256", Digits: 8, Period: 30, Counter: 7},
	}
}

func TestSealOpenRoundTrip(t *testing.T) {
	env, err := Seal(accounts(), "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	got, err := env.Open("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Issuer != "ACME" || got[1].Counter != 7 {
		t.Errorf("round trip mismatch: %+v", got)
	}
}

func TestWrongPassphraseRejected(t *testing.T) {
	env, _ := Seal(accounts(), "right")
	if _, err := env.Open("wrong"); err != ErrWrongPassphrase {
		t.Errorf("expected ErrWrongPassphrase, got %v", err)
	}
}

func TestTamperedTagRejected(t *testing.T) {
	env, _ := Seal(accounts(), "pw")
	env.Payload.CT[len(env.Payload.CT)-1] ^= 0xff // flip a tag byte
	if _, err := env.Open("pw"); err == nil {
		t.Error("expected tampered payload to be rejected")
	}
}

func TestTamperedWrapRejected(t *testing.T) {
	env, _ := Seal(accounts(), "pw")
	env.Wraps[0].CT[0] ^= 0xff
	if _, err := env.Open("pw"); err != ErrWrongPassphrase {
		t.Errorf("expected wrap tamper -> ErrWrongPassphrase, got %v", err)
	}
}

func TestMarshalUnmarshalRoundTrip(t *testing.T) {
	env, _ := Seal(accounts(), "pw")
	data, err := env.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	env2, err := Unmarshal(data)
	if err != nil {
		t.Fatal(err)
	}
	got, err := env2.Open("pw")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 accounts, got %d", len(got))
	}
}

func TestNonceFreshPerSeal(t *testing.T) {
	e1, _ := Seal(accounts(), "pw")
	e2, _ := Seal(accounts(), "pw")
	if bytes.Equal(e1.Payload.Nonce, e2.Payload.Nonce) {
		t.Error("payload nonce reused across seals")
	}
	if bytes.Equal(e1.Payload.CT, e2.Payload.CT) {
		t.Error("ciphertext identical across seals (nonce/DEK reuse)")
	}
}

func TestChangePassphrase(t *testing.T) {
	env, _ := Seal(accounts(), "old")
	if err := env.ChangePassphrase("old", "new"); err != nil {
		t.Fatal(err)
	}
	if _, err := env.Open("old"); err != ErrWrongPassphrase {
		t.Error("old passphrase should no longer work")
	}
	if _, err := env.Open("new"); err != nil {
		t.Errorf("new passphrase should work: %v", err)
	}
}

func TestBase64URLSecretRejected(t *testing.T) {
	// A b64-url value with '-'/'_' must fail std-base64 []byte decoding.
	bad := `{"version":1,"aead":"xchacha20poly1305","wraps":[],"payload":{"nonce":"AA-_","ct":"AA"}}`
	var e Envelope
	if err := json.Unmarshal([]byte(bad), &e); err == nil {
		t.Error("expected base64url to be rejected by std-base64 decoder")
	}
}

func TestUnsupportedVersionRejected(t *testing.T) {
	bad := `{"version":99,"aead":"xchacha20poly1305","wraps":[],"payload":{"nonce":"","ct":""}}`
	if _, err := Unmarshal([]byte(bad)); err != ErrUnsupportedVersion {
		t.Errorf("expected ErrUnsupportedVersion, got %v", err)
	}
}

func TestEnvelopeUsesStandardBase64Padding(t *testing.T) {
	env, _ := Seal(accounts(), "pw")
	data, _ := env.Marshal()
	// Standard base64 padding uses '='; ensure no url-alphabet chars leaked.
	if strings.ContainsAny(string(data), "-_") {
		t.Error("envelope contains base64url characters")
	}
}
