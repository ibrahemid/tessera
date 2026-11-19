package screenshot

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

type fakeInfo struct {
	mode  fs.FileMode
	owner int
}

func (f fakeInfo) Name() string       { return "run" }
func (f fakeInfo) Size() int64        { return 0 }
func (f fakeInfo) Mode() fs.FileMode  { return f.mode }
func (f fakeInfo) ModTime() time.Time { return time.Time{} }
func (f fakeInfo) IsDir() bool        { return f.mode.IsDir() }
func (f fakeInfo) Sys() any           { return f.owner }

func TestRuntimeDir(t *testing.T) {
	const uid = 1000
	const dir = "/run/user/1000"
	ownerOf := func(fi fs.FileInfo) (int, bool) {
		owner, ok := fi.Sys().(int)
		return owner, ok && owner >= 0
	}
	cases := []struct {
		name string
		dir  string
		info fs.FileInfo
		err  error
		want string
	}{
		{"private directory", dir, fakeInfo{fs.ModeDir | 0o700, uid}, nil, dir},
		{"unset", "", fakeInfo{fs.ModeDir | 0o700, uid}, nil, ""},
		{"relative", "run/user/1000", fakeInfo{fs.ModeDir | 0o700, uid}, nil, ""},
		{"missing", dir, nil, fs.ErrNotExist, ""},
		{"regular file", dir, fakeInfo{0o700, uid}, nil, ""},
		{"symlink", dir, fakeInfo{fs.ModeSymlink | 0o700, uid}, nil, ""},
		{"group writable", dir, fakeInfo{fs.ModeDir | 0o720, uid}, nil, ""},
		{"group readable", dir, fakeInfo{fs.ModeDir | 0o750, uid}, nil, ""},
		{"world writable", dir, fakeInfo{fs.ModeDir | 0o707, uid}, nil, ""},
		{"sticky shared", dir, fakeInfo{fs.ModeDir | fs.ModeSticky | 0o777, uid}, nil, ""},
		{"foreign owner", dir, fakeInfo{fs.ModeDir | 0o700, 0}, nil, ""},
		{"owner unknown", dir, fakeInfo{fs.ModeDir | 0o700, -1}, nil, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			lstat := func(p string) (fs.FileInfo, error) {
				if p != c.dir {
					t.Fatalf("lstat(%q), want %q", p, c.dir)
				}
				return c.info, c.err
			}
			if got := runtimeDir(c.dir, uid, lstat, ownerOf); got != c.want {
				t.Fatalf("runtimeDir = %q, want %q", got, c.want)
			}
		})
	}
}

func scratchIn(t *testing.T, want string) {
	t.Helper()
	dir, err := ScratchDir()
	if err != nil {
		t.Fatalf("ScratchDir: %v", err)
	}
	defer os.RemoveAll(dir)
	if filepath.Dir(dir) != want {
		t.Fatalf("ScratchDir = %s, want a directory in %s", dir, want)
	}
	if !strings.HasPrefix(filepath.Base(dir), scratchPrefix) {
		t.Fatalf("ScratchDir = %s, want the %s prefix", dir, scratchPrefix)
	}
	fi, err := os.Lstat(dir)
	if err != nil || !fi.IsDir() {
		t.Fatalf("ScratchDir left %s as %v, %v", dir, fi, err)
	}
	if runtime.GOOS != "windows" && fi.Mode().Perm() != 0o700 {
		t.Fatalf("ScratchDir mode = %o, want 700", fi.Mode().Perm())
	}
}

func setTempDir(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	if runtime.GOOS == "windows" {
		t.Setenv("TMP", tmp)
		t.Setenv("TEMP", tmp)
	} else {
		t.Setenv("TMPDIR", tmp)
	}
	return tmp
}

func TestScratchDirIsFreshAndPrivate(t *testing.T) {
	tmp := setTempDir(t)
	t.Setenv("XDG_RUNTIME_DIR", "")
	scratchIn(t, tmp)

	a, err := ScratchDir()
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(a)
	b, err := ScratchDir()
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(b)
	if a == b {
		t.Fatalf("ScratchDir returned %s twice", a)
	}
}

func TestScratchDirRuntimeDir(t *testing.T) {
	tmp := setTempDir(t)
	run := filepath.Join(t.TempDir(), "run")
	if err := os.Mkdir(run, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(run, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_RUNTIME_DIR", run)
	if runtime.GOOS != "linux" {
		scratchIn(t, tmp)
		return
	}
	scratchIn(t, run)

	t.Run("group writable", func(t *testing.T) {
		if err := os.Chmod(run, 0o770); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(run, 0o700) })
		scratchIn(t, tmp)
	})
	t.Run("relative", func(t *testing.T) {
		t.Chdir(filepath.Dir(run))
		t.Setenv("XDG_RUNTIME_DIR", filepath.Base(run))
		scratchIn(t, tmp)
	})
	t.Run("symlink", func(t *testing.T) {
		link := filepath.Join(t.TempDir(), "link")
		if err := os.Symlink(run, link); err != nil {
			t.Fatal(err)
		}
		t.Setenv("XDG_RUNTIME_DIR", link)
		scratchIn(t, tmp)
	})
	t.Run("foreign owner", func(t *testing.T) {
		if os.Getuid() != 0 {
			t.Skip("changing a directory's owner needs root")
		}
		foreign := filepath.Join(t.TempDir(), "foreign")
		if err := os.Mkdir(foreign, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chown(foreign, 65534, 65534); err != nil {
			t.Fatal(err)
		}
		t.Setenv("XDG_RUNTIME_DIR", foreign)
		scratchIn(t, tmp)
	})
}

func TestScratchDirReportsCreateFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("TMPDIR does not steer os.TempDir on Windows")
	}
	t.Setenv("XDG_RUNTIME_DIR", "")
	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "absent"))
	_, err := ScratchDir()
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("ScratchDir = %v, want a wrapped fs.ErrNotExist", err)
	}
}
