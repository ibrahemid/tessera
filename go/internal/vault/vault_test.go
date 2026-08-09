package vault

import (
	"bytes"
	"encoding/json"
	"errors"
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

func TestUpdateAccountsPreservesWrapsAndOpens(t *testing.T) {
	env, _ := Seal(accounts(), "pw")
	// Simulate a second (non-passphrase) wrap that must survive an update.
	env.Wraps = append(env.Wraps, wrap{Type: "secure-enclave", Nonce: []byte("x"), CT: []byte("y")})
	newAccts := append(accounts(), account.Account{
		ID: "3", Type: account.TOTP, Issuer: "New", Account: "c@x.com",
		Secret: []byte("zzzzzzzzzz"), Algorithm: "SHA1", Digits: 6, Period: 30})
	if err := env.UpdateAccounts("pw", newAccts); err != nil {
		t.Fatal(err)
	}
	got, err := env.Open("pw")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Errorf("expected 3 accounts after update, got %d", len(got))
	}
	hasSE := false
	for _, w := range env.Wraps {
		if w.Type == "secure-enclave" {
			hasSE = true
		}
	}
	if !hasSE {
		t.Error("secure-enclave wrap was lost during update")
	}
}

func TestAppOnlyVaultError(t *testing.T) {
	env, _ := Seal(accounts(), "pw")
	env.Wraps = []wrap{{Type: "secure-enclave", Nonce: []byte("x"), CT: []byte("y")}}
	if _, err := env.Open("pw"); !errors.Is(err, ErrAppOnlyVault) {
		t.Errorf("expected ErrAppOnlyVault, got %v", err)
	}
	env.Wraps = nil
	if _, err := env.Open("pw"); !errors.Is(err, ErrNoPassphraseWrap) {
		t.Errorf("expected ErrNoPassphraseWrap, got %v", err)
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

// TestInsaneArgon2ParamsRejected pins that the costs read from an untrusted
// vault file are validated before argon2 runs. t=0 and p=0 panic inside
// x/crypto/argon2, and a multi-GiB m would hang the process instead of failing.
func TestInsaneArgon2ParamsRejected(t *testing.T) {
	cases := []struct {
		name   string
		params Argon2Params
	}{
		{"zero iterations", Argon2Params{V: 1, M: 131072, T: 0, P: 4}},
		{"zero parallelism", Argon2Params{V: 1, M: 131072, T: 3, P: 0}},
		{"memory 4 GiB", Argon2Params{V: 1, M: 4 * 1024 * 1024, T: 3, P: 4}},
		{"memory below floor", Argon2Params{V: 1, M: 1, T: 3, P: 4}},
		{"memory below 8*p", Argon2Params{V: 1, M: 16, T: 3, P: 8}},
		{"iterations absurd", Argon2Params{V: 1, M: 131072, T: 1 << 20, P: 4}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env, _ := Seal(accounts(), "pw")
			p := tc.params
			env.Wraps[0].Params = &p
			_, err := env.Open("pw")
			if !errors.Is(err, ErrCorrupt) {
				t.Fatalf("expected ErrCorrupt, got %v", err)
			}
		})
	}
}

func TestUnknownKDFSkipped(t *testing.T) {
	env, _ := Seal(accounts(), "pw")
	env.Wraps[0].KDF = "scrypt"
	if _, err := env.Open("pw"); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("an unknown kdf must not be argon2-derived silently, got %v", err)
	}
	// A second, well-formed argon2id wrap still opens the vault.
	env2, _ := Seal(accounts(), "pw")
	bad := env2.Wraps[0]
	bad.KDF = "scrypt"
	env2.Wraps = append([]wrap{bad}, env2.Wraps...)
	if _, err := env2.Open("pw"); err != nil {
		t.Fatalf("valid wrap alongside an unknown-kdf wrap should open: %v", err)
	}
	// With a usable wrap present, a wrong passphrase still reports itself as
	// such rather than blaming the malformed sibling wrap.
	if _, err := env2.Open("nope"); err != ErrWrongPassphrase {
		t.Errorf("wrong passphrase alongside a bad wrap: got %v, want ErrWrongPassphrase", err)
	}
}

