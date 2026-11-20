package vault

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/spectest"
)

type v2VaultVectors struct {
	VaultV2 struct {
		Passphrase            string          `json:"passphrase"`
		Envelope              json.RawMessage `json:"envelope"`
		ExpectedCanonicalJSON string          `json:"expected_canonical_json"`
		ExpectedVersion       int             `json:"expected_version"`
	} `json:"vault_v2"`
	EnvelopeStrict struct {
		Passphrase            string `json:"passphrase"`
		ExpectedCanonicalJSON string `json:"expected_canonical_json"`
		Valid                 []struct {
			Name         string `json:"name"`
			EnvelopeJSON string `json:"envelope_json"`
		} `json:"valid"`
		Invalid []struct {
			Name         string `json:"name"`
			Passphrase   string `json:"passphrase"`
			EnvelopeJSON string `json:"envelope_json"`
			Error        string `json:"error"`
		} `json:"invalid"`
		Preserve struct {
			EnvelopeJSON     string   `json:"envelope_json"`
			UnknownTopLevel  []string `json:"unknown_top_level"`
			UnknownWrapTypes []string `json:"unknown_wrap_types"`
		} `json:"preserve"`
		LineSeparators struct {
			EnvelopeJSON         string `json:"envelope_json"`
			ExpectedEnvelopeJSON string `json:"expected_envelope_json"`
		} `json:"line_separators"`
	} `json:"envelope_strict"`
	Rotation                v2RotationVectors `json:"rotation"`
	RotationExtraPassphrase v2RotationVectors `json:"rotation_extra_passphrase"`
}

type v2RotationVectors struct {
	Passphrase            string          `json:"passphrase"`
	NewPassphrase         string          `json:"new_passphrase"`
	OldDEKB64             string          `json:"old_dek_b64"`
	Envelope              json.RawMessage `json:"envelope"`
	ExpectedCanonicalJSON string          `json:"expected_canonical_json"`
	ExpectedVersion       int             `json:"expected_version"`
	ExpectedRemoved       []string        `json:"expected_removed"`
	ExpectedWrapTypes     []string        `json:"expected_wrap_types"`
	KeptTopLevel          []string        `json:"kept_top_level"`
}

var v2EnvelopeErrors = map[string]error{
	"corrupt":             ErrCorrupt,
	"auth_failed":         ErrWrongPassphrase,
	"no_wraps":            ErrNoWraps,
	"too_many_wraps":      ErrTooManyWraps,
	"unsupported_version": ErrUnsupportedVersion,
	"unsupported_aead":    ErrUnsupportedAEAD,
	"app_only":            ErrAppOnlyVault,
	"no_passphrase_wrap":  ErrNoPassphraseWrap,
}

func loadV2VaultVectors(t *testing.T) v2VaultVectors {
	t.Helper()
	var v v2VaultVectors
	spectest.Load(t, &v)
	return v
}

func v2Canonical(t *testing.T, accts []account.Account) string {
	t.Helper()
	b, err := account.CanonicalJSON(accts)
	if err != nil {
		t.Fatalf("canonical: %v", err)
	}
	return string(b)
}

func v2OpenNoUpgrade(e *Envelope, passphrase string) ([]account.Account, error) {
	dek, _, err := e.unwrapWithPassphrase(passphrase)
	if err != nil {
		return nil, err
	}
	return e.OpenWithDEK(dek)
}

func v2Members(t *testing.T, data []byte) map[string]json.RawMessage {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	return m
}

func v2Compact(t *testing.T, raw []byte) string {
	t.Helper()
	var b bytes.Buffer
	if err := json.Compact(&b, raw); err != nil {
		t.Fatalf("compact: %v", err)
	}
	return b.String()
}

func v2WrapsOfType(t *testing.T, data []byte, typ string) []string {
	t.Helper()
	var wraps []json.RawMessage
	if err := json.Unmarshal(v2Members(t, data)["wraps"], &wraps); err != nil {
		t.Fatalf("decode wraps: %v", err)
	}
	var out []string
	for _, w := range wraps {
		var head struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(w, &head); err != nil {
			t.Fatalf("decode wrap: %v", err)
		}
		if head.Type == typ {
			out = append(out, v2Compact(t, w))
		}
	}
	return out
}

