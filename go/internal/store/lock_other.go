//go:build !unix

package store

import (
	"os"
)

// tryAcquire uses exclusive creation of the sidecar where flock is unavailable.
func tryAcquire(lockPath string) (*Lock, error) {
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		if os.IsExist(err) {
			return nil, ErrLocked
		}
		return nil, err
	}
	return &Lock{f: f}, nil
}

// Release closes and removes the sidecar, freeing it for the next waiter.
func (l *Lock) Release() error {
	if l == nil || l.f == nil {
		return nil
	}
	f := l.f
	l.f = nil
	name := f.Name()
	err := f.Close()
	if rerr := os.Remove(name); err == nil {
		err = rerr
	}
	return err
}
