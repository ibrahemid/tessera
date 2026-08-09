//go:build unix

package store

import (
	"errors"
	"os"
	"syscall"
)

// tryAcquire takes a non-blocking flock on the sidecar. flock is tied to the
// open file description, so two descriptors contend even inside one process,
// which is what makes the concurrency test meaningful.
func tryAcquire(lockPath string) (*Lock, error) {
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrLocked
		}
		return nil, err
	}
	return &Lock{f: f}, nil
}

// Release drops the lock. The sidecar file is never unlinked: another process
// may already hold it open and be retrying, and removing it would let a third
// process create a fresh file and lock that instead.
func (l *Lock) Release() error {
	if l == nil || l.f == nil {
		return nil
	}
	f := l.f
	l.f = nil
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return f.Close()
}