func TestV2VectorsVaultEnvelope(t *testing.T) {
	v := loadV2VaultVectors(t).VaultV2
	e, err := Unmarshal(v.Envelope)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	accts, err := e.Open(v.Passphrase)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if got := v2Canonical(t, accts); got != v.ExpectedCanonicalJSON {
		t.Fatalf("canonical mismatch\n got  %s\n want %s", got, v.ExpectedCanonicalJSON)
	}
	if e.Version != v.ExpectedVersion {
		t.Fatalf("version = %d, want %d", e.Version, v.ExpectedVersion)
	}
	if account.FormatVersion(accts) != v.ExpectedVersion {
		t.Fatalf("FormatVersion = %d, want %d", account.FormatVersion(accts), v.ExpectedVersion)
	}
	if e.NeedsPersist() {
		t.Fatal("default-cost wrap must not be resealed")
	}
}

func TestV2VectorsEnvelopeStrictValid(t *testing.T) {
	v := loadV2VaultVectors(t).EnvelopeStrict
	if len(v.Valid) == 0 {
		t.Fatal("envelope_strict.valid is empty")
	}
	for _, c := range v.Valid {
		t.Run(c.Name, func(t *testing.T) {
			e, err := Unmarshal([]byte(c.EnvelopeJSON))
			if err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			accts, err := v2OpenNoUpgrade(e, v.Passphrase)
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			if got := v2Canonical(t, accts); got != v.ExpectedCanonicalJSON {
				t.Fatalf("canonical mismatch\n got  %s\n want %s", got, v.ExpectedCanonicalJSON)
			}
		})
	}
}

func TestV2VectorsEnvelopeStrictInvalid(t *testing.T) {
	v := loadV2VaultVectors(t).EnvelopeStrict
	if len(v.Invalid) == 0 {
		t.Fatal("envelope_strict.invalid is empty")
	}
	for _, c := range v.Invalid {
		t.Run(c.Name, func(t *testing.T) {
			want, ok := v2EnvelopeErrors[c.Error]
			if !ok {
				t.Fatalf("unknown error code %q", c.Error)
			}
			passphrase := v.Passphrase
			if c.Passphrase != "" {
				passphrase = c.Passphrase
			}
			e, err := Unmarshal([]byte(c.EnvelopeJSON))
			if err == nil {
				_, err = v2OpenNoUpgrade(e, passphrase)
			}
			if !errors.Is(err, want) {
				t.Fatalf("err = %v, want %v", err, want)
			}
		})
	}
}

func TestV2VectorsEnvelopePreserve(t *testing.T) {
	v := loadV2VaultVectors(t).EnvelopeStrict
	orig := []byte(v.Preserve.EnvelopeJSON)
	e, err := Unmarshal(orig)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	same, err := e.MarshalJSON()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var a, b any
	if err := json.Unmarshal(orig, &a); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(same, &b); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("re-marshal changed the envelope\n got  %s\n want %s", same, orig)
	}

	dek, err := e.UnwrapDEK(v.Passphrase)
	if err != nil {
		t.Fatalf("unwrap: %v", err)
	}
	accts, err := e.OpenWithDEK(dek)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := e.UpdateAccountsWithDEK(dek, accts); err != nil {
		t.Fatalf("update: %v", err)
	}
	out, err := e.MarshalJSON()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	origMembers, outMembers := v2Members(t, orig), v2Members(t, out)
	for _, k := range v.Preserve.UnknownTopLevel {
		if v2Compact(t, outMembers[k]) != v2Compact(t, origMembers[k]) {
			t.Fatalf("top-level %q = %s, want %s", k, outMembers[k], origMembers[k])
		}
	}
	for _, typ := range append([]string{"secure-enclave"}, v.Preserve.UnknownWrapTypes...) {
		want, got := v2WrapsOfType(t, orig, typ), v2WrapsOfType(t, out, typ)
		if len(want) == 0 || !slices.Equal(got, want) {
			t.Fatalf("wraps of type %q = %v, want %v", typ, got, want)
		}
	}
}

