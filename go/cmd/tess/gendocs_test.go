//go:build gendocs

package main

import (
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ibrahemid/tessera/go/internal/store"
	"github.com/spf13/cobra"
)

// monthStartEpoch is 2023-11-01T00:00:00Z: a zone west of UTC reads it as October.
const monthStartEpoch = "1698796800"

type gendocsRun struct {
	root          *cobra.Command
	manDir        string
	completionDir string
}

func runGendocs(t *testing.T) (gendocsRun, error) {
	t.Helper()
	defer func(v string) { vaultPath = v }(vaultPath)
	dir := t.TempDir()
	run := gendocsRun{
		root:          newRootCmd(),
		manDir:        filepath.Join(dir, "man"),
		completionDir: filepath.Join(dir, "completions"),
	}
	run.root.SetOut(io.Discard)
	run.root.SetErr(io.Discard)
	run.root.SetArgs([]string{"__gendocs", "--man", run.manDir, "--completions", run.completionDir})
	return run, run.root.Execute()
}

func readDir(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	files := make(map[string][]byte, len(entries))
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		files[e.Name()] = b
	}
	return files
}

func gunzip(t *testing.T, name string, b []byte) string {
	t.Helper()
	zr, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if zr.Name != "" || !zr.ModTime.IsZero() {
		t.Errorf("%s: gzip header name %q, mtime %v; want empty and zero", name, zr.Name, zr.ModTime)
	}
	page, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return string(page)
}

func visibleCommandPaths(c *cobra.Command) []string {
	var paths []string
	for _, child := range c.Commands() {
		if child.IsAvailableCommand() && !child.IsAdditionalHelpTopicCommand() {
			paths = append(paths, visibleCommandPaths(child)...)
		}
	}
	return append(paths, c.CommandPath())
}

func TestGendocsIsReproducible(t *testing.T) {
	t.Setenv("SOURCE_DATE_EPOCH", monthStartEpoch)
	first, err := runGendocs(t)
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	second, err := runGendocs(t)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	for _, dirs := range [][2]string{{first.manDir, second.manDir}, {first.completionDir, second.completionDir}} {
		a, b := readDir(t, dirs[0]), readDir(t, dirs[1])
		if len(a) != len(b) {
			t.Fatalf("%d files vs %d files", len(a), len(b))
		}
		for name, content := range a {
			if !bytes.Equal(content, b[name]) {
				t.Errorf("%s differs between runs", name)
			}
		}
	}
}

func TestGendocsRequiresSourceDateEpoch(t *testing.T) {
	for _, value := range []string{"", "yesterday", "-1", "1.5"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("SOURCE_DATE_EPOCH", value)
			if value == "" {
				os.Unsetenv("SOURCE_DATE_EPOCH")
			}
			run, err := runGendocs(t)
			var sde *sourceDateError
			if !errors.As(err, &sde) {
				t.Fatalf("error = %v, want *sourceDateError", err)
			}
			if _, err := os.Stat(run.manDir); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("man directory exists after a failed run: %v", err)
			}
		})
	}
}

