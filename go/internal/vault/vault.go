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
	"unicode/utf8"

	"github.com/ibrahemid/tessera/go/internal/account"
	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/chacha20poly1305"
)

const (
	aeadName              = "xchacha20poly1305"
	kdfName               = "argon2id"
	passphraseType        = "passphrase"
	secureEnclaveType     = "secure-enclave"
	dekLen                = 32
	saltLen               = 16
	nonceLen              = chacha20poly1305.NonceSizeX
	maxWraps              = 16
	maxPassphraseAttempts = 4
)

// Bounds on the argon2id costs read from a vault file. The file is untrusted
// input until the DEK is unwrapped, so costs that would panic x/crypto/argon2
// (t=0, p=0) or exhaust the machine (multi-GiB m, unbounded m*t) are rejected
// before any derivation runs.
const (
	minArgon2M    uint32 = 8       // KiB; argon2 needs m >= 8*p
	maxArgon2M    uint32 = 1 << 20 // KiB = 1 GiB
	maxArgon2P    uint8  = 16
	maxArgon2Work uint64 = 4194304
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
	ErrTooManyWraps       = errors.New("vault: envelope has more than 16 wraps")
	ErrPassphraseEncoding = errors.New("vault: the new passphrase is not valid UTF-8; enter it again as UTF-8 text")
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
	case p.T < 1:
		return corruptf("argon2 t=%d out of range", p.T)
	case p.P < 1 || p.P > maxArgon2P:
		return corruptf("argon2 p=%d out of range", p.P)
	case p.M < minArgon2M || p.M > maxArgon2M:
		return corruptf("argon2 m=%d KiB out of range", p.M)
	case p.M < 8*uint32(p.P):
		return corruptf("argon2 m=%d KiB below 8*p (p=%d)", p.M, p.P)
	case uint64(p.M)*uint64(p.T) > maxArgon2Work:
		return corruptf("argon2 m*t=%d exceeds %d", uint64(p.M)*uint64(p.T), maxArgon2Work)
	}
	return nil
}

func (p Argon2Params) belowDefault() bool {
	return p.M < DefaultArgon2.M || p.T < DefaultArgon2.T || p.P < DefaultArgon2.P
}

// box is an XChaCha20-Poly1305 sealed blob (ct includes the 16-byte tag).
type box struct {
	Nonce []byte
	CT    []byte
}

// wrap encrypts the DEK under one unlock method.
type wrap struct {
	Type   string
	KDF    string
	Params *Argon2Params
	Salt   []byte
	SEKey  []byte
	Nonce  []byte
	CT     []byte

	raw       json.RawMessage
	paramsErr error
}

// Envelope is the on-disk vault structure.
type Envelope struct {
	Version int
	AEAD    string
	Wraps   []wrap
	Payload box

	// resealed records that a successful unwrap upgraded a passphrase wrap whose
	// cost was below the current default (spec/vault-format.md § passphrase
	// wrap). The upgrade lives in memory until the caller writes the envelope.
	resealed bool
	extra    map[string]json.RawMessage
}

// NeedsPersist reports whether an in-memory wrap upgrade is waiting to be
// written to disk. A read-only vault simply never writes it; the next writable
// unlock redoes the upgrade.
func (e *Envelope) NeedsPersist() bool { return e.resealed }

func corruptf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrCorrupt, fmt.Sprintf(format, args...))
}

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

