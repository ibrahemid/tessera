//go:build unix

package store

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

type tempFixture struct {
	name string
	make func(t *testing.T, path string)
	gone bool
}

func ageBy(t *testing.T, path string, d time.Duration) {
	t.Helper()
	old := time.Now().Add(-d)
	tv := []unix.Timeval{unix.NsecToTimeval(old.UnixNano()), unix.NsecToTimeval(old.UnixNano())}
	if err := unix.Lutimes(path, tv); err != nil {
		t.Fatal(err)
	}
}

func writeMode(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte("partial"), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func staleTempFixtures(target string) []tempFixture {
	return []tempFixture{
		{".tess-1001.tmp", func(t *testing.T, p string) { writeMode(t, p, 0o600); ageBy(t, p, 25*time.Hour) }, true},
		{".tess-1002.tmp", func(t *testing.T, p string) { writeMode(t, p, 0o600); ageBy(t, p, 23*time.Hour) }, false},
		{".tess-1003.tmp", func(t *testing.T, p string) { writeMode(t, p, 0o644); ageBy(t, p, 25*time.Hour) }, false},
		{".tess-1004.tmp", func(t *testing.T, p string) {
			if err := os.Symlink(target, p); err != nil {
				t.Fatal(err)
			}
			ageBy(t, p, 25*time.Hour)
		}, false},
		{".tess-1005.tmp", func(t *testing.T, p string) {
			if err := os.Mkdir(p, 0o700); err != nil {
				t.Fatal(err)
			}
			ageBy(t, p, 25*time.Hour)
		}, false},
		{".tess-abc.tmp", func(t *testing.T, p string) { writeMode(t, p, 0o600); ageBy(t, p, 25*time.Hour) }, false},
		{"notes.tmp", func(t *testing.T, p string) { writeMode(t, p, 0o600); ageBy(t, p, 25*time.Hour) }, false},
	}
}

func plantTemps(t *testing.T, dir string) []tempFixture {
	t.Helper()
	target := filepath.Join(t.TempDir(), "elsewhere")
	writeMode(t, target, 0o600)
	ageBy(t, target, 25*time.Hour)
	fixtures := staleTempFixtures(target)
	for _, f := range fixtures {
		f.make(t, filepath.Join(dir, f.name))
	}
	t.Cleanup(func() {
		if _, err := os.Stat(target); err != nil {
			t.Errorf("the link target outside the folder was removed: %v", err)
		}
	})
	return fixtures
}

func checkTemps(t *testing.T, dir string, fixtures []tempFixture) {
	t.Helper()
	for _, f := range fixtures {
		_, err := os.Lstat(filepath.Join(dir, f.name))
		if f.gone && err == nil {
			t.Errorf("%s was left behind", f.name)
		}
		if !f.gone && err != nil {
			t.Errorf("%s was removed: %v", f.name, err)
		}
	}
}

func TestSaveRemovesItsOwnStaleTempFiles(t *testing.T) {
	dir := t.TempDir()
	fixtures := plantTemps(t, dir)
	if err := Save(filepath.Join(dir, "vault.json"), sealOne(t, "x")); err != nil {
		t.Fatal(err)
	}
	checkTemps(t, dir, fixtures)
}

func TestBackupWriteRemovesItsOwnStaleTempFiles(t *testing.T) {
	dir := t.TempDir()
	fixtures := plantTemps(t, dir)
	if _, err := WriteBackup(dir, "", sealOne(t, "x"), time.Now(), false); err != nil {
		t.Fatal(err)
	}
	checkTemps(t, dir, fixtures)
	names := dirNames(t, dir)
	if !slices.Contains(names, BackupName(time.Now())) {
		t.Errorf("no backup written: %v", names)
	}
}
