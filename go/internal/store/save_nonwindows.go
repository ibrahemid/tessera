//go:build !windows

package store

import (
	"os"
)

func mkdirPrivate(dir string) error { return os.MkdirAll(dir, 0o700) }

func readFile(path string) ([]byte, error) { return readCappedFile(path) }

func removeFile(path string) error { return os.Remove(path) }

func renameReplace(from, to string) error { return os.Rename(from, to) }

func createTemp(dir string) (*os.File, error) {
	f, err := os.CreateTemp(dir, ".tess-*.tmp")
	if err != nil {
		return nil, err
	}
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		os.Remove(f.Name())
		return nil, err
	}
	return f, nil
}
