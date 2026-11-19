package store

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/ibrahemid/tessera/go/internal/vault"
)

// BackupRetention is how many dated backup files a backup folder keeps.
const BackupRetention = 30

const backupDateLayout = "20060102"

var backupNamePattern = regexp.MustCompile(`^tessera-backup-[0-9]{8}\.json$`)

// ErrBackupNeedsPassphrase reports a vault with no passphrase wrap, which is
// never written to a backup folder.
var ErrBackupNeedsPassphrase = errors.New("store: the vault has no passphrase unlock; only a vault that opens with a passphrase is backed up")

// BackupDirProblem says why a backup folder cannot be used.
type BackupDirProblem int

const (
	BackupDirRelative BackupDirProblem = iota + 1
	BackupDirMissing
	BackupDirNotFolder
	BackupDirUnreadable
)

// BackupDirError reports a backup folder that cannot be used.
type BackupDirError struct {
	Dir     string
	Problem BackupDirProblem
	Err     error
}

func (e *BackupDirError) Error() string {
	switch e.Problem {
	case BackupDirRelative:
		return fmt.Sprintf("store: the backup folder %s is a relative path; use an absolute path", e.Dir)
	case BackupDirMissing:
		return fmt.Sprintf("store: the backup folder %s does not exist; create it or choose another folder", e.Dir)
	case BackupDirNotFolder:
		return fmt.Sprintf("store: the backup folder %s is not a folder; choose a folder", e.Dir)
	}
	return fmt.Sprintf("store: cannot open the backup folder %s: %v", e.Dir, e.Err)
}

func (e *BackupDirError) Unwrap() error { return e.Err }

// BackupPruneError reports older backup files that could not be deleted after
// a backup was written.
type BackupPruneError struct {
	Paths []string
	Err   error
}

func (e *BackupPruneError) Error() string {
	if len(e.Paths) == 1 {
		return fmt.Sprintf("store: could not delete the older backup %s (%v); delete it by hand", e.Paths[0], e.Err)
	}
	return fmt.Sprintf("store: could not delete the older backups %s (%v); delete them by hand", strings.Join(e.Paths, ", "), e.Err)
}

func (e *BackupPruneError) Unwrap() error { return e.Err }

// BackupResult describes a written backup file and the older ones deleted
// after it.
type BackupResult struct {
	Path    string
	Removed []string
}

// CheckBackupDir returns a *BackupDirError when dir is not an absolute path to
// an existing folder.
func CheckBackupDir(dir string) error {
	if !filepath.IsAbs(dir) {
		return &BackupDirError{Dir: dir, Problem: BackupDirRelative}
	}
	info, err := os.Stat(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return &BackupDirError{Dir: dir, Problem: BackupDirMissing, Err: err}
	case err != nil:
		return &BackupDirError{Dir: dir, Problem: BackupDirUnreadable, Err: err}
	case !info.IsDir():
		return &BackupDirError{Dir: dir, Problem: BackupDirNotFolder}
	}
	return nil
}

// BackupName is the backup file name for the UTC day of t.
func BackupName(t time.Time) string {
	return "tessera-backup-" + t.UTC().Format(backupDateLayout) + ".json"
}

// BackupDate returns the day a backup file name carries.
func BackupDate(name string) (time.Time, bool) {
	if !backupNamePattern.MatchString(name) {
		return time.Time{}, false
	}
	d, err := time.Parse(backupDateLayout, strings.TrimSuffix(strings.TrimPrefix(name, "tessera-backup-"), ".json"))
	return d, err == nil
}

// HasPassphraseWrap reports whether env carries a passphrase wrap.
func HasPassphraseWrap(env *vault.Envelope) bool {
	for _, w := range env.Wraps {
		if w.Type == "passphrase" {
			return true
		}
	}
	return false
}

// WriteBackup writes env, keeping only its passphrase wraps, to the day file
// for at in dir, replacing a file of the same day. It then keeps the
// BackupRetention newest files whose names match the backup pattern exactly,
// or only the day file when rotated is set, since older files open with the
// key the rotation replaced. When the day file is written but an older file
// cannot be deleted, the result is returned with a *BackupPruneError.
func WriteBackup(dir string, env *vault.Envelope, at time.Time, rotated bool) (*BackupResult, error) {
	if err := CheckBackupDir(dir); err != nil {
		return nil, err
	}
	if !HasPassphraseWrap(env) {
		return nil, ErrBackupNeedsPassphrase
	}
	cp := *env
	cp.Wraps = env.Wraps[:0:0]
	for _, w := range env.Wraps {
		if w.Type == "passphrase" {
			cp.Wraps = append(cp.Wraps, w)
		}
	}
	data, err := cp.Marshal()
	if err != nil {
		return nil, err
	}
	name := BackupName(at)
	path := filepath.Join(dir, name)
	if err := writeAtomic(path, data, true); err != nil {
		return nil, fmt.Errorf("store: could not write the backup %s: %w", path, err)
	}
	removed, err := pruneBackups(dir, name, rotated)
	return &BackupResult{Path: path, Removed: removed}, err
}

// ListBackups returns the names of the backup files in dir, newest first.
func ListBackups(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && backupNamePattern.MatchString(e.Name()) {
			names = append(names, e.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	return names, nil
}

func pruneBackups(dir, keep string, rotated bool) ([]string, error) {
	names, err := ListBackups(dir)
	if err != nil {
		return nil, fmt.Errorf("store: list the backups in %s to delete older ones: %w", dir, err)
	}
	var removed, failed []string
	var firstErr error
	kept := 1
	for _, name := range names {
		if name == keep {
			continue
		}
		if !rotated && kept < BackupRetention {
			kept++
			continue
		}
		path := filepath.Join(dir, name)
		if err := removeFile(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			failed = append(failed, path)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		removed = append(removed, path)
	}
	if len(failed) > 0 {
		return removed, &BackupPruneError{Paths: failed, Err: firstErr}
	}
	return removed, nil
}
