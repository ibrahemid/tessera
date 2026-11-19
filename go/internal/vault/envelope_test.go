package vault

import (
	"bytes"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/ibrahemid/tessera/go/internal/account"
)

var lowCost = Argon2Params{V: 1, M: 8, T: 1, P: 1}

func lowCostWrap(t *testing.T, dek []byte, passphrase string, fill byte) wrap {
	t.Helper()
	salt := bytes.Repeat([]byte{fill}, saltLen)
	p := lowCost
	b, err := seal(derivePassphraseKey(passphrase, salt, p), dek)
	if err != nil {
		t.Fatal(err)
	}
	return wrap{Type: passphraseType, KDF: kdfName, Params: &p, Salt: salt, Nonce: b.Nonce, CT: b.CT}
}

func lowCostEnvelope(t *testing.T, accts []account.Account, passphrase string) (*Envelope, []byte) {
	t.Helper()
	dek := bytes.Repeat([]byte{0x42}, dekLen)
	plain, err := account.CanonicalJSON(accts)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := seal(dek, plain)
	if err != nil {
		t.Fatal(err)
	}
	return &Envelope{
		Version: account.FormatVersion(accts),
		AEAD:    aeadName,
		Wraps:   []wrap{lowCostWrap(t, dek, passphrase, 1)},
		Payload: payload,
	}, dek
}

func seWrap(t *testing.T) wrap {
	t.Helper()
	return wrap{
		Type: secureEnclaveType, KDF: "se-keychain-v1",
		SEKey: bytes.Repeat([]byte{9}, 16),
		Nonce: bytes.Repeat([]byte{3}, nonceLen),
		CT:    bytes.Repeat([]byte{4}, dekLen+16),
	}
}

func compactJSON(t *testing.T, e *Envelope) string {
	t.Helper()
	b, err := e.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestMarshalRefusesWrapCountOutOfRange(t *testing.T) {
	env, dek := lowCostEnvelope(t, accounts(), "pw")
	env.Wraps = nil
	if _, err := env.MarshalJSON(); !errors.Is(err, ErrNoWraps) {
		t.Errorf("zero wraps: got %v, want ErrNoWraps", err)
	}
	for i := 0; i < maxWraps+1; i++ {
		env.Wraps = append(env.Wraps, lowCostWrap(t, dek, "pw", byte(i)))
	}
	if _, err := env.Marshal(); !errors.Is(err, ErrTooManyWraps) {
		t.Errorf("17 wraps: got %v, want ErrTooManyWraps", err)
	}
	env.Wraps = env.Wraps[:maxWraps]
	data, err := env.Marshal()
	if err != nil {
		t.Fatalf("16 wraps must marshal: %v", err)
	}
	if _, err := Unmarshal(data); err != nil {
		t.Fatalf("16 wraps must unmarshal: %v", err)
	}
}

func TestUnmarshalWrapCountAndVersion(t *testing.T) {
	env, dek := lowCostEnvelope(t, accounts(), "pw")
	for i := 1; i < maxWraps; i++ {
		env.Wraps = append(env.Wraps, lowCostWrap(t, dek, "pw", byte(i)))
	}
	first, err := env.Wraps[0].marshal()
	if err != nil {
		t.Fatal(err)
	}
	seventeen := strings.Replace(compactJSON(t, env), `"wraps":[`, `"wraps":[`+string(first)+",", 1)
	if _, err := Unmarshal([]byte(seventeen)); !errors.Is(err, ErrTooManyWraps) {
		t.Errorf("17 wraps on disk: got %v, want ErrTooManyWraps", err)
	}
	env.Wraps = env.Wraps[:1]
	base := compactJSON(t, env)
	for _, tc := range []struct {
		version string
		want    error
	}{{"1", nil}, {"2", nil}, {"0", ErrUnsupportedVersion}, {"3", ErrUnsupportedVersion}, {"1.0", ErrUnsupportedVersion}, {`"1"`, ErrUnsupportedVersion}} {
		data := strings.Replace(base, `"version":1`, `"version":`+tc.version, 1)
		_, err := Unmarshal([]byte(data))
		if tc.want == nil && err != nil || tc.want != nil && !errors.Is(err, tc.want) {
			t.Errorf("version %s: got %v, want %v", tc.version, err, tc.want)
		}
	}
	if _, err := Unmarshal([]byte(strings.Replace(base, aeadName, "aes256gcm", 1))); !errors.Is(err, ErrUnsupportedAEAD) {
		t.Errorf("unknown aead: got %v", err)
	}
}

func TestVersionDerivedOnSeal(t *testing.T) {
	v1 := accounts()
	v2 := accounts()
	v2[0].Note = "recovery codes in the safe"
	env, err := Seal(v2, "pw")
	if err != nil {
		t.Fatal(err)
	}
	if env.Version != 2 {
		t.Fatalf("seal with a v2 field: version %d, want 2", env.Version)
	}
	data, err := env.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(`"version": 2`)) {
		t.Errorf("marshalled envelope does not carry version 2: %s", data)
	}
	dek, err := env.UnwrapDEK("pw")
	if err != nil {
		t.Fatal(err)
	}
	if err := env.UpdateAccountsWithDEK(dek, v1); err != nil {
		t.Fatal(err)
	}
	if env.Version != 1 {
		t.Errorf("clearing the last v2 field: version %d, want 1", env.Version)
	}
	ext := accounts()
	ext[1].Extra = map[string]json.RawMessage{"x_future": json.RawMessage(`[1,{"a":null}]`)}
	if err := env.UpdateAccounts("pw", ext); err != nil {
		t.Fatal(err)
	}
	if env.Version != 2 {
		t.Errorf("extension field: version %d, want 2", env.Version)
	}
	if env2, _ := Seal(v1, "pw"); env2.Version != 1 {
		t.Errorf("seal of v1 accounts: version %d, want 1", env2.Version)
	}
}