func TestZeroWrapEnvelopeRejected(t *testing.T) {
	env, _ := Seal(accounts(), "pw")
	env.Wraps = nil
	data, err := env.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Unmarshal(data); !errors.Is(err, ErrNoWraps) {
		t.Fatalf("expected ErrNoWraps, got %v", err)
	}
}

// TestStaleParamVersionResealed covers spec/vault-format.md § passphrase wrap:
// a wrap written under an older params.v is re-wrapped at the current default
// on the next successful unlock, payload untouched.
func TestStaleParamVersionResealed(t *testing.T) {
	env, _ := Seal(accounts(), "pw")
	oldPayload := append([]byte(nil), env.Payload.CT...)
	oldSalt := append([]byte(nil), env.Wraps[0].Salt...)
	stale := *env.Wraps[0].Params
	stale.V = 0
	env.Wraps[0].Params = &stale

	if env.NeedsPersist() {
		t.Fatal("a freshly sealed envelope should not need persisting")
	}
	got, err := env.Open("pw")
	if err != nil || len(got) != 2 {
		t.Fatalf("open with stale params: %v", err)
	}
	if !env.NeedsPersist() {
		t.Fatal("a stale params.v wrap should be flagged for persistence")
	}
	if env.Wraps[0].Params.V != DefaultArgon2.V || env.Wraps[0].Params.M != DefaultArgon2.M {
		t.Errorf("wrap not upgraded to defaults: %+v", *env.Wraps[0].Params)
	}
	if bytes.Equal(env.Wraps[0].Salt, oldSalt) {
		t.Error("re-wrap must use a fresh salt")
	}
	if !bytes.Equal(env.Payload.CT, oldPayload) {
		t.Error("re-wrapping must not touch the payload")
	}
	if _, err := env.Open("pw"); err != nil {
		t.Fatalf("upgraded wrap must still open: %v", err)
	}
}

func TestCurrentParamVersionNotResealed(t *testing.T) {
	env, _ := Seal(accounts(), "pw")
	salt := append([]byte(nil), env.Wraps[0].Salt...)
	if _, err := env.Open("pw"); err != nil {
		t.Fatal(err)
	}
	if env.NeedsPersist() {
		t.Error("a current-version wrap must not be re-wrapped")
	}
	if !bytes.Equal(env.Wraps[0].Salt, salt) {
		t.Error("salt changed without a params.v bump")
	}
}

// TestUpdateAccountsWithDEKRoundTrip covers the cached-DEK write path used by
// every mutating command, so argon2 runs once per run instead of once per write.
func TestUpdateAccountsWithDEKRoundTrip(t *testing.T) {
	env, _ := Seal(accounts(), "pw")
	dek, err := env.UnwrapDEK("pw")
	if err != nil {
		t.Fatal(err)
	}
	extra := append(accounts(), account.Account{
		ID: "3", Type: account.TOTP, Issuer: "New", Account: "c@x.com",
		Secret: []byte("zzzzzzzzzz"), Algorithm: "SHA1", Digits: 6, Period: 30})
	if err := env.UpdateAccountsWithDEK(dek, extra); err != nil {
		t.Fatal(err)
	}
	// The same DEK still opens it, and so does the passphrase.
	got, err := env.OpenWithDEK(dek)
	if err != nil || len(got) != 3 {
		t.Fatalf("OpenWithDEK after update: %d accounts, %v", len(got), err)
	}
	got, err = env.Open("pw")
	if err != nil || len(got) != 3 {
		t.Fatalf("Open after update: %d accounts, %v", len(got), err)
	}
	if err := env.UpdateAccountsWithDEK(dek, []account.Account{{ID: "bad"}}); err == nil {
		t.Error("invalid accounts must not be sealed")
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
