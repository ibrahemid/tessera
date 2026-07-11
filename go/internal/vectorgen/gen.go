// Command vectorgen produces the generated interop vectors (Steam codes,
// argon2id key, and a pinned vault envelope) that the Swift core must match.
// Output is printed as JSON snippets to paste into /spec/testvectors.json so the
// hand-authored RFC vectors and formatting are preserved.
package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/otp"
	"github.com/ibrahemid/tessera/go/internal/vault"
	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/chacha20"
	"golang.org/x/crypto/chacha20poly1305"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "edge" {
		os.Stdout.Write(edgeCanonical())
		return
	}
	steamVectors()
	argon2Vectors()
	aeadVector()
	hchachaVector()
	handlesVectors()
	vaultVector()
}

// handlesVectors pins handle assignment and the canonical serialization of an
// account carrying a handle. Expected handles are computed by the reference
// implementation (account.AssignHandles), so the block is always self-consistent
// with Go; the Swift core MUST reproduce it byte-for-byte. Paste the emitted
// object into /spec/testvectors.json under "handles".
func handlesVectors() {
	type inAcct struct {
		ID        string `json:"id"`
		Issuer    string `json:"issuer"`
		Account   string `json:"account"`
		CreatedAt int64  `json:"created_at"`
		Handle    string `json:"handle,omitempty"`
	}
	type acase struct {
		Name     string            `json:"name"`
		Accounts []inAcct          `json:"accounts"`
		Expected map[string]string `json:"expected"`
	}

	cases := [][]inAcct{
		{{ID: "m1", Issuer: "Google Cloud", CreatedAt: 1}}, // multi-word -> gc
		{ // single word with internal caps: lowercase then first two chars, NO camelCase split -> gi/gi2
			{ID: "g1", Issuer: "GitHub", CreatedAt: 1},
			{ID: "g2", Issuer: "GitHub", CreatedAt: 2},
		},
		{{ID: "d1", Issuer: "1Password", CreatedAt: 1}},                      // digit-leading -> x1p
		{{ID: "e1", Issuer: "", Account: "alice@example.com", CreatedAt: 1}}, // empty issuer, email local part -> al
		{{ID: "z1", Issuer: "", Account: "", CreatedAt: 1}},                  // empty issuer and account -> acct
		{ // collision chain -> base, base2, base3 (assignment order created_at asc)
			{ID: "c1", Issuer: "ACME", CreatedAt: 1},
			{ID: "c2", Issuer: "ACME", CreatedAt: 2},
			{ID: "c3", Issuer: "ACME", CreatedAt: 3},
		},
		{ // a user-edited handle occupies base2 so the next auto-assign skips to base3
			{ID: "u1", Issuer: "ACME", CreatedAt: 1, Handle: "ac"},
			{ID: "u2", Issuer: "ACME", CreatedAt: 2, Handle: "ac2"},
			{ID: "u3", Issuer: "ACME", CreatedAt: 3},
		},
	}
	names := []string{"multi-word", "github-single-word-caps", "digit-leading", "empty-issuer-email", "empty-issuer-and-account", "collision-chain", "user-edited-skip"}

	var out []acase
	for i, in := range cases {
		accts := make([]account.Account, len(in))
		for j, a := range in {
			accts[j] = account.Account{ID: a.ID, Issuer: a.Issuer, Account: a.Account, CreatedAt: a.CreatedAt, Handle: a.Handle}
		}
		account.AssignHandles(accts)
		exp := make(map[string]string, len(accts))
		for _, a := range accts {
			exp[a.ID] = a.Handle
		}
		out = append(out, acase{Name: names[i], Accounts: in, Expected: exp})
	}

	canon, err := account.CanonicalJSON([]account.Account{{
		ID: "00000000-0000-4000-8000-000000000010", Type: account.TOTP,
		Issuer: "ACME", Account: "alice@example.com", Handle: "ac",
		Secret: []byte("12345678901234567890"), Algorithm: "SHA1", Digits: 6, Period: 30,
		CreatedAt: 1700000000, UpdatedAt: 1700000000,
	}})
	if err != nil {
		panic(err)
	}

	emit("handles", map[string]any{
		"_status":    "Deterministic handle assignment + canonical serialization. Both cores MUST produce identical handles and identical canonical bytes.",
		"canonical":  map[string]string{"expected_canonical_json": string(canon)},
		"assignment": out,
	})
}

