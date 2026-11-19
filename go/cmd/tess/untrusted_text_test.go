package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/store"
)

// hostileIssuer carries an OSC 52 clipboard write, a C1 control sequence
// introducer, and a right-to-left override.
const hostileIssuer = "Evil\x1b]52;c;SGVsbG8=\x07\u009b31m\u202eCorp"

const escapedOSC = `\x1b]52`

// runRootStreams runs the root command the way main does, printing a returned
// error to the captured stderr.
func runRootStreams(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	t.Setenv("TESSERA_CLIP_CLEAR", "")
	want := vaultPath
	c := newRootCmd()
	vaultPath = want
	var stdout, stderr bytes.Buffer
	c.SetOut(&stdout)
	c.SetErr(&stderr)
	c.SetArgs(args)
	err := c.Execute()
	if err != nil {
		reportError(&stderr, err)
	}
	return stdout.String(), stderr.String(), err
}

// assertNoRawControl fails when s holds any byte a terminal could act on.
func assertNoRawControl(t *testing.T, where, s string) {
	t.Helper()
	if !utf8.ValidString(s) {
		t.Errorf("%s: invalid UTF-8 reached the terminal: %q", where, s)
	}
	for _, r := range s {
		switch {
		case r == '\n' || r == '\t':
		case r < 0x20, r == 0x7f, r >= 0x80 && r <= 0x9f,
			r >= 0x202a && r <= 0x202e, r >= 0x2066 && r <= 0x2069, r == 0xfeff:
			t.Errorf("%s: raw %U reached the terminal: %q", where, r, s)
			return
		}
	}
}

func sealHostileVault(t *testing.T) string {
	t.Helper()
	path := withVault(t)
	sealVault(t, path, []account.Account{
		handled("a", hostileIssuer, "me\x1b]0;title\x07", "ev"),
		handled("b", hostileIssuer, "ops\u202e", "ev2"),
	})
	return path
}

func TestUntrustedTextIsEscapedInEveryView(t *testing.T) {
	cases := []struct {
		name  string
		tty   bool
		args  []string
		fails bool
	}{
		{"code table", false, nil, false},
		{"code table on a terminal", true, nil, false},
		{"list", false, []string{"list"}, false},
		{"show", false, []string{"show", "ev"}, false},
		{"ambiguous query", false, []string{"evil"}, true},
		{"ambiguous code on a terminal", true, []string{"code", "evil"}, false},
		{"ambiguous show on a terminal", true, []string{"show", "evil"}, true},
		{"ambiguous show", false, []string{"show", "evil"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sealHostileVault(t)
			stubTTY(t, c.tty)
			stubClipboard(t, func(string) error { return nil })
			stdout, stderr, err := runRootStreams(t, c.args...)
			if (err != nil) != c.fails {
				t.Fatalf("err = %v, want failure %v", err, c.fails)
			}
			assertNoRawControl(t, "stdout", stdout)
			assertNoRawControl(t, "stderr", stderr)
			if !strings.Contains(stdout+stderr, escapedOSC) {
				t.Errorf("the issuer should be shown escaped:\nstdout: %s\nstderr: %s", stdout, stderr)
			}
		})
	}
}

