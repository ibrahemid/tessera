package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ErrLocked reports that another process currently holds the vault lock.
var ErrLocked = errors.New("store: vault lock is held")

// Lock timing: the holder may be running argon2id or waiting on the user, so
// the wait is generous, but it is bounded: a wedged process must not block
// every other invocation forever.
const lockRetryInterval = 20 * time.Millisecond

// lockTimeout is a var only so the tests can shorten the give-up path.
var lockTimeout = 20 * time.Second

// Lock is an exclusive advisory lock over one vault path. It guards the whole
// load -> mutate -> save cycle: without it two concurrent `tess` runs both read
// the account list, and the second write silently drops the first one's edits.
type Lock struct {
	f *os.File
}

// LockPath returns the sidecar lock file guarding the vault at path. The vault
// file itself cannot be locked: it is replaced by rename on every save, so a
// lock on its inode would not be seen by the next writer.
func LockPath(vaultPath string) string { return vaultPath + ".lock" }

// TryAcquire takes the exclusive lock for the vault at path without waiting.
// It returns ErrLocked when another process holds it.
func TryAcquire(vaultPath string) (*Lock, error) {
	lp := LockPath(vaultPath)
	if err := os.MkdirAll(filepath.Dir(lp), 0o700); err != nil {
		return nil, fmt.Errorf("store: create vault dir: %w", err)
	}
	l, err := tryAcquire(lp)
	if err == nil {
		return l, nil
	}
	if errors.Is(err, ErrLocked) {
		return nil, ErrLocked
	}
	return nil, fmt.Errorf("store: lock %s: %w", lp, err)
}

// Acquire takes the exclusive lock for the vault at path, waiting while another
// process holds it and giving up at lockTimeout.
func Acquire(vaultPath string) (*Lock, error) {
	deadline := time.Now().Add(lockTimeout)
	for {
		l, err := TryAcquire(vaultPath)
		if err == nil {
			return l, nil
		}
		if !errors.Is(err, ErrLocked) {
			return nil, err
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("store: vault is in use by another tess process (waiting on %s)", LockPath(vaultPath))
		}
		time.Sleep(lockRetryInterval)
	}
}

// WithLock runs fn while holding the vault lock.
func WithLock(vaultPath string, fn func() error) error {
	l, err := Acquire(vaultPath)
	if err != nil {
		return err
	}
	defer l.Release()
	return fn()
}
