// Package vault implements the Tessera encrypted vault: envelope encryption with
// a random Data Encryption Key (DEK), the DEK wrapped per unlock method, and an
// XChaCha20-Poly1305 sealed payload. The format is specified in
// /spec/vault-format.md and is shared byte-for-byte with the Swift core.
package vault

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ibrahemid/tessera/go/internal/account"
	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/chacha20poly1305"
)

const (
	currentVersion = 1
	aeadName       = "xchacha20poly1305"
	kdfName        = "argon2id"
	dekLen         = 32
	saltLen        = 16
)

// Bounds on the argon2id costs read from a vault file. The file is untrusted
// input until the DEK is unwrapped, so costs that would panic x/crypto/argon2
// (t=0, p=0) or exhaust the machine (multi-GiB m, unbounded t) are rejected
// before any derivation runs.
const (
	minArgon2M uint32 = 8       // KiB; argon2 needs m >= 8*p
	maxArgon2M uint32 = 1 << 20 // KiB = 1 GiB
	maxArgon2T uint32 = 1 << 16
)

// DefaultArgon2 are the pinned argon2id parameters (128 MiB, t=3, p=4).
var DefaultArgon2 = Argon2Params{V: 1, M: 131072, T: 3, P: 4}

// Sentinel errors for negative-path handling.
var (
	ErrUnsupportedVersion = errors.New("vault: unsupported version")
	ErrUnsupportedAEAD    = errors.New("vault: unsupported aead")
	ErrNoPassphraseWrap   = errors.New("vault: no passphrase wrap present")
	ErrAppOnlyVault       = errors.New("vault: openable only by the Tessera app on the Mac that created it (Secure Enclave); use the app's Settings > Export encrypted backup for a file the CLI can open")
	ErrWrongPassphrase    = errors.New("vault: wrong passphrase or corrupt vault")
	ErrCorrupt            = errors.New("vault: corrupt or tampered vault")
	ErrNoWraps            = errors.New("vault: envelope has no wraps (spec/vault-format.md requires at least one)")
)

// Argon2Params are the versioned argon2id cost parameters. M is memory in KiB.
type Argon2Params struct {
	V int    `json:"v"`
	M uint32 `json:"m"`
	T uint32 `json:"t"`
	P uint8  `json:"p"`
}

// validate rejects costs a vault file must never carry.
func (p Argon2Params) validate() error {
	switch {
	case p.T < 1 || p.T > maxArgon2T:
		return fmt.Errorf("%w: argon2 t=%d out of range", ErrCorrupt, p.T)
	case p.P < 1:
		return fmt.Errorf("%w: argon2 p=%d out of range", ErrCorrupt, p.P)
	case p.M < minArgon2M || p.M > maxArgon2M:
		return fmt.Errorf("%w: argon2 m=%d KiB out of range", ErrCorrupt, p.M)
	case p.M < 8*uint32(p.P):
		return fmt.Errorf("%w: argon2 m=%d KiB below 8*p (p=%d)", ErrCorrupt, p.M, p.P)
	}
	return nil
}

// box is an XChaCha20-Poly1305 sealed blob (ct includes the 16-byte tag).
type box struct {
	Nonce []byte `json:"nonce"`
	CT    []byte `json:"ct"`
}

// wrap encrypts the DEK under one unlock method.
type wrap struct {
	Type   string        `json:"type"`
	KDF    string        `json:"kdf,omitempty"`
	Params *Argon2Params `json:"params,omitempty"`
	Salt   []byte        `json:"salt,omitempty"`
	SEKey  []byte        `json:"se_key,omitempty"`
	Nonce  []byte        `json:"nonce"`
	CT     []byte        `json:"ct"`
}

// Envelope is the on-disk vault structure.
type Envelope struct {
	Version int    `json:"version"`
	AEAD    string `json:"aead"`
	Wraps   []wrap `json:"wraps"`
	Payload box    `json:"payload"`

	// resealed records that a successful unwrap upgraded a passphrase wrap whose
	// params.v was below the current default (spec/vault-format.md § passphrase
	// wrap). The upgrade lives in memory until the caller writes the envelope.
	resealed bool
}

// NeedsPersist reports whether an in-memory wrap upgrade is waiting to be
// written to disk. A read-only vault simply never writes it; the next writable
// unlock redoes the upgrade.
func (e *Envelope) NeedsPersist() bool { return e.resealed }

func randBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return nil, fmt.Errorf("vault: rand: %w", err)
	}
	return b, nil
}

func seal(key, plaintext []byte) (box, error) {
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return box{}, err
	}
	nonce, err := randBytes(aead.NonceSize())
	if err != nil {
		return box{}, err
	}
	ct := aead.Seal(nil, nonce, plaintext, nil)
	return box{Nonce: nonce, CT: ct}, nil
}

func open(key []byte, b box) ([]byte, error) {
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, err
	}
	if len(b.Nonce) != aead.NonceSize() {
		return nil, ErrCorrupt
	}
	pt, err := aead.Open(nil, b.Nonce, b.CT, nil)
	if err != nil {
		return nil, ErrWrongPassphrase
	}
	return pt, nil
}

func derivePassphraseKey(passphrase string, salt []byte, p Argon2Params) []byte {
	return argon2.IDKey([]byte(passphrase), salt, p.T, p.M, p.P, dekLen)
}

// Seal creates a new envelope containing accounts, protected by a passphrase wrap.
func Seal(accounts []account.Account, passphrase string) (*Envelope, error) {
	for _, a := range accounts {
		if err := a.Validate(); err != nil {
			return nil, err
		}
	}
	dek, err := randBytes(dekLen)
	if err != nil {
		return nil, err
	}
	payloadPlain, err := account.CanonicalJSON(accounts)
	if err != nil {
		return nil, err
	}
	payload, err := seal(dek, payloadPlain)
	if err != nil {
		return nil, err
	}
	pw, err := newPassphraseWrap(dek, passphrase)
	if err != nil {
		return nil, err
	}
	return &Envelope{
		Version: currentVersion,
		AEAD:    aeadName,
		Wraps:   []wrap{pw},
		Payload: payload,
	}, nil
}

func newPassphraseWrap(dek []byte, passphrase string) (wrap, error) {
	salt, err := randBytes(saltLen)
	if err != nil {
		return wrap{}, err
	}
	params := DefaultArgon2
	key := derivePassphraseKey(passphrase, salt, params)
	b, err := seal(key, dek)
	if err != nil {
		return wrap{}, err
	}
	return wrap{
		Type: "passphrase", KDF: kdfName, Params: &params,
		Salt: salt, Nonce: b.Nonce, CT: b.CT,
	}, nil
}

// Open decrypts the envelope using a passphrase and returns the accounts.
func (e *Envelope) Open(passphrase string) ([]account.Account, error) {
	dek, err := e.UnwrapDEK(passphrase)
	if err != nil {
		return nil, err
	}
	return e.OpenWithDEK(dek)
}

// UnwrapDEK derives the wrap key from the passphrase and returns the Data
// Encryption Key. Callers that perform several payload operations in one run
// hold on to the DEK instead of paying for argon2id again.
func (e *Envelope) UnwrapDEK(passphrase string) ([]byte, error) {
	return e.unwrapWithPassphrase(passphrase)
}

// OpenWithDEK decrypts the payload with an already-unwrapped DEK.
func (e *Envelope) OpenWithDEK(dek []byte) ([]account.Account, error) {
	plain, err := open(dek, e.Payload)
	if err != nil {
		return nil, err
	}
	return account.ParseCanonicalJSON(plain)
}

func (e *Envelope) unwrapWithPassphrase(passphrase string) ([]byte, error) {
	if e.Version != currentVersion {
		return nil, ErrUnsupportedVersion
	}
	if e.AEAD != aeadName {
		return nil, ErrUnsupportedAEAD
	}
	found := false     // at least one wrap claims type "passphrase"
	attempted := false // at least one of them was well-formed enough to derive against
	var paramErr error
	for i := range e.Wraps {
		w := e.Wraps[i]
		if w.Type != "passphrase" {
			continue
		}
		found = true
		// An unknown kdf is not argon2id; deriving with argon2id anyway would
		// silently produce the wrong key instead of naming the real problem.
		if w.KDF != kdfName {
			if paramErr == nil {
				paramErr = fmt.Errorf("%w: unsupported passphrase kdf %q", ErrCorrupt, w.KDF)
			}
			continue
		}
		if w.Params == nil || len(w.Salt) != saltLen {
			continue
		}
		if err := w.Params.validate(); err != nil {
			if paramErr == nil {
				paramErr = err
			}
			continue
		}
		attempted = true
		key := derivePassphraseKey(passphrase, w.Salt, *w.Params)
		dek, err := open(key, box{Nonce: w.Nonce, CT: w.CT})
		if err != nil {
			continue
		}
		e.upgradeWrap(i, dek, passphrase)
		return dek, nil
	}
	if !found {
		for _, w := range e.Wraps {
			if w.Type == "secure-enclave" {
				return nil, ErrAppOnlyVault
			}
		}
		return nil, ErrNoPassphraseWrap
	}
	// A malformed wrap only explains the failure when no usable wrap was tried;
	// otherwise the passphrase itself is what did not work.
	if !attempted && paramErr != nil {
		return nil, paramErr
	}
	return nil, ErrWrongPassphrase
}

