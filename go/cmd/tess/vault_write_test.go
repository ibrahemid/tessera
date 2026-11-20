package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/store"
	"github.com/ibrahemid/tessera/go/internal/tui"
	"github.com/ibrahemid/tessera/go/internal/vault"
)

func noted(id, issuer, acct string, created int64) account.Account {
	a := totp(id, issuer, acct, created)
	a.Note = "desk drawer"
	return a
}

func storedVersion(t *testing.T, path string) int {
	t.Helper()
	env, err := store.Load(path)
	if err != nil {
		t.Fatalf("load %s: %v", path, err)
	}
	return env.Version
}

// noticeLines returns the notice lines in out.
func noticeLines(out string) []string {
	var notes []string
	for line := range strings.Lines(out) {
		if strings.HasPrefix(line, "note: ") {
			notes = append(notes, strings.TrimSuffix(line, "\n"))
		}
	}
	return notes
}

func wantNotes(paths ...string) []string {
	var notes []string
	for _, p := range paths {
		notes = append(notes, "note: "+versionNotice(p))
	}
	return notes
}

func TestWriteVaultNoticesOnlyARiseFromVersionOne(t *testing.T) {
	v1, err := vault.Seal([]account.Account{totp("a", "ACME", "x", 1)}, "testpass123")
	if err != nil {
		t.Fatal(err)
	}
	v2, err := vault.Seal([]account.Account{noted("a", "ACME", "x", 1)}, "testpass123")
	if err != nil {
		t.Fatal(err)
	}
	if v1.Version != 1 || v2.Version != 2 {
		t.Fatalf("sealed versions %d and %d, want 1 and 2", v1.Version, v2.Version)
	}
	cases := []struct {
		name  string
		setup func(t *testing.T, path string)
		write *vault.Envelope
		rises bool
	}{
		{"version 1 to 2", func(t *testing.T, path string) { sealVault(t, path, []account.Account{totp("a", "ACME", "x", 1)}) }, v2, true},
		{"a vault this write creates", func(*testing.T, string) {}, v2, false},
		{"version 2 to 2", func(t *testing.T, path string) { sealVault(t, path, []account.Account{noted("a", "ACME", "x", 1)}) }, v2, false},
		{"version 1 to 1", func(t *testing.T, path string) { sealVault(t, path, []account.Account{totp("a", "ACME", "x", 1)}) }, v1, false},
		{"version 2 to 1", func(t *testing.T, path string) { sealVault(t, path, []account.Account{noted("a", "ACME", "x", 1)}) }, v1, false},
		{"a file that does not parse", func(t *testing.T, path string) {
			if err := os.WriteFile(path, []byte("not a vault"), 0o600); err != nil {
				t.Fatal(err)
			}
		}, v2, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "vault.json")
			c.setup(t, path)
			var got []string
			if err := writeVault(func(msg string) { got = append(got, msg) }, path, c.write); err != nil {
				t.Fatalf("write: %v", err)
			}
			var want []string
			if c.rises {
				want = []string{versionNotice(path)}
			}
			if !slices.Equal(got, want) {
				t.Fatalf("notices %q, want %q", got, want)
			}
			if v := storedVersion(t, path); v != c.write.Version {
				t.Fatalf("stored version %d, want %d", v, c.write.Version)
			}
		})
	}
}

