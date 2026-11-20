// Package store handles vault file location and atomic, owner-only-readable
// persistence of the encrypted envelope. The path is shared with the macOS app.
package store

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ibrahemid/tessera/go/internal/vault"
)

// ErrNotFound is returned when no vault exists at the resolved path.
var ErrNotFound = errors.New("store: no vault found (run `tess vault init`)")

// ErrNoBackup is returned by LoadBackup when the vault has no backup file.
var ErrNoBackup = errors.New("store: this vault has no backup file")

// InvalidPathError reports a vault path that contains a NUL, carriage return
// or line feed character.
type InvalidPathError struct {
	Path   string
	Source string
}

func (e *InvalidPathError) Error() string {
	return fmt.Sprintf("store: the vault path from %s (%q) contains a NUL, carriage return or line feed character; use a path without them", e.Source, e.Path)
}

// BackupAvailableError reports a vault file that cannot be parsed while a
// backup from before the last write exists. The backup is never used in its
// place without an explicit LoadBackup.
type BackupAvailableError struct {
	Path   string
	Backup string
	Err    error
}

func (e *BackupAvailableError) Error() string {
	return fmt.Sprintf("store: cannot read the vault at %s (%v); the vault as it was before the last write is at %s", e.Path, e.Err, e.Backup)
}

func (e *BackupAvailableError) Unwrap() error { return e.Err }

// MaxVaultSize is the largest vault file Load reads. No real vault comes close.
const MaxVaultSize int64 = 64 << 20

// TooLargeError reports a file over MaxVaultSize, refused before it is read.
type TooLargeError struct {
	Path  string
	Limit int64
}

func (e *TooLargeError) Error() string {
	return fmt.Sprintf("store: %s is larger than %d MiB, more than any vault holds; check that the path points at a Tessera vault", e.Path, e.Limit>>20)
}

// ExistsError reports that WriteSecretFile found a file, directory or symlink
// at the destination and was not asked to replace it.
type ExistsError struct {
	Path string
}

func (e *ExistsError) Error() string {
	return fmt.Sprintf("store: %s already exists; choose another path or allow replacing it", e.Path)
}

func (e *ExistsError) Unwrap() error { return fs.ErrExist }

// DanglingLinkError reports a vault path that is a symbolic link whose target
// does not exist.
type DanglingLinkError struct {
	Link   string
	Target string
}

func (e *DanglingLinkError) Error() string {
	return fmt.Sprintf("store: the vault path %s is a symbolic link to %s, which does not exist; restore that file or point the link at the vault", e.Link, e.Target)
}

// Resolve returns override if non-empty, else $TESSERA_VAULT, else DefaultPath,
// as an absolute, cleaned path. A path whose last element is a symbolic link
// resolves to the file the link points at, so every write lands next to the
// real file; a link to a missing file is a *DanglingLinkError.
func Resolve(override string) (string, error) {
	path, source := override, "--vault"
	if path == "" {
		path, source = os.Getenv("TESSERA_VAULT"), "TESSERA_VAULT"
	}
	if path == "" {
		p, err := DefaultPath()
		if err != nil {
			return "", err
		}
		path, source = p, "the default location"
	}
	if strings.ContainsAny(path, "\x00\r\n") {
		return "", &InvalidPathError{Path: path, Source: source}
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("store: make the vault path absolute: %w", err)
	}
	return resolveLink(abs)
}

const maxLinkHops = 255

func resolveLink(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&fs.ModeSymlink == 0 {
		return path, nil
	}
	target, err := filepath.EvalSymlinks(path)
	if err == nil {
		return filepath.Abs(target)
	}
	if errors.Is(err, fs.ErrNotExist) {
		return "", &DanglingLinkError{Link: path, Target: missingLinkTarget(path)}
	}
	return "", fmt.Errorf("store: follow the symbolic link at the vault path %s: %w", path, err)
}

func missingLinkTarget(link string) string {
	cur := link
	for range maxLinkHops {
		dest, err := os.Readlink(cur)
		if err != nil {
			return cur
		}
		if !filepath.IsAbs(dest) {
			dest = filepath.Join(filepath.Dir(cur), dest)
		}
		info, err := os.Lstat(dest)
		if err != nil || info.Mode()&fs.ModeSymlink == 0 {
			return dest
		}
		cur = dest
	}
	return cur
}

// BackupPath returns the backup file kept next to the vault at path.
func BackupPath(vaultPath string) string { return vaultPath + ".bak" }