func TestJSONOutputKeepsTheStoredText(t *testing.T) {
	sealHostileVault(t)
	stdout, _, err := runRootStreams(t, "list", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var rows []struct {
		Issuer string `json:"issuer"`
	}
	if err := json.Unmarshal([]byte(stdout), &rows); err != nil {
		t.Fatalf("list --json is not JSON: %v", err)
	}
	if len(rows) != 2 || rows[0].Issuer != hostileIssuer {
		t.Fatalf("--json must carry the stored issuer unchanged, got %+v", rows)
	}
}

func TestModifyMessagesEscapeUntrustedText(t *testing.T) {
	cases := [][]string{
		{"rename", "ev", "--issuer", hostileIssuer},
		{"move", "ev", "work\x1b]52;c;AA\x07"},
		{"tag", "ev", "t\x1b]52;c;AA\x07"},
		{"tag", "ev", "--remove", "x\x1b]52;c;AA\x07"},
		{"rm", "--yes", "ev"},
	}
	for _, args := range cases {
		t.Run(args[0], func(t *testing.T) {
			sealHostileVault(t)
			stdout, stderr, err := runRootStreams(t, args...)
			if err != nil {
				t.Fatalf("%v: %v", args, err)
			}
			assertNoRawControl(t, "stdout", stdout)
			assertNoRawControl(t, "stderr", stderr)
			if !strings.Contains(stdout, `\x1b]`) {
				t.Errorf("expected the escaped text in %q", stdout)
			}
		})
	}
}

func TestAddMessagesEscapeUntrustedText(t *testing.T) {
	path := withVault(t)
	sealVault(t, path, []account.Account{})
	const uri = "otpauth://totp/me%1B%5D0%3Bt%07?secret=JBSWY3DPEHPK3PXP&issuer=Evil%1B%5D52%3Bc%3BSGVsbG8%3D%07%C2%9B31m%E2%80%AECorp"
	for _, want := range []string{"Added", "Skipped"} {
		stdout, stderr, err := runRootStreams(t, "add", uri)
		if err != nil {
			t.Fatalf("add: %v", err)
		}
		assertNoRawControl(t, "stdout", stdout)
		assertNoRawControl(t, "stderr", stderr)
		if !strings.HasPrefix(stdout, want) {
			t.Errorf("want a %s line, got %q", want, stdout)
		}
	}
}

func TestErrorsEscapeUntrustedText(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"no match", []string{"code", "zz\x1b]52;c;AA\x07\u202e"}},
		{"issuer mismatch", []string{"add", "otpauth://totp/Evil%1B%5D52%3Bc%3BAA%07:me?secret=JBSWY3DPEHPK3PXP&issuer=Other"}},
		{"bad alias", []string{"alias", "ev", "x\x1b]52;c;AA\x07"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sealHostileVault(t)
			stdout, stderr, err := runRootStreams(t, c.args...)
			if err == nil {
				t.Fatalf("%v should fail", c.args)
			}
			assertNoRawControl(t, "stdout", stdout)
			assertNoRawControl(t, "stderr", stderr)
			if !strings.HasPrefix(stderr, "error: ") {
				t.Errorf("want an error line on stderr, got %q", stderr)
			}
		})
	}
}

func TestImportProblemsEscapeTheSourceName(t *testing.T) {
	sealHostileVault(t)
	src := filepath.Join(t.TempDir(), "list\x1b]52;c;AA\x07\u202e.txt")
	if err := os.WriteFile(src, []byte("not an account\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, _ := runRootStreams(t, "import", src)
	assertNoRawControl(t, "stdout", stdout)
	assertNoRawControl(t, "stderr", stderr)
	if !strings.Contains(stderr, "Could not import 1 item:") || !strings.Contains(stderr, escapedOSC) {
		t.Errorf("want the escaped source in the problem list, got %q", stderr)
	}
}

func TestQRExportNamesAreEscapedAndStayInTheDirectory(t *testing.T) {
	path := withVault(t)
	sealVault(t, path, []account.Account{
		handled("a", "../../x\x1b]52;c;AA\x07", "a/b\\c\u202e", "ev"),
	})
	dir := filepath.Join(t.TempDir(), "qr")
	stdout, stderr, err := runRootStreams(t, "export", "--qr", dir)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	assertNoRawControl(t, "stdout", stdout)
	assertNoRawControl(t, "stderr", stderr)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].IsDir() {
		t.Fatalf("want one file directly in the directory, got %v", entries)
	}
	name := entries[0].Name()
	assertNoRawControl(t, "file name", name)
	if strings.ContainsAny(name, `/\`) || strings.HasPrefix(name, ".") {
		t.Errorf("unsafe file name %q", name)
	}
}

func TestCompletionDescriptionsEscapeUntrustedText(t *testing.T) {
	stubCompletionAccounts(t, func() ([]account.Account, error) {
		return []account.Account{handled("a", hostileIssuer, "me\x1b]0;t\x07", "ev")}, nil
	})
	got, _ := completeHandles("e")
	if len(got) != 1 {
		t.Fatalf("want one completion, got %q", got)
	}
	assertNoRawControl(t, "completion", got[0])
}

func TestVaultStatusEscapesWrapTypes(t *testing.T) {
	path := withVault(t)
	sealVault(t, path, []account.Account{})
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	wraps := doc["wraps"].([]any)
	extra := map[string]any{}
	for k, v := range wraps[0].(map[string]any) {
		extra[k] = v
	}
	extra["type"] = "x\x1b]52;c;AA\x07\u202e"
	doc["wraps"] = append(wraps, extra)
	raw, err = json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(path); err != nil {
		t.Fatalf("the edited vault no longer loads: %v", err)
	}
	stdout, stderr, err := runRootStreams(t, "vault", "status")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	assertNoRawControl(t, "stdout", stdout)
	assertNoRawControl(t, "stderr", stderr)
	if !strings.Contains(stdout, escapedOSC) {
		t.Errorf("want the escaped wrap type, got %q", stdout)
	}
}