func TestGendocsWritesOnePagePerVisibleCommand(t *testing.T) {
	t.Setenv("SOURCE_DATE_EPOCH", monthStartEpoch)
	run, err := runGendocs(t)
	if err != nil {
		t.Fatal(err)
	}
	var want []string
	for _, p := range visibleCommandPaths(run.root) {
		want = append(want, strings.ReplaceAll(p, " ", "-")+".1.gz")
	}
	pages := readDir(t, run.manDir)
	got := make([]string, 0, len(pages))
	for name := range pages {
		got = append(got, name)
	}
	slices.Sort(want)
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Errorf("pages = %v\nwant %v", got, want)
	}
	for _, name := range []string{"tess.1.gz", "tess-add.1.gz", "tess-vault-init.1.gz"} {
		if _, ok := pages[name]; !ok {
			t.Errorf("missing %s", name)
		}
	}
	for name, b := range pages {
		if strings.Contains(name, "__") || strings.HasPrefix(name, "tess-help") {
			t.Errorf("page for a hidden command: %s", name)
		}
		page := gunzip(t, name, b)
		for _, hidden := range []string{"__clip-clear", "__gendocs", "__complete", "Auto generated"} {
			if strings.Contains(page, hidden) {
				t.Errorf("%s mentions %q", name, hidden)
			}
		}
	}
	root := gunzip(t, "tess.1.gz", pages["tess.1.gz"])
	if want := `.TH "TESS" "1" "Nov 2023" "tess ` + resolveVersion() + `"`; !strings.Contains(root, want) {
		t.Errorf("tess.1 header lacks %q:\n%.300s", want, root)
	}
	if !strings.Contains(root, `\fB--version\fP`) {
		t.Error("tess.1 does not document --version")
	}
	if !strings.Contains(root, ".EX\n  tess ") || !strings.Contains(root, "\n  tess add <uri|key|file>  add accounts\n") {
		t.Errorf("tess.1 does not keep the indented help lines preformatted:\n%s", root)
	}
	if alias := gunzip(t, "tess-alias.1.gz", pages["tess-alias.1.gz"]); !strings.Contains(alias, `\fBtess alias <account> <alias> [flags]\fP`) {
		t.Errorf("tess-alias.1 synopsis lost its arguments:\n%s", alias)
	}
}

func TestManDescription(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"prose", "Set <alias> for *one* account_name `now`", "Set \\<alias\\> for \\*one\\* account\\_name \\`now\\`\n"},
		{"block between prose", "Run:\n\n  tess <q>  # code\n  tess\n\nThen go.", "Run:\n\n\n```\n  tess <q>  # code\n  tess\n```\n\n\nThen go.\n"},
		{"block at end", "Use:\n  tess *", "Use:\n\n```\n  tess *\n```\n"},
		{"tab indent", "\tx", "\n```\n\tx\n```\n"},
		{"empty", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := manDescription(c.in); got != c.want {
				t.Errorf("manDescription(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestGendocsDateIgnoresLocalZone(t *testing.T) {
	t.Setenv("SOURCE_DATE_EPOCH", monthStartEpoch)
	defer func(loc *time.Location) { time.Local = loc }(time.Local)
	time.Local = time.FixedZone("UTC-5", -5*60*60)
	run, err := runGendocs(t)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(run.manDir, "tess.1.gz"))
	if err != nil {
		t.Fatal(err)
	}
	if page := gunzip(t, "tess.1.gz", b); !strings.Contains(page, `"Nov 2023"`) {
		t.Errorf("tess.1 date is not Nov 2023:\n%.300s", page)
	}
}

func TestGendocsWritesCompletionScripts(t *testing.T) {
	t.Setenv("SOURCE_DATE_EPOCH", monthStartEpoch)
	run, err := runGendocs(t)
	if err != nil {
		t.Fatal(err)
	}
	scripts := readDir(t, run.completionDir)
	got := make([]string, 0, len(scripts))
	for name, b := range scripts {
		got = append(got, name)
		if !bytes.Contains(b, []byte("__complete")) {
			t.Errorf("%s does not call tess __complete", name)
		}
	}
	slices.Sort(got)
	if want := []string{"_tess", "tess.bash", "tess.fish", "tess.ps1"}; !slices.Equal(got, want) {
		t.Errorf("completion files = %v, want %v", got, want)
	}
}

func TestVaultFlagHelpNamesDefaultPath(t *testing.T) {
	defer func(v string) { vaultPath = v }(vaultPath)
	root := newRootCmd()
	hint := store.DefaultPathHint()
	if usage := root.PersistentFlags().Lookup("vault").Usage; !strings.Contains(usage, hint) {
		t.Errorf("--vault usage %q does not name %q", usage, hint)
	}
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(io.Discard)
	root.SetArgs([]string{"--help"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), hint) {
		t.Errorf("tess --help does not name %q", hint)
	}
}