// Exists reports whether a vault file exists at path.
func Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// Load reads and parses the envelope at path. A file over MaxVaultSize is
// refused with a *TooLargeError before it is read. When the file is too large
// or does not parse and a backup exists, the error is a *BackupAvailableError
// naming it.
func Load(path string) (*vault.Envelope, error) {
	data, err := readFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, ErrNotFound
		}
		var tooLarge *TooLargeError
		if errors.As(err, &tooLarge) {
			return nil, withBackup(path, err)
		}
		return nil, fmt.Errorf("store: read vault: %w", err)
	}
	env, err := vault.Unmarshal(data)
	if err != nil {
		return nil, withBackup(path, err)
	}
	return env, nil
}

func withBackup(path string, err error) error {
	bak := BackupPath(path)
	if _, statErr := os.Lstat(bak); statErr == nil {
		return &BackupAvailableError{Path: path, Backup: bak, Err: err}
	}
	return err
}

// HasBackup reports whether the vault at path has a backup file.
func HasBackup(path string) bool {
	_, err := os.Lstat(BackupPath(path))
	return err == nil
}

func readCappedFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() > MaxVaultSize {
		return nil, &TooLargeError{Path: path, Limit: MaxVaultSize}
	}
	data, err := io.ReadAll(io.LimitReader(f, MaxVaultSize+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > MaxVaultSize {
		return nil, &TooLargeError{Path: path, Limit: MaxVaultSize}
	}
	return data, nil
}

// LoadBackup reads and parses the backup of the vault at path.
func LoadBackup(path string) (*vault.Envelope, error) {
	bak := BackupPath(path)
	data, err := readFile(bak)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, ErrNoBackup
		}
		return nil, fmt.Errorf("store: read backup: %w", err)
	}
	env, err := vault.Unmarshal(data)
	if err != nil {
		return nil, fmt.Errorf("store: backup %s: %w", bak, err)
	}
	return env, nil
}

// Save writes the envelope atomically and durably with owner-only access,
// after copying the current vault file to BackupPath(path) when it parses. The
// directory is created owner-only when missing.
func Save(path string, env *vault.Envelope) error {
	data, err := env.Marshal()
	if err != nil {
		return err
	}
	if err := mkdirPrivate(filepath.Dir(path)); err != nil {
		return fmt.Errorf("store: create vault dir: %w", err)
	}
	removeStaleTemps(filepath.Dir(path), time.Now())
	if err := backupCurrent(path); err != nil {
		return err
	}
	return writeAtomic(path, data, true)
}

func backupCurrent(path string) error {
	data, err := readFile(path)
	var tooLarge *TooLargeError
	if errors.Is(err, fs.ErrNotExist) || errors.As(err, &tooLarge) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("store: read vault for backup: %w", err)
	}
	if _, err := vault.Unmarshal(data); err != nil {
		return nil
	}
	if err := writeAtomic(BackupPath(path), data, true); err != nil {
		return fmt.Errorf("store: write backup: %w", err)
	}
	return nil
}

// RemoveBackup deletes the backup of the vault at path and flushes the
// directory. A missing backup is not an error.
func RemoveBackup(path string) error {
	bak := BackupPath(path)
	if err := removeFile(bak); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("store: remove backup: %w", err)
	}
	if err := syncDir(filepath.Dir(bak)); err != nil {
		return fmt.Errorf("store: flush %s: %w", filepath.Dir(bak), err)
	}
	return nil
}

// MkdirPrivate creates dir and any missing parents owner-only, the way Save
// creates the vault directory: mode 0700 on Unix, a protected DACL inherited by
// everything inside it on Windows. Directories that already exist are left as
// they are.
func MkdirPrivate(dir string) error {
	return mkdirPrivate(dir)
}

// WriteSecretFile writes data to path as a regular owner-only file through an
// exclusive temporary file in the same directory, flushed and renamed into
// place. When something already exists at path it returns an *ExistsError
// unless replace is true. A symlink at path is never followed: it is replaced
// by the new file when replace is true.
func WriteSecretFile(path string, data []byte, replace bool) error {
	return writeAtomic(path, data, replace)
}

func writeAtomic(path string, data []byte, replace bool) error {
	dir := filepath.Dir(path)
	tmp, err := createTemp(dir)
	if err != nil {
		return fmt.Errorf("store: temp file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("store: write: %w", err)
	}
	if err := syncFile(tmp); err != nil {
		tmp.Close()
		return fmt.Errorf("store: flush %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("store: close: %w", err)
	}
	if replace {
		err = renameReplace(tmpName, path)
	} else {
		err = renameNoReplace(tmpName, path)
	}
	if err != nil {
		var exists *ExistsError
		if errors.As(err, &exists) {
			return err
		}
		return fmt.Errorf("store: rename into %s: %w", path, err)
	}
	if err := syncDir(dir); err != nil {
		return fmt.Errorf("store: flush %s: %w", dir, err)
	}
	return nil
}
