package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/chacha20poly1305"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/store"
	"github.com/ibrahemid/tessera/go/internal/vault"
)

func addPassphraseWrap(t *testing.T, path, pass string) {
	t.Helper()
	dek := vaultDEK(t, path, "testpass123")
	params := vault.DefaultArgon2
	salt := bytes.Repeat([]byte{0x41}, 16)
	nonce := bytes.Repeat([]byte{0x42}, chacha20poly1305.NonceSizeX)
	key := argon2.IDKey([]byte(pass), salt, params.T, params.M, params.P, 32)
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		t.Fatal(err)
	}
	wrap, err := json.Marshal(map[string]any{
		"type": "passphrase", "kdf": "argon2id",
		"params": map[string]any{"v": params.V, "m": params.M, "t": params.T, "p": params.P},
		"salt":   base64.StdEncoding.EncodeToString(salt),
		"nonce":  base64.StdEncoding.EncodeToString(nonce),
		"ct":     base64.StdEncoding.EncodeToString(aead.Seal(nil, nonce, dek, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	var list []json.RawMessage
	if err := json.Unmarshal(doc["wraps"], &list); err != nil {
		t.Fatal(err)
	}
	if doc["wraps"], err = json.Marshal(append(list, wrap)); err != nil {
		t.Fatal(err)
	}
	if data, err = json.Marshal(doc); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := openVault(t, path, pass); err != nil {
		t.Fatalf("the added passphrase does not open the vault: %v", err)
	}
}

func openVault(t *testing.T, path, pass string) ([]account.Account, error) {
	t.Helper()
	env, err := store.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return env.Open(pass)
}

func TestVaultRotationReportsTheRemovedPassphrase(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"vault", "passwd"}, "Removed every other passphrase; only the new one opens the vault"},
		{[]string{"vault", "rekey"}, "Removed every other passphrase; only the one you entered opens the vault"},
	}
	for _, c := range cases {
		t.Run(c.args[1], func(t *testing.T) {
			path := withVault(t)
			sealVault(t, path, []account.Account{totp("a", "ACME", "x", 1)})
			addPassphraseWrap(t, path, "a-second-passphrase")
			swapStores(t)
			t.Setenv("TESSERA_NEW_PASSPHRASE", "brand-new-pass")

			stdout, _, err := runRootStreams(t, c.args...)
			if err != nil {
				t.Fatalf("%s: %v", c.args[1], err)
			}
			if !strings.Contains(stdout, c.want) {
				t.Errorf("stdout lacks %q:\n%s", c.want, stdout)
			}
			if _, err := openVault(t, path, "a-second-passphrase"); err == nil {
				t.Error("the second passphrase still opens the vault")
			}
			env, err := store.Load(path)
			if err != nil {
				t.Fatal(err)
			}
			if got := env.WrapTypes(); !slices.Equal(got, []string{"passphrase"}) {
				t.Errorf("wraps after %s = %v", c.args[1], got)
			}
		})
	}
}