func TestWriteVaultNoticeWaitsForTheSave(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.json")
	sealVault(t, path, []account.Account{totp("a", "ACME", "x", 1)})
	if err := os.Mkdir(path+".bak", 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path+".bak", "keep"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	v2, err := vault.Seal([]account.Account{noted("a", "ACME", "x", 1)}, "testpass123")
	if err != nil {
		t.Fatal(err)
	}
	called := false
	if err := writeVault(func(string) { called = true }, path, v2); err == nil {
		t.Fatal("the write should fail when the backup cannot be replaced")
	}
	if called {
		t.Fatal("a failed write printed the version notice")
	}
}

func TestVersionNoticePrintsOncePerRise(t *testing.T) {
	path := withVault(t)
	sealVault(t, path, []account.Account{totp("a", "ACME", "x", 1), totp("b", "GitHub", "dev", 2)})
	warnings := captureWarnings(t)
	steps := []struct {
		args    []string
		notice  bool
		version int
	}{
		{[]string{"list"}, false, 1},
		{[]string{"note", "ACME", "desk drawer"}, true, 2},
		{[]string{"note", "ACME", "top shelf"}, false, 2},
		{[]string{"order", "GitHub", "1"}, false, 2},
		{[]string{"note", "--clear", "ACME"}, false, 2},
		{[]string{"order", "--clear", "ACME"}, false, 2},
		{[]string{"order", "--clear", "GitHub"}, false, 1},
		{[]string{"order", "ACME", "2"}, true, 2},
		{[]string{"order", "--clear", "ACME"}, false, 2},
		{[]string{"order", "--clear", "GitHub"}, false, 1},
		{[]string{"rm", "--yes", "GitHub"}, true, 2},
		{[]string{"trash", "empty", "--yes"}, false, 1},
	}
	for _, s := range steps {
		warnings.Reset()
		stdout, stderr, err := runRootStreams(t, s.args...)
		if err != nil {
			t.Fatalf("%v: %v", s.args, err)
		}
		var want []string
		if s.notice {
			want = wantNotes(path)
		}
		if got := noticeLines(warnings.String() + stderr); !slices.Equal(got, want) {
			t.Fatalf("%v: notices %q, want %q", s.args, got, want)
		}
		if strings.Contains(stdout, "format version") {
			t.Fatalf("%v: the notice reached stdout: %q", s.args, stdout)
		}
		if v := storedVersion(t, path); v != s.version {
			t.Fatalf("%v: stored version %d, want %d", s.args, v, s.version)
		}
	}
}

func TestVersionNoticeKeepsImportJSONOnStdout(t *testing.T) {
	path := withVault(t)
	sealVault(t, path, []account.Account{totp("a", "ACME", "x", 1)})
	warnings := captureWarnings(t)
	src := filepath.Join(t.TempDir(), "aegis.json")
	const aegis = `{"version":1,"header":{"slots":null,"params":null},"db":{"version":3,"entries":[` +
		`{"type":"totp","uuid":"5f3a6e70-8f4a-4b8a-9c1e-1a2b3c4d5e6f","name":"me","issuer":"Noted","note":"desk drawer",` +
		`"info":{"secret":"JBSWY3DPEHPK3PXP","algo":"SHA1","digits":6,"period":30}}]}}`
	if err := os.WriteFile(src, []byte(aegis), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err := runRootStreams(t, "import", src, "--json")
	if err != nil {
		t.Fatalf("import: %v\n%s", err, stderr)
	}
	var report struct {
		Added int `json:"added"`
	}
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	if report.Added != 1 {
		t.Fatalf("added %d, want 1", report.Added)
	}
	if got := noticeLines(warnings.String() + stderr); !slices.Equal(got, wantNotes(path)) {
		t.Fatalf("notices %q, want %q", got, wantNotes(path))
	}
	if v := storedVersion(t, path); v != 2 {
		t.Fatalf("stored version %d, want 2", v)
	}
}

func TestVersionNoticeOnEveryWriteCommand(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T, path string) []string
	}{
		{"restore", func(t *testing.T, path string) []string {
			sealVault(t, path, []account.Account{noted("a", "ACME", "x", 1)})
			sealVault(t, path, []account.Account{totp("a", "ACME", "x", 1)})
			return []string{"vault", "restore", "--bak", "--yes"}
		}},
		{"dedupe", func(t *testing.T, path string) []string {
			sealVault(t, path, []account.Account{totp("a", "ACME", "x", 1), totp("b", "ACME", "x", 2)})
			return []string{"dedupe", "--yes"}
		}},
		{"merge", func(t *testing.T, path string) []string {
			sealVault(t, path, []account.Account{totp("a", "ACME", "x", 1)})
			src := filepath.Join(t.TempDir(), "src.json")
			sealAt(t, src, "testpass123", []account.Account{noted("b", "Beta", "y", 2)})
			return []string{"merge", src}
		}},
		{"edit", func(t *testing.T, path string) []string {
			sealVault(t, path, []account.Account{totp("a", "ACME", "x", 1)})
			return []string{"rm", "--yes", "ACME"}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := withVault(t)
			args := c.setup(t, path)
			if v := storedVersion(t, path); v != 1 {
				t.Fatalf("setup left version %d, want 1", v)
			}
			warnings := captureWarnings(t)
			stdout, stderr, err := runRootStreams(t, args...)
			if err != nil {
				t.Fatalf("%v: %v\n%s", args, err, stderr)
			}
			if got := noticeLines(warnings.String() + stderr); !slices.Equal(got, wantNotes(path)) {
				t.Fatalf("%v: notices %q, want %q", args, got, wantNotes(path))
			}
			if strings.Contains(stdout, "format version") {
				t.Fatalf("%v: the notice reached stdout: %q", args, stdout)
			}
			if v := storedVersion(t, path); v != 2 {
				t.Fatalf("%v: stored version %d, want 2", args, v)
			}
		})
	}
}

