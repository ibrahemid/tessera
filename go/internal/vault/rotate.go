package vault

import (
	"slices"

	"github.com/ibrahemid/tessera/go/internal/account"
)

func (e *Envelope) Rotate(passphrase string, accounts []account.Account) ([]byte, []string, error) {
	if err := validateAccounts(accounts); err != nil {
		return nil, nil, err
	}
	plain, err := account.CanonicalJSON(accounts)
	if err != nil {
		return nil, nil, err
	}
	dek, err := randBytes(dekLen)
	if err != nil {
		return nil, nil, err
	}
	payload, err := seal(dek, plain)
	if err != nil {
		return nil, nil, err
	}
	pw, err := newPassphraseWrap(dek, passphrase)
	if err != nil {
		return nil, nil, err
	}
	removed := []string{}
	for _, w := range e.Wraps {
		if w.Type != passphraseType && !slices.Contains(removed, w.Type) {
			removed = append(removed, w.Type)
		}
	}
	e.Version = account.FormatVersion(accounts)
	e.AEAD = aeadName
	e.Wraps = []wrap{pw}
	e.Payload = payload
	e.resealed = false
	return dek, removed, nil
}

func (e *Envelope) RotatePassphrase(old, new string) ([]string, error) {
	dek, _, err := e.unwrapWithPassphrase(old)
	if err != nil {
		return nil, err
	}
	accounts, err := e.OpenWithDEK(dek)
	if err != nil {
		return nil, err
	}
	_, removed, err := e.Rotate(new, accounts)
	if err != nil {
		return nil, err
	}
	return removed, nil
}

func (e *Envelope) Rekey(passphrase string) ([]string, error) {
	return e.RotatePassphrase(passphrase, passphrase)
}
