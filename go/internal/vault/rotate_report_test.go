package vault

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/ibrahemid/tessera/go/internal/account"
)

func lowCostArgon2(t *testing.T) {
	t.Helper()
	old := DefaultArgon2
	DefaultArgon2 = Argon2Params{V: old.V, M: 64, T: 1, P: 1}
	t.Cleanup(func() { DefaultArgon2 = old })
}

func envelopeWithTwoPassphrases(t *testing.T) *Envelope {
	t.Helper()
	accts := []account.Account{{ID: "a", Type: account.TOTP, Issuer: "A", Account: "a",
		Secret: []byte("12345678901234567890"), Algorithm: "SHA1", Digits: 6, Period: 30}}
	env, err := Seal(accts, "first-passphrase")
	if err != nil {
		t.Fatal(err)
	}
	dek, err := env.UnwrapDEK("first-passphrase")
	if err != nil {
		t.Fatal(err)
	}
	second, err := newPassphraseWrap(dek, "second-passphrase")
	if err != nil {
		t.Fatal(err)
	}
	env.Wraps = append(env.Wraps, second)
	if _, err := env.Open("second-passphrase"); err != nil {
		t.Fatalf("second passphrase should open before rotation: %v", err)
	}
	return env
}

func TestRotationReportsADroppedPassphraseWrap(t *testing.T) {
	lowCostArgon2(t)
	env := envelopeWithTwoPassphrases(t)

	removed, err := env.RotatePassphrase("first-passphrase", "first-passphrase-new")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.Open("second-passphrase"); err == nil {
		t.Fatal("second passphrase still opens after rotation")
	}
	if len(removed) == 0 {
		t.Fatalf("rotation removed the wrap that opened with the second passphrase (2 wraps -> %d) but reported nothing removed", len(env.Wraps))
	}
}

func TestRotationReportsRemovedWrapsInEnvelopeOrder(t *testing.T) {
	lowCostArgon2(t)
	fido2 := wrap{Type: "fido2", raw: json.RawMessage(`{"type":"fido2"}`)}
	cases := []struct {
		name   string
		opener string
		lead   []wrap
		want   []string
	}{
		{"opened with the first", "first-passphrase", []wrap{fido2}, []string{"fido2", "passphrase"}},
		{"opened with the second", "second-passphrase", []wrap{fido2}, []string{"fido2", "passphrase"}},
		{"no other wrap", "second-passphrase", nil, []string{"passphrase"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			env := envelopeWithTwoPassphrases(t)
			env.Wraps = append(c.lead, env.Wraps...)
			removed, err := env.Rekey(c.opener)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(removed, c.want) {
				t.Fatalf("removed = %v, want %v", removed, c.want)
			}
		})
	}
}

func TestRekeyWithOnePassphraseReportsNothing(t *testing.T) {
	lowCostArgon2(t)
	env, err := Seal([]account.Account{{ID: "a", Type: account.TOTP, Issuer: "A", Account: "a",
		Secret: []byte("12345678901234567890"), Algorithm: "SHA1", Digits: 6, Period: 30}}, "only")
	if err != nil {
		t.Fatal(err)
	}
	removed, err := env.Rekey("only")
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 0 {
		t.Fatalf("removed = %v, want nothing", removed)
	}
}