func TestTwoWayMergeNoticesTheSideThatRose(t *testing.T) {
	cases := []struct {
		name          string
		local, source []account.Account
		sourceRises   bool
	}{
		{"local", []account.Account{totp("a", "ACME", "x", 1)}, []account.Account{noted("b", "Beta", "y", 2)}, false},
		{"source", []account.Account{noted("a", "ACME", "x", 1)}, []account.Account{totp("b", "Beta", "y", 2)}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			target := withVault(t)
			sealAt(t, target, "testpass123", c.local)
			src := filepath.Join(t.TempDir(), "src.json")
			sealAt(t, src, "testpass123", c.source)
			resolved, err := resolveMergeSource(src)
			if err != nil {
				t.Fatal(err)
			}
			rising := target
			if c.sourceRises {
				rising = resolved
			}
			warnings := captureWarnings(t)
			out := runMergeTwoWay(t, src)
			if strings.Contains(out, "format version") {
				t.Fatalf("the notice reached stdout: %q", out)
			}
			if got := noticeLines(warnings.String()); !slices.Equal(got, wantNotes(rising)) {
				t.Fatalf("notices %q, want %q", got, wantNotes(rising))
			}
			for _, p := range []string{target, src} {
				if v := storedVersion(t, p); v != 2 {
					t.Fatalf("%s: stored version %d, want 2", p, v)
				}
			}
			warnings.Reset()
			runMergeTwoWay(t, src)
			if got := noticeLines(warnings.String()); got != nil {
				t.Fatalf("a second run printed %q", got)
			}
		})
	}
}

func TestWatchWriteHandsTheVersionNoticeToTheView(t *testing.T) {
	w, path := openTestWatch(t)
	warnings := captureWarnings(t)
	steps := []struct {
		change tui.Change
		notice string
	}{
		{tui.Change{Kind: tui.ChangeTrash, ID: "b"}, versionNotice(path)},
		{tui.Change{Kind: tui.ChangePin, ID: "a", Pinned: true}, ""},
		{tui.Change{Kind: tui.ChangeRestore, ID: "b"}, ""},
		{tui.Change{Kind: tui.ChangeTrash, ID: "b"}, versionNotice(path)},
	}
	for i, s := range steps {
		res, err := w.mutate(s.change)
		if err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
		if res.Notice != s.notice {
			t.Fatalf("step %d: notice %q, want %q", i, res.Notice, s.notice)
		}
	}
	if warnings.Len() != 0 {
		t.Fatalf("the watch write printed to stderr: %q", warnings.String())
	}
	if w.s.notify != nil {
		t.Fatal("the view's notice hook outlived the write")
	}
}
