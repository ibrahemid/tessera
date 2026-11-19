//go:build unix

package store

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"syscall"
)

// tryAcquire takes a non-blocking flock on the sidecar. flock is tied to the
// open file description, so two descriptors contend even inside one process,
// which is what makes the concurrency test meaningful.
func tryAcquire(lockPath string) (*Lock, error) {
	f, err := openLockFile(lockPath)
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

func openLockFile(lockPath string) (*os.File, error) {
	f, err := os.OpenFile(lockPath, os.O_RDWR|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o600)
	if err == nil {
		if err := f.Chmod(0o600); err != nil {
			f.Close()
			return nil, err
		}
		return f, nil
	}
	if !errors.Is(err, fs.ErrExist) {
		return nil, err
	}
	f, err = os.OpenFile(lockPath, os.O_RDWR|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, lockOpenError(lockPath, err)
	}
	if err := checkLockFile(f, lockPath); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

func checkLockFile(f *os.File, lockPath string) error {
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return &LockFileError{Path: lockPath, Reason: "is not a regular file"}
	}
	if st, ok := info.Sys().(*syscall.Stat_t); ok && int(st.Uid) != os.Geteuid() {
		return &LockFileError{Path: lockPath, Reason: "is owned by another user"}
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		return &LockFileError{Path: lockPath, Reason: fmt.Sprintf("has mode %04o, not 0600", perm)}
	}
	return nil
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