func validateAccounts(accounts []account.Account) error {
	for _, a := range accounts {
		if err := a.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func checkVersion(v int) error {
	if v != 1 && v != 2 {
		return ErrUnsupportedVersion
	}
	return nil
}

// Seal creates a new envelope containing accounts, protected by a passphrase wrap.
func Seal(accounts []account.Account, passphrase string) (*Envelope, error) {
	if err := validateAccounts(accounts); err != nil {
		return nil, err
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
		Version: account.FormatVersion(accounts),
		AEAD:    aeadName,
		Wraps:   []wrap{pw},
		Payload: payload,
	}, nil
}

func newPassphraseWrap(dek []byte, passphrase string) (wrap, error) {
	if !utf8.ValidString(passphrase) {
		return wrap{}, ErrPassphraseEncoding
	}
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
		Type: passphraseType, KDF: kdfName, Params: &params,
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
	dek, i, err := e.unwrapWithPassphrase(passphrase)
	if err != nil {
		return nil, err
	}
	e.upgradeWrap(i, dek, passphrase)
	return dek, nil
}

// OpenWithDEK decrypts the payload with an already-unwrapped DEK.
func (e *Envelope) OpenWithDEK(dek []byte) ([]account.Account, error) {
	plain, err := open(dek, e.Payload)
	if err != nil {
		return nil, err
	}
	return account.ParseCanonicalJSON(plain)
}

func (w *wrap) passphraseCheck() error {
	switch {
	case w.KDF != kdfName:
		return corruptf("unsupported passphrase kdf %q", w.KDF)
	case len(w.Salt) != saltLen:
		return corruptf("passphrase salt is %d bytes, want %d", len(w.Salt), saltLen)
	case w.paramsErr != nil:
		return w.paramsErr
	case w.Params == nil:
		return corruptf("passphrase wrap has no argon2 params")
	}
	if err := w.Params.validate(); err != nil {
		return err
	}
	if len(w.Nonce) != nonceLen {
		return corruptf("wrap nonce is %d bytes, want %d", len(w.Nonce), nonceLen)
	}
	return nil
}

func (e *Envelope) unwrapWithPassphrase(passphrase string) ([]byte, int, error) {
	if err := checkVersion(e.Version); err != nil {
		return nil, -1, err
	}
	if e.AEAD != aeadName {
		return nil, -1, ErrUnsupportedAEAD
	}
	found := false
	attempts := 0
	var checkErr error
	for i := range e.Wraps {
		w := &e.Wraps[i]
		if w.Type != passphraseType {
			continue
		}
		found = true
		if err := w.passphraseCheck(); err != nil {
			if checkErr == nil {
				checkErr = err
			}
			continue
		}
		if attempts == maxPassphraseAttempts {
			break
		}
		attempts++
		key := derivePassphraseKey(passphrase, w.Salt, *w.Params)
		dek, err := open(key, box{Nonce: w.Nonce, CT: w.CT})
		if err != nil {
			continue
		}
		return dek, i, nil
	}
	if !found {
		for _, w := range e.Wraps {
			if w.Type == secureEnclaveType {
				return nil, -1, ErrAppOnlyVault
			}
		}
		return nil, -1, ErrNoPassphraseWrap
	}
	if attempts == 0 && checkErr != nil {
		return nil, -1, checkErr
	}
	return nil, -1, ErrWrongPassphrase
}

// upgradeWrap re-wraps the DEK at the current default cost when the wrap that
// just opened has m, t or p below it (spec/vault-format.md § passphrase wrap).
// The payload is untouched; the caller persists on its next write.
func (e *Envelope) upgradeWrap(i int, dek []byte, passphrase string) {
	if e.Wraps[i].Params == nil || !e.Wraps[i].Params.belowDefault() {
		return
	}
	nw, err := newPassphraseWrap(dek, passphrase)
	if err != nil {
		return
	}
	e.Wraps[i] = nw
	e.resealed = true
}

// ChangePassphrase rotates the DEK under a new passphrase (see RotatePassphrase).
func (e *Envelope) ChangePassphrase(old, new string) error {
	_, err := e.RotatePassphrase(old, new)
	return err
}

// UpdateAccounts re-seals the payload with the new account list, reusing the
// existing DEK (unwrapped via passphrase) so all wraps, including a Mac
// secure-enclave wrap, are preserved.
func (e *Envelope) UpdateAccounts(passphrase string, accounts []account.Account) error {
	dek, err := e.UnwrapDEK(passphrase)
	if err != nil {
		return err
	}
	return e.UpdateAccountsWithDEK(dek, accounts)
}

// UpdateAccountsWithDEK re-seals the payload with an already-unwrapped DEK, so a
// command that mutates the vault derives argon2id once per run rather than once
// per write. Wraps are untouched.
func (e *Envelope) UpdateAccountsWithDEK(dek []byte, accounts []account.Account) error {
	if err := validateAccounts(accounts); err != nil {
		return err
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
	e.Version = account.FormatVersion(accounts)
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
