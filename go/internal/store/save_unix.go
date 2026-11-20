//go:build unix

package store

import (
	"errors"
	"io/fs"
	"os"
	"runtime"
	"syscall"
)

func syncFile(f *os.File) error {
	err := f.Sync()
	if runtime.GOOS == "darwin" && isUnsupported(err) {
		// Some filesystems (network shares, exFAT) reject F_FULLFSYNC; fsync is the strongest flush they offer.
		return syscall.Fsync(int(f.Fd()))
	}
	return err
}

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	if err := syncFile(d); err != nil && !isUnsupported(err) {
		return err
	}
	return nil
}

func isUnsupported(err error) bool {
	return errors.Is(err, syscall.EINVAL) || errors.Is(err, syscall.ENOTSUP) || errors.Is(err, syscall.EOPNOTSUPP)
}

// link(2) fails with EEXIST atomically, even over a dangling symlink; filesystems without hard links fall back to a check.
func renameNoReplace(from, to string) error {
	err := os.Link(from, to)
	if err == nil {
		_ = os.Remove(from)
		return nil
	}
	if errors.Is(err, fs.ErrExist) {
		return &ExistsError{Path: to}
	}
	if !errors.Is(err, syscall.EPERM) && !errors.Is(err, syscall.ENOSYS) && !isUnsupported(err) {
		return err
	}
	if _, err := os.Lstat(to); err == nil {
		return &ExistsError{Path: to}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return os.Rename(from, to)
}
