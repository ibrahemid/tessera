//go:build !unix && !windows

package store

import (
	"errors"
	"io/fs"
	"os"
)

func syncFile(f *os.File) error { return f.Sync() }

func syncDir(string) error { return nil }

func renameNoReplace(from, to string) error {
	if _, err := os.Lstat(to); err == nil {
		return &ExistsError{Path: to}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return os.Rename(from, to)
}