func TestUnknownKeysAndWrapsPreserved(t *testing.T) {
	env, err := Seal(accounts(), "pw")
	if err != nil {
		t.Fatal(err)
	}
	base := compactJSON(t, env)
	pw := strings.TrimSuffix(strings.SplitN(strings.SplitN(base, `"wraps":[`, 2)[1], `],"payload"`, 2)[0], "}")
	in := strings.Replace(base, `"wraps":[`+pw+`}`,
		`"wraps":[`+pw+`,"hint": "x" },{"type":"fido2","blob":"AQ==","n": [1, 2]}`, 1)
	in = strings.TrimSuffix(in, "}") + `,"zz_future":{"b": true, "a":[1,-2]},"aa_future":"\u2028"}`
	e, err := Unmarshal([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if got := e.WrapTypes(); !slices.Equal(got, []string{"passphrase", "fido2"}) {
		t.Fatalf("wrap types %v", got)
	}
	if _, err := e.Open("pw"); err != nil {
		t.Fatal(err)
	}
	if err := e.UpdateAccounts("pw", accounts()[:1]); err != nil {
		t.Fatal(err)
	}
	out := compactJSON(t, e)
	for _, want := range []string{
		`"hint":"x"}`,
		`{"type":"fido2","blob":"AQ==","n":[1,2]}`,
		`"aa_future":"\u2028","zz_future":{"b":true,"a":[1,-2]}}`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("written envelope lost %s:\n%s", want, out)
		}
	}
	again, err := Unmarshal([]byte(out))
	if err != nil {
		t.Fatal(err)
	}
	if compactJSON(t, again) != out {
		t.Error("second load and save is not byte-identical")
	}
}

func TestPayloadUnknownKeyRejected(t *testing.T) {
	env, _ := lowCostEnvelope(t, accounts(), "pw")
	in := strings.Replace(compactJSON(t, env), `"payload":{`, `"payload":{"aad":"",`, 1)
	if _, err := Unmarshal([]byte(in)); !errors.Is(err, ErrCorrupt) {
		t.Errorf("unknown payload key: got %v, want ErrCorrupt", err)
	}
}

func TestDuplicateKeyRejectedAtAnyDepth(t *testing.T) {
	env, _ := lowCostEnvelope(t, accounts(), "pw")
	base := compactJSON(t, env)
	cases := map[string]string{
		"top":     strings.Replace(base, `"aead":`, `"version":1,"aead":`, 1),
		"wrap":    strings.Replace(base, `"kdf":"argon2id"`, `"kdf":"argon2id","kdf":"argon2id"`, 1),
		"params":  strings.Replace(base, `"m":8`, `"m":8,"m":8`, 1),
		"payload": strings.Replace(base, `"payload":{`, `"payload":{"ct":"AA==",`, 1),
		"escaped": strings.Replace(base, `"aead":`, `"\u0076ersion":1,"aead":`, 1),
		"extra":   strings.TrimSuffix(base, "}") + `,"x":{"a":1,"a":2}}`,
	}
	for name, in := range cases {
		if in == base {
			t.Fatalf("%s: mutation did not apply", name)
		}
		if _, err := Unmarshal([]byte(in)); !errors.Is(err, ErrCorrupt) {
			t.Errorf("%s: got %v, want ErrCorrupt", name, err)
		}
	}
}

func TestWrongCaseKeyRejected(t *testing.T) {
	env, _ := lowCostEnvelope(t, accounts(), "pw")
	in := strings.Replace(compactJSON(t, env), `"wraps":`, `"WRAPS":`, 1)
	if _, err := Unmarshal([]byte(in)); !errors.Is(err, ErrCorrupt) {
		t.Errorf("WRAPS: got %v, want ErrCorrupt", err)
	}
}

func TestPassphraseAttemptsCapped(t *testing.T) {
	env, dek := lowCostEnvelope(t, accounts(), "pw")
	right := env.Wraps[0]
	env.Wraps = nil
	for i := 0; i < maxPassphraseAttempts; i++ {
		env.Wraps = append(env.Wraps, lowCostWrap(t, dek, "other", byte(10+i)))
	}
	env.Wraps = append(env.Wraps, right)
	if _, err := env.UnwrapDEK("pw"); !errors.Is(err, ErrWrongPassphrase) {
		t.Errorf("fifth well-formed wrap must not be tried: got %v", err)
	}
	env.Wraps = append(env.Wraps[1:maxPassphraseAttempts], right)
	if _, err := env.UnwrapDEK("pw"); err != nil {
		t.Errorf("fourth well-formed wrap must be tried: %v", err)
	}
}

func TestMalformedWrapsDoNotCountTowardAttempts(t *testing.T) {
	env, dek := lowCostEnvelope(t, accounts(), "pw")
	right := env.Wraps[0]
	env.Wraps = nil
	for i := 0; i < maxPassphraseAttempts+2; i++ {
		bad := lowCostWrap(t, dek, "pw", byte(20+i))
		bad.Salt = bad.Salt[:saltLen-1]
		env.Wraps = append(env.Wraps, bad)
	}
	env.Wraps = append(env.Wraps, right)
	if _, err := env.UnwrapDEK("pw"); err != nil {
		t.Errorf("malformed wraps must be skipped without using attempts: %v", err)
	}
}

func TestFirstFailedCheckReported(t *testing.T) {
	env, dek := lowCostEnvelope(t, accounts(), "pw")
	salt := lowCostWrap(t, dek, "pw", 2)
	salt.Salt = salt.Salt[:5]
	cost := lowCostWrap(t, dek, "pw", 3)
	cost.Params = &Argon2Params{V: 1, M: 8, T: 1, P: 17}
	env.Wraps = []wrap{salt, cost}
	_, err := env.UnwrapDEK("pw")
	if !errors.Is(err, ErrCorrupt) || !strings.Contains(err.Error(), "salt") {
		t.Errorf("got %v, want the salt check", err)
	}
}

func TestUnwrapRejectsUnknownVersionInMemory(t *testing.T) {
	env, _ := lowCostEnvelope(t, accounts(), "pw")
	env.Version = 3
	if _, err := env.Open("pw"); !errors.Is(err, ErrUnsupportedVersion) {
		t.Errorf("got %v", err)
	}
}

func TestRotatePassphrase(t *testing.T) {
	accts := accounts()
	accts[0].Note = "n"
	env, oldDEK := lowCostEnvelope(t, accts, "old")
	env.Wraps = append(env.Wraps, seWrap(t), seWrap(t), wrap{Type: "fido2", raw: json.RawMessage(`{"type":"fido2"}`)})
	env.extra = map[string]json.RawMessage{"x_future": json.RawMessage(`1`)}
	want, err := account.CanonicalJSON(accts)
	if err != nil {
		t.Fatal(err)
	}

	removed, err := env.RotatePassphrase("old", "new")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(removed, []string{"secure-enclave", "fido2"}) {
		t.Errorf("removed %v", removed)
	}
	if got := env.WrapTypes(); !slices.Equal(got, []string{"passphrase"}) {
		t.Errorf("wraps after rotation %v", got)
	}
	if env.Version != 2 {
		t.Errorf("version %d", env.Version)
	}
	if _, err := env.OpenWithDEK(oldDEK); !errors.Is(err, ErrWrongPassphrase) {
		t.Errorf("old DEK still opens the payload: %v", err)
	}
	if _, err := env.Open("old"); !errors.Is(err, ErrWrongPassphrase) {
		t.Errorf("old passphrase still opens: %v", err)
	}
	got, err := env.Open("new")
	if err != nil {
		t.Fatal(err)
	}
	gotBytes, _ := account.CanonicalJSON(got)
	if !bytes.Equal(gotBytes, want) {
		t.Error("rotation changed the account bytes")
	}
	if !strings.Contains(compactJSON(t, env), `"x_future":1`) {
		t.Error("rotation dropped an unknown top-level key")
	}
	if env.NeedsPersist() {
		t.Error("a rotated envelope carries no pending upgrade")
	}
}

func TestRekeyKeepsPassphrase(t *testing.T) {
	env, oldDEK := lowCostEnvelope(t, accounts(), "pw")
	removed, err := env.Rekey("pw")
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 0 || removed == nil {
		t.Errorf("removed %#v, want empty non-nil", removed)
	}
	if _, err := env.OpenWithDEK(oldDEK); !errors.Is(err, ErrWrongPassphrase) {
		t.Error("rekey kept the old DEK")
	}
	newDEK, err := env.UnwrapDEK("pw")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(newDEK, oldDEK) {
		t.Error("rekey reused the DEK")
	}
}

func TestRotationFailureLeavesEnvelopeUnchanged(t *testing.T) {
	env, _ := lowCostEnvelope(t, accounts(), "pw")
	env.Wraps = append(env.Wraps, seWrap(t))
	before := compactJSON(t, env)
	if _, err := env.RotatePassphrase("wrong", "new"); !errors.Is(err, ErrWrongPassphrase) {
		t.Errorf("got %v", err)
	}
	if _, _, err := env.Rotate("new", []account.Account{{ID: "bad"}}); err == nil {
		t.Error("invalid accounts must not rotate")
	}
	if compactJSON(t, env) != before {
		t.Error("a failed rotation modified the envelope")
	}
	app := &Envelope{Version: 1, AEAD: aeadName, Wraps: []wrap{seWrap(t)}, Payload: env.Payload}
	if _, err := app.Rekey("pw"); !errors.Is(err, ErrAppOnlyVault) {
		t.Errorf("app-only vault: got %v", err)
	}
	if len(app.Wraps) != 1 {
		t.Error("rotation without a passphrase wrap must not proceed")
	}
}

func TestChangePassphraseRotatesAndDropsSEWrap(t *testing.T) {
	env, oldDEK := lowCostEnvelope(t, accounts(), "old")
	env.Wraps = append(env.Wraps, seWrap(t))
	if err := env.ChangePassphrase("old", "new"); err != nil {
		t.Fatal(err)
	}
	if got := env.WrapTypes(); !slices.Equal(got, []string{"passphrase"}) {
		t.Errorf("wraps %v", got)
	}
	if _, err := env.OpenWithDEK(oldDEK); err == nil {
		t.Error("passphrase change kept the old DEK")
	}
}
