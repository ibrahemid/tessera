//go:build linux

package secretstore

import (
	"context"
	"errors"
	"fmt"
	"math"
	"runtime"
	"time"

	"golang.org/x/sys/unix"
)

const (
	keyringKeyType       = "user"
	keyringDescPrefix    = "tessera:"
	keyringProbe         = keyringDescPrefix
	keyringBlocked       = "keyctl blocked (seccomp or kernel config)"
	keyringMaxDuplicates = 8

	keyPosAll    = 0x3f000000
	keyUsrView   = 0x00010000
	keyUsrRead   = 0x00020000
	keyUsrSearch = 0x00080000
	keyringPerm  = keyPosAll | keyUsrView | keyUsrRead | keyUsrSearch
)

var (
	errKeyringWrite     = errors.New("secretstore: could not save the passphrase in the kernel keyring; check the keyring with keyctl show @u and retry")
	errKeyringPersisted = errors.New("secretstore: the kernel keyring entry did not persist; check the keyring with keyctl show @u and retry")
	errKeyringRead      = errors.New("secretstore: could not read the kernel keyring entry; remember the passphrase again")
	errKeyringQuery     = errors.New("secretstore: could not search the kernel keyring; check the keyring with keyctl show @u and retry")
	errKeyringDelete    = errors.New("secretstore: could not delete the kernel keyring entry; remove it with keyctl unlink and retry")
	errKeyringQuota     = errors.New("secretstore: the kernel keyring quota is full; remove unused keys with keyctl or raise kernel.keys.maxbytes")
	errKeyringTTL       = errors.New("secretstore: the expiry must be positive and at most 136 years")
)

type keyctlCalls struct {
	addKey    func(keyType, description string, payload []byte, ringid int) (int, error)
	search    func(ringid int, keyType, description string, destRingid int) (int, error)
	read      func(id int, buf []byte) (int, error)
	setperm   func(id int, perm uint32) error
	keyctlInt func(cmd, arg2, arg3, arg4, arg5 int) (int, error)
}

var keyctl = keyctlCalls{
	addKey:    unix.AddKey,
	search:    unix.KeyctlSearch,
	read:      func(id int, buf []byte) (int, error) { return unix.KeyctlBuffer(unix.KEYCTL_READ, id, buf, 0) },
	setperm:   unix.KeyctlSetperm,
	keyctlInt: unix.KeyctlInt,
}

func init() {
	register(20, func() Backend { return kernelKeyring{} })
}

type kernelKeyring struct{}

func (kernelKeyring) ID() string   { return "kernel-keyring" }
func (kernelKeyring) Name() string { return "kernel keyring" }

func (k kernelKeyring) Available(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return &UnavailableError{Backend: k.Name(), Reason: err.Error()}
	}
	_, err := keyctl.search(unix.KEY_SPEC_USER_KEYRING, keyringKeyType, keyringProbe, 0)
	switch {
	case err == nil, keyGone(err):
		return nil
	case errors.Is(err, unix.EPERM), errors.Is(err, unix.ENOSYS), errors.Is(err, unix.EACCES):
		return &UnavailableError{Backend: k.Name(), Reason: keyringBlocked}
	}
	return &UnavailableError{Backend: k.Name(), Reason: "keyctl failed: " + err.Error()}
}

func (k kernelKeyring) Store(ctx context.Context, key, secret string, ttl time.Duration) error {
	if err := ValidateKey(key); err != nil {
		return err
	}
	if err := ValidateSecret(secret); err != nil {
		return err
	}
	seconds, err := keyringTimeout(ttl)
	if err != nil {
		return err
	}
	if err := k.check(ctx); err != nil {
		return err
	}
	if err := withUserKeyringPossessed(func() error { return k.write(key, secret, seconds) }); err != nil {
		return k.fail(err, errKeyringWrite)
	}
	got, ok, err := k.Lookup(ctx, key)
	if err != nil {
		return err
	}
	if !ok || got != secret {
		return errKeyringPersisted
	}
	return nil
}

