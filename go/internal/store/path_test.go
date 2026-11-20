package store

import (
	"errors"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestResolvePrecedence(t *testing.T) {
	dir := t.TempDir()
	override := filepath.Join(dir, "override.json")
	env := filepath.Join(dir, "env", "path.json")
	t.Setenv("TESSERA_VAULT", env)
	if p, err := Resolve(override); err != nil || p != override {
		t.Errorf("Resolve(override) = %q, %v; want %q", p, err, override)
	}
	if p, err := Resolve(""); err != nil || p != env {
		t.Errorf("Resolve(\"\") = %q, %v; want %q", p, err, env)
	}
	t.Setenv("TESSERA_VAULT", "")
	t.Setenv("XDG_DATA_HOME", dir)
	want := filepath.Join(dir, "tessera", "vault.json")
	if p, err := Resolve(""); err != nil || p != want {
		t.Errorf("Resolve(\"\") without env = %q, %v; want %q", p, err, want)
	}
}

func TestResolveReturnsAbsoluteCleanedPaths(t *testing.T) {
	cwd := t.TempDir()
	t.Chdir(cwd)
	cwd, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(cwd, "rel", "vault.json")
	relative := filepath.Join("rel", "x", "..", ".", "vault.json")

	if p, err := Resolve(relative); err != nil || p != want {
		t.Errorf("Resolve(%q) = %q, %v; want %q", relative, p, err, want)
	}
	t.Setenv("TESSERA_VAULT", relative)
	if p, err := Resolve(""); err != nil || p != want {
		t.Errorf("Resolve with TESSERA_VAULT=%q = %q, %v; want %q", relative, p, err, want)
	}
	t.Setenv("TESSERA_VAULT", "")
	sep := string(filepath.Separator)
	t.Setenv("XDG_DATA_HOME", cwd+sep+"data"+sep+"..")
	if p, err := Resolve(""); err != nil || p != filepath.Join(cwd, "tessera", "vault.json") {
		t.Errorf("Resolve with an unclean XDG_DATA_HOME = %q, %v", p, err)
	}
}

func TestResolveRejectsControlCharacters(t *testing.T) {
	base := t.TempDir()
	chars := []struct {
		name, char string
		inEnv      bool
	}{
		{"NUL", "\x00", false},
		{"CR", "\r", true},
		{"LF", "\n", true},
	}
	for _, c := range chars {
		bad := filepath.Join(base, "a"+c.char+"b.json")
		t.Run("flag/"+c.name, func(t *testing.T) {
			assertInvalidPath(t, bad, "--vault")
		})
		if !c.inEnv {
			continue
		}
		t.Run("env/"+c.name, func(t *testing.T) {
			t.Setenv("TESSERA_VAULT", bad)
			assertInvalidPath(t, "", "TESSERA_VAULT")
		})
		t.Run("default/"+c.name, func(t *testing.T) {
			t.Setenv("TESSERA_VAULT", "")
			t.Setenv("XDG_DATA_HOME", filepath.Join(base, "data"+c.char))
			assertInvalidPath(t, "", "the default location")
		})
	}
}

func assertInvalidPath(t *testing.T, override, source string) {
	t.Helper()
	p, err := Resolve(override)
	var ipe *InvalidPathError
	if !errors.As(err, &ipe) {
		t.Fatalf("Resolve = %q, %v; want *InvalidPathError", p, err)
	}
	if ipe.Source != source {
		t.Errorf("Source = %q, want %q", ipe.Source, source)
	}
	if !strings.Contains(err.Error(), source) || strings.ContainsAny(err.Error(), "\x00\r\n") {
		t.Errorf("error %q should name %s and contain no raw control characters", err, source)
	}
}

func TestDefaultPathHint(t *testing.T) {
	want := "~/.local/share/tessera/vault.json"
	if runtime.GOOS == "windows" {
		want = `%LOCALAPPDATA%\Tessera\vault.json`
	}
	if got := DefaultPathHint(); got != want {
		t.Errorf("DefaultPathHint() = %q, want %q", got, want)
	}
}