// upgradeWrap re-wraps the DEK at the current default cost when the wrap that
// just opened carries an older params.v (spec/vault-format.md § passphrase
// wrap). The payload is untouched; the caller persists on its next write.
func (e *Envelope) upgradeWrap(i int, dek []byte, passphrase string) {
	if e.Wraps[i].Params == nil || e.Wraps[i].Params.V >= DefaultArgon2.V {
		return
	}
	nw, err := newPassphraseWrap(dek, passphrase)
	if err != nil {
		return
	}
	e.Wraps[i] = nw
	e.resealed = true
}

// ChangePassphrase re-wraps the existing DEK under a new passphrase, leaving the
// payload untouched. Other wrap types (e.g. secure-enclave) are preserved.
func (e *Envelope) ChangePassphrase(old, new string) error {
	dek, err := e.unwrapWithPassphrase(old)
	if err != nil {
		return err
	}
	nw, err := newPassphraseWrap(dek, new)
	if err != nil {
		return err
	}
	out := nw.appendPreserving(e.Wraps)
	e.Wraps = out
	return nil
}

func (w wrap) appendPreserving(existing []wrap) []wrap {
	out := make([]wrap, 0, len(existing)+1)
	for _, e := range existing {
		if e.Type == "passphrase" {
			continue
		}
		out = append(out, e)
	}
	return append(out, w)
}

// UpdateAccounts re-seals the payload with the new account list, reusing the
// existing DEK (unwrapped via passphrase) so all wraps, including a Mac
// secure-enclave wrap, are preserved.
func (e *Envelope) UpdateAccounts(passphrase string, accounts []account.Account) error {
	dek, err := e.unwrapWithPassphrase(passphrase)
	if err != nil {
		return err
	}
	return e.UpdateAccountsWithDEK(dek, accounts)
}

// UpdateAccountsWithDEK re-seals the payload with an already-unwrapped DEK, so a
// command that mutates the vault derives argon2id once per run rather than once
// per write. Wraps are untouched.
func (e *Envelope) UpdateAccountsWithDEK(dek []byte, accounts []account.Account) error {
	for _, a := range accounts {
		if err := a.Validate(); err != nil {
			return err
		}
	}
	plain, err := account.CanonicalJSON(accounts)
	if err != nil {
		return err
	}
	b, err := seal(dek, plain)
	if err != nil {
		return err
	}
	e.Payload = b
	return nil
}

// WrapTypes returns the type of each wrap present in the envelope (e.g.
// "passphrase", "secure-enclave") without decrypting anything.
func (e *Envelope) WrapTypes() []string {
	types := make([]string, 0, len(e.Wraps))
	for _, w := range e.Wraps {
		types = append(types, w.Type)
	}
	return types
}

// Marshal serializes the envelope to JSON bytes for storage.
func (e *Envelope) Marshal() ([]byte, error) {
	return json.MarshalIndent(e, "", "  ")
}

// Unmarshal parses an envelope from JSON bytes.
func Unmarshal(data []byte) (*Envelope, error) {
	var e Envelope
	if err := json.Unmarshal(data, &e); err != nil {
		return nil, fmt.Errorf("vault: parse envelope: %w", err)
	}
	if e.Version != currentVersion {
		return nil, ErrUnsupportedVersion
	}
	if e.AEAD != aeadName {
		return nil, ErrUnsupportedAEAD
	}
	// spec/vault-format.md: wraps MUST be non-empty on disk; a zero-wrap file is
	// permanently unopenable, so reject it at the door instead of at unlock.
	if len(e.Wraps) == 0 {
		return nil, ErrNoWraps
	}
	return &e, nil
}