func (k kernelKeyring) Lookup(ctx context.Context, key string) (string, bool, error) {
	if err := ValidateKey(key); err != nil {
		return "", false, err
	}
	if err := k.check(ctx); err != nil {
		return "", false, err
	}
	id, found, err := k.find(key)
	if err != nil || !found {
		return "", false, err
	}
	buf := make([]byte, maxSecretLen+1)
	defer clear(buf)
	n, err := keyctl.read(id, buf)
	switch {
	case keyGone(err):
		return "", false, nil
	case err != nil:
		return "", false, k.fail(err, errKeyringRead)
	case n > maxSecretLen:
		return "", false, fmt.Errorf("%w (the entry is longer than %d bytes)", errKeyringRead, maxSecretLen)
	}
	return string(buf[:n]), true, nil
}

func (k kernelKeyring) Has(ctx context.Context, key string) (bool, error) {
	if err := ValidateKey(key); err != nil {
		return false, err
	}
	if err := k.check(ctx); err != nil {
		return false, err
	}
	_, found, err := k.find(key)
	return found, err
}

func (k kernelKeyring) Delete(ctx context.Context, key string) error {
	if err := ValidateKey(key); err != nil {
		return err
	}
	for range keyringMaxDuplicates {
		if err := k.check(ctx); err != nil {
			return err
		}
		id, found, err := k.find(key)
		if err != nil {
			return err
		}
		if !found {
			return nil
		}
		if _, err := keyctl.keyctlInt(unix.KEYCTL_INVALIDATE, id, 0, 0, 0); err != nil && !keyGone(err) {
			return k.fail(err, errKeyringDelete)
		}
	}
	return errKeyringDelete
}

func (k kernelKeyring) find(key string) (int, bool, error) {
	id, err := keyctl.search(unix.KEY_SPEC_USER_KEYRING, keyringKeyType, keyringDescPrefix+key, 0)
	switch {
	case err == nil:
		return id, true, nil
	case keyGone(err):
		return 0, false, nil
	}
	return 0, false, k.fail(err, errKeyringQuery)
}

func (k kernelKeyring) write(key, secret string, seconds int) error {
	payload := []byte(secret)
	id, err := keyctl.addKey(keyringKeyType, keyringDescPrefix+key, payload, unix.KEY_SPEC_USER_KEYRING)
	clear(payload)
	if err != nil {
		return err
	}
	if err := keyctl.setperm(id, keyringPerm); err != nil {
		k.invalidate(id)
		return err
	}
	if _, err := keyctl.keyctlInt(unix.KEYCTL_SET_TIMEOUT, id, seconds, 0, 0); err != nil {
		k.invalidate(id)
		return err
	}
	return nil
}

func (k kernelKeyring) invalidate(id int) {
	_, _ = keyctl.keyctlInt(unix.KEYCTL_INVALIDATE, id, 0, 0, 0)
}

func withUserKeyringPossessed(fn func() error) error {
	done := make(chan error, 1)
	go func() {
		// SETPERM and SET_TIMEOUT need possession, which a session keyring without @u lacks; this never-unlocked thread links @u into its own keyring and exits with it.
		runtime.LockOSThread()
		if _, err := keyctl.keyctlInt(unix.KEYCTL_GET_KEYRING_ID, unix.KEY_SPEC_THREAD_KEYRING, 1, 0, 0); err != nil {
			done <- err
			return
		}
		if _, err := keyctl.keyctlInt(unix.KEYCTL_LINK, unix.KEY_SPEC_USER_KEYRING, unix.KEY_SPEC_THREAD_KEYRING, 0, 0); err != nil {
			done <- err
			return
		}
		done <- fn()
	}()
	return <-done
}

func (k kernelKeyring) check(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("secretstore: %s: %w", k.Name(), err)
	}
	return nil
}

func (k kernelKeyring) fail(err, sentinel error) error {
	switch {
	case errors.Is(err, unix.EPERM), errors.Is(err, unix.ENOSYS):
		return &UnavailableError{Backend: k.Name(), Reason: keyringBlocked}
	case errors.Is(err, unix.EDQUOT):
		return errKeyringQuota
	}
	return fmt.Errorf("%w (%v)", sentinel, err)
}

func keyringTimeout(ttl time.Duration) (int, error) {
	if ttl < 0 {
		return 0, errKeyringTTL
	}
	seconds := (ttl + time.Second - 1) / time.Second
	if seconds > math.MaxUint32 {
		return 0, errKeyringTTL
	}
	return int(seconds), nil
}

func keyGone(err error) bool {
	return errors.Is(err, unix.ENOKEY) || errors.Is(err, unix.EKEYEXPIRED) || errors.Is(err, unix.EKEYREVOKED)
}
