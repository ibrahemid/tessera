package vault

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/spectest"
	"golang.org/x/crypto/chacha20poly1305"
)

type crossVectors struct {
	VaultCrossdecrypt struct {
		Passphrase            string          `json:"passphrase"`
		ExpectedCanonicalJSON string          `json:"expected_canonical_json"`
		Envelope              json.RawMessage `json:"envelope"`
		Negatives             struct {
			WrongPassphrase string `json:"wrong_passphrase"`
		} `json:"negatives"`
	} `json:"vault_crossdecrypt"`
	Argon2 struct {
		Cases []struct {
			PassphraseB64 string `json:"passphrase_b64"`
			SaltB64       string `json:"salt_b64"`
			KeyB64        string `json:"key_b64"`
		} `json:"cases"`
	} `json:"argon2id"`
}

func TestPinnedEnvelopeDecryptsToCanonical(t *testing.T) {
	var v crossVectors
	spectest.Load(t, &v)
	env, err := Unmarshal(v.VaultCrossdecrypt.Envelope)
	if err != nil {
		t.Fatal(err)
	}
	got, err := env.Open(v.VaultCrossdecrypt.Passphrase)
	if err != nil {
		t.Fatalf("open pinned envelope: %v", err)
	}
	canonical, err := account.CanonicalJSON(got)
	if err != nil {
		t.Fatal(err)
	}
	if string(canonical) != v.VaultCrossdecrypt.ExpectedCanonicalJSON {
		t.Errorf("canonical mismatch:\n got: %s\nwant: %s", canonical, v.VaultCrossdecrypt.ExpectedCanonicalJSON)
	}
}

func TestPinnedEnvelopeNegatives(t *testing.T) {
	var v crossVectors
	spectest.Load(t, &v)

	env, err := Unmarshal(v.VaultCrossdecrypt.Envelope)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.Open(v.VaultCrossdecrypt.Negatives.WrongPassphrase); err != ErrWrongPassphrase {
		t.Errorf("wrong passphrase: expected ErrWrongPassphrase, got %v", err)
	}

	// Tamper a payload byte; decryption must fail even with the right passphrase.
	tampered, _ := Unmarshal(v.VaultCrossdecrypt.Envelope)
	tampered.Payload.CT[len(tampered.Payload.CT)-1] ^= 0x01
	if _, err := tampered.Open(v.VaultCrossdecrypt.Passphrase); err == nil {
		t.Error("tampered payload must not decrypt")
	}
}

// TestAEADPayloadVector confirms Go opens (and reproduces) the fixed-DEK AEAD
// vector. Combined with the Swift verifier asserting its seal equals this same
// ct byte-for-byte, this covers Go-seals/Swift-opens and Swift-seals/Go-opens.
func TestAEADPayloadVector(t *testing.T) {
	var raw struct {
		AEAD struct {
			DEKB64    string `json:"dek_b64"`
			NonceB64  string `json:"nonce_b64"`
			CTB64     string `json:"ct_b64"`
			Canonical string `json:"expected_canonical_json"`
		} `json:"aead_payload"`
	}
	spectest.Load(t, &raw)
	dek, _ := base64.StdEncoding.DecodeString(raw.AEAD.DEKB64)
	nonce, _ := base64.StdEncoding.DecodeString(raw.AEAD.NonceB64)
	ct, _ := base64.StdEncoding.DecodeString(raw.AEAD.CTB64)

	aead, err := chacha20poly1305.NewX(dek)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := aead.Open(nil, nonce, ct, nil)
	if err != nil {
		t.Fatalf("Go failed to open the shared AEAD vector: %v", err)
	}
	if string(plain) != raw.AEAD.Canonical {
		t.Errorf("decrypted payload != expected canonical")
	}
	// Re-seal deterministically; must equal the pinned ct (so it equals Swift's).
	if reSealed := aead.Seal(nil, nonce, plain, nil); !bytes.Equal(reSealed, ct) {
		t.Error("Go re-seal does not reproduce the pinned ciphertext")
	}
}

func TestArgon2idVector(t *testing.T) {
	var v crossVectors
	spectest.Load(t, &v)
	for _, c := range v.Argon2.Cases {
		pass, _ := base64.StdEncoding.DecodeString(c.PassphraseB64)
		salt, _ := base64.StdEncoding.DecodeString(c.SaltB64)
		wantKey, _ := base64.StdEncoding.DecodeString(c.KeyB64)
		got := derivePassphraseKey(string(pass), salt, DefaultArgon2)
		if base64.StdEncoding.EncodeToString(got) != base64.StdEncoding.EncodeToString(wantKey) {
			t.Errorf("argon2id key mismatch")
		}
	}
}