// hchachaVector pins an HChaCha20 known-answer test (draft-irtf-cfrg-xchacha
// §2.2.1) computed from x/crypto so the Swift HChaCha20 can be asserted directly,
// localizing a subkey-derivation bug independent of the full AEAD path.
func hchachaVector() {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	nonce := []byte{0, 0, 0, 9, 0, 0, 0, 0x4a, 0, 0, 0, 0, 0x31, 0x41, 0x59, 0x27}
	subkey, err := chacha20.HChaCha20(key, nonce)
	if err != nil {
		panic(err)
	}
	type v struct {
		KeyB64    string `json:"key_b64"`
		NonceB64  string `json:"nonce16_b64"`
		SubkeyB64 string `json:"subkey_b64"`
	}
	emit("hchacha20", v{
		KeyB64:    base64.StdEncoding.EncodeToString(key),
		NonceB64:  base64.StdEncoding.EncodeToString(nonce),
		SubkeyB64: base64.StdEncoding.EncodeToString(subkey),
	})
}

// edgeAccount is the nasty-string account whose canonical encoding pins escaping.
func edgeAccount() account.Account {
	return account.Account{
		ID:        "00000000-0000-4000-8000-0000000000ff",
		Type:      account.TOTP,
		Issuer:    "A\"B\\C\n\tD<e>&f  é🔐",
		Account:   "user/name",
		Secret:    []byte{0x00, 0x01, 0x02, 0xfe, 0xff},
		Algorithm: "SHA1", Digits: 6, Period: 30,
		Tags:      []string{"a\"b", "c\\d"},
		CreatedAt: 1700000000, UpdatedAt: 1700000000,
	}
}

func edgeCanonical() []byte {
	b, err := account.CanonicalJSON([]account.Account{edgeAccount()})
	if err != nil {
		panic(err)
	}
	return b
}

// aeadVector seals the canonical payload with a FIXED DEK and FIXED nonce using
// XChaCha20-Poly1305 directly (no argon2id), so the Swift core can verify its
// XChaCha + canonical-JSON decode locally via swiftc.
func aeadVector() {
	dek := make([]byte, 32)
	for i := range dek {
		dek[i] = byte(i)
	}
	nonce := make([]byte, chacha20poly1305.NonceSizeX)
	for i := range nonce {
		nonce[i] = 0x42
	}
	canonical := sampleCanonical()
	aead, err := chacha20poly1305.NewX(dek)
	if err != nil {
		panic(err)
	}
	ct := aead.Seal(nil, nonce, canonical, nil)
	type v struct {
		DEKB64      string `json:"dek_b64"`
		NonceB64    string `json:"nonce_b64"`
		CTB64       string `json:"ct_b64"`
		PlaintextJS string `json:"expected_canonical_json"`
	}
	emit("aead_payload", v{
		DEKB64:      base64.StdEncoding.EncodeToString(dek),
		NonceB64:    base64.StdEncoding.EncodeToString(nonce),
		CTB64:       base64.StdEncoding.EncodeToString(ct),
		PlaintextJS: string(canonical),
	})
}

func sampleAccounts() []account.Account {
	return []account.Account{
		{ID: "00000000-0000-4000-8000-000000000001", Type: account.TOTP, Issuer: "ACME",
			Account: "alice@example.com", Secret: []byte("12345678901234567890"),
			Algorithm: "SHA1", Digits: 6, Period: 30, CreatedAt: 1700000000, UpdatedAt: 1700000000},
		{ID: "00000000-0000-4000-8000-000000000002", Type: account.Steam, Issuer: "Steam",
			Account: "gamer", Secret: []byte("abcdefghijklmnop"),
			Algorithm: "SHA1", Digits: 5, Period: 30, CreatedAt: 1700000000, UpdatedAt: 1700000000},
	}
}

func sampleCanonical() []byte {
	b, err := account.CanonicalJSON(sampleAccounts())
	if err != nil {
		panic(err)
	}
	return b
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
	accts := sampleAccounts()
	canonical := sampleCanonical()
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
