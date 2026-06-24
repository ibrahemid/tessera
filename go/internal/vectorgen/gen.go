// Command vectorgen produces the generated interop vectors (Steam codes,
// argon2id key, and a pinned vault envelope) that the Swift core must match.
// Output is printed as JSON snippets to paste into /spec/testvectors.json so the
// hand-authored RFC vectors and formatting are preserved.
package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/otp"
	"github.com/ibrahemid/tessera/go/internal/vault"
	"golang.org/x/crypto/argon2"
)

func main() {
	steamVectors()
	argon2Vectors()
	vaultVector()
}

func steamVectors() {
	secretRaw := []byte("12345678901234567890")
	secretB64 := base64.StdEncoding.EncodeToString(secretRaw)
	times := []int64{0, 1111111109, 1234567890, 2000000000}
	type c struct {
		SecretB64 string `json:"secret_b64"`
		Time      int64  `json:"time"`
		Code      string `json:"code"`
	}
	var cases []c
	for _, t := range times {
		code, err := otp.Steam(secretRaw, time.Unix(t, 0))
		if err != nil {
			panic(err)
		}
		cases = append(cases, c{SecretB64: secretB64, Time: t, Code: code})
	}
	emit("steam.cases", cases)
}

func argon2Vectors() {
	type c struct {
		PassphraseB64 string `json:"passphrase_b64"`
		SaltB64       string `json:"salt_b64"`
		KeyB64        string `json:"key_b64"`
	}
	pass := []byte("correct horse battery staple")
	salt := []byte("tessera-fixed-16")
	key := argon2.IDKey(pass, salt, 3, 131072, 4, 32)
	emit("argon2id.cases", []c{{
		PassphraseB64: base64.StdEncoding.EncodeToString(pass),
		SaltB64:       base64.StdEncoding.EncodeToString(salt),
		KeyB64:        base64.StdEncoding.EncodeToString(key),
	}})
}

func vaultVector() {
	accts := []account.Account{
		{ID: "00000000-0000-4000-8000-000000000001", Type: account.TOTP, Issuer: "ACME",
			Account: "alice@example.com", Secret: []byte("12345678901234567890"),
			Algorithm: "SHA1", Digits: 6, Period: 30, CreatedAt: 1700000000, UpdatedAt: 1700000000},
		{ID: "00000000-0000-4000-8000-000000000002", Type: account.Steam, Issuer: "Steam",
			Account: "gamer", Secret: []byte("abcdefghijklmnop"),
			Algorithm: "SHA1", Digits: 5, Period: 30, CreatedAt: 1700000000, UpdatedAt: 1700000000},
	}
	canonical, err := account.CanonicalJSON(accts)
	if err != nil {
		panic(err)
	}
	env, err := vault.Seal(accts, "correct horse battery staple")
	if err != nil {
		panic(err)
	}
	envJSON, err := env.Marshal()
	if err != nil {
		panic(err)
	}
	// Sanity: it must open with the right passphrase and not the wrong one.
	if _, err := env.Open("correct horse battery staple"); err != nil {
		panic("generated envelope failed to open: " + err.Error())
	}

	fmt.Println("\n=== vault_crossdecrypt.expected_canonical_json (json string literal) ===")
	b, _ := json.Marshal(string(canonical))
	fmt.Println(string(b))

	fmt.Println("\n=== vault_crossdecrypt.envelope (object) ===")
	fmt.Println(string(envJSON))
}

func emit(name string, v any) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		panic(err)
	}
	fmt.Printf("\n=== %s ===\n%s\n", name, b)
}