func TestV2VectorsRotation(t *testing.T) {
	checkRotationVectors(t, loadV2VaultVectors(t).Rotation)
}

func TestV2VectorsRotationExtraPassphrase(t *testing.T) {
	v := loadV2VaultVectors(t).RotationExtraPassphrase
	if len(v.ExpectedRemoved) == 0 || v.ExpectedRemoved[0] != "passphrase" {
		t.Fatalf("expected_removed = %v, want it to start with passphrase", v.ExpectedRemoved)
	}
	checkRotationVectors(t, v)
}

func checkRotationVectors(t *testing.T, v v2RotationVectors) {
	t.Helper()
	if len(v.Envelope) == 0 {
		t.Fatal("rotation vector set is missing")
	}
	oldDEK, err := base64.StdEncoding.DecodeString(v.OldDEKB64)
	if err != nil {
		t.Fatalf("old dek: %v", err)
	}
	origMembers := v2Members(t, v.Envelope)
	cases := []struct {
		name   string
		rotate func(*Envelope) ([]string, error)
		opener string
		stale  string
	}{
		{"passwd", func(e *Envelope) ([]string, error) { return e.RotatePassphrase(v.Passphrase, v.NewPassphrase) }, v.NewPassphrase, v.Passphrase},
		{"rekey", func(e *Envelope) ([]string, error) { return e.Rekey(v.Passphrase) }, v.Passphrase, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e, err := Unmarshal(v.Envelope)
			if err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			removed, err := c.rotate(e)
			if err != nil {
				t.Fatalf("rotate: %v", err)
			}
			if !slices.Equal(removed, v.ExpectedRemoved) {
				t.Fatalf("removed = %v, want %v", removed, v.ExpectedRemoved)
			}
			if got := e.WrapTypes(); !slices.Equal(got, v.ExpectedWrapTypes) {
				t.Fatalf("wrap types = %v, want %v", got, v.ExpectedWrapTypes)
			}
			data, err := e.MarshalJSON()
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			outMembers := v2Members(t, data)
			for _, k := range v.KeptTopLevel {
				if v2Compact(t, outMembers[k]) != v2Compact(t, origMembers[k]) {
					t.Fatalf("top-level %q = %s, want %s", k, outMembers[k], origMembers[k])
				}
			}
			reopened, err := Unmarshal(data)
			if err != nil {
				t.Fatalf("re-unmarshal: %v", err)
			}
			accts, err := reopened.Open(c.opener)
			if err != nil {
				t.Fatalf("open after rotation: %v", err)
			}
			if got := v2Canonical(t, accts); got != v.ExpectedCanonicalJSON {
				t.Fatalf("canonical mismatch\n got  %s\n want %s", got, v.ExpectedCanonicalJSON)
			}
			if reopened.Version != v.ExpectedVersion {
				t.Fatalf("version = %d, want %d", reopened.Version, v.ExpectedVersion)
			}
			if _, err := reopened.OpenWithDEK(oldDEK); !errors.Is(err, ErrWrongPassphrase) {
				t.Fatalf("old DEK err = %v, want ErrWrongPassphrase", err)
			}
			if c.stale != "" {
				if _, err := reopened.Open(c.stale); !errors.Is(err, ErrWrongPassphrase) {
					t.Fatalf("old passphrase err = %v, want ErrWrongPassphrase", err)
				}
			}
		})
	}
}

func TestV2VectorsEnvelopeLineSeparators(t *testing.T) {
	v := loadV2VaultVectors(t).EnvelopeStrict.LineSeparators
	in := []byte(v.EnvelopeJSON)
	if !bytes.Contains(in, []byte("\u2028")) || !bytes.Contains(in, []byte("\u2029")) {
		t.Fatal("line_separators input carries no raw line separators")
	}
	e, err := Unmarshal(in)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	out, err := e.Marshal()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(out) != v.ExpectedEnvelopeJSON {
		t.Fatalf("written envelope\n got  %s\n want %s", out, v.ExpectedEnvelopeJSON)
	}
	if bytes.Contains(out, []byte("\u2028")) || bytes.Contains(out, []byte("\u2029")) {
		t.Fatal("written envelope keeps a raw line separator")
	}
}
