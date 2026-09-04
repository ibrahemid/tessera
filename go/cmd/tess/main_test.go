package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ibrahemid/tessera/go/internal/account"
)

func TestResolveVersionPrefersLdflagsStamp(t *testing.T) {
	orig := version
	defer func() { version = orig }()

	version = "1.2.3"
	if got := resolveVersion(); got != "1.2.3" {
		t.Fatalf("resolveVersion() = %q, want stamped %q", got, "1.2.3")
	}
}

func TestResolveVersionFallsBackForInTreeBuilds(t *testing.T) {
	orig := version
	defer func() { version = orig }()

	// A test binary's build info reports "(devel)" (or is absent), so the
	// module-version fallback must not fire and "dev" must survive.
	version = "dev"
	if got := resolveVersion(); got != "dev" {
		t.Fatalf("resolveVersion() = %q, want %q for an in-tree build", got, "dev")
	}
}

func runRoot(t *testing.T, args ...string) (string, error) {
	t.Helper()
	t.Setenv("TESSERA_CLIP_CLEAR", "")
	// Registering the root's --vault flag rebinds the global to its zero
	// value, so the test's vault has to be put back after construction.
	want := vaultPath
	c := newRootCmd()
	vaultPath = want
	var buf bytes.Buffer
	c.SetOut(&buf)
	c.SetErr(&bytes.Buffer{})
	c.SetArgs(args)
	err := c.Execute()
	return buf.String(), err
}

// TestRootQueryShortcutCopiesLikeCode pins `tess acme` == `tess code acme`.
func TestRootQueryShortcutCopiesLikeCode(t *testing.T) {
	path := withVault(t)
	sealVault(t, path, []account.Account{totp("a", "ACME", "x", 1)})
	stubTTY(t, true)
	var copied string
	stubClipboard(t, func(s string) error { copied = s; return nil })

	out, err := runRoot(t, "acme")
	if err != nil {
		t.Fatalf("tess acme: %v", err)
	}
	if len(copied) != 6 {
		t.Fatalf("copied %q, want a 6-digit code", copied)
	}
	if !strings.Contains(out, "copied") {
		t.Errorf("output should report the copy, got %q", out)
	}
}

// TestRootQueryShortcutTakesTheCodeFlags: the shortcut is not a lesser path.
func TestRootQueryShortcutTakesTheCodeFlags(t *testing.T) {
	path := withVault(t)
	sealVault(t, path, []account.Account{totp("a", "ACME", "x", 1)})
	stubTTY(t, true)
	stubClipboard(t, func(string) error {
		t.Error("--no-copy must not touch the clipboard")
		return nil
	})

	if _, err := runRoot(t, "acme", "--no-copy"); err != nil {
		t.Fatalf("tess acme --no-copy: %v", err)
	}
	out, err := runRoot(t, "acme", "--json", "--now")
	if err != nil {
		t.Fatalf("tess acme --json --now: %v", err)
	}
	if !strings.Contains(out, `"expires_in"`) {
		t.Errorf("JSON should carry expires_in, got %q", out)
	}
}

// TestRootSubcommandNameBeatsAHandle: an account called "list" must not shadow
// `tess list`, or the CLI becomes unpredictable as the vault grows.
func TestRootSubcommandNameBeatsAHandle(t *testing.T) {
	path := withVault(t)
	a := totp("a", "List", "me", 1)
	a.Handle = "list"
	sealVault(t, path, []account.Account{a})
	stubTTY(t, true)
	stubClipboard(t, func(string) error {
		t.Error("`tess list` must run the list command, not copy a code")
		return nil
	})

	out, err := runRoot(t, "list")
	if err != nil {
		t.Fatalf("tess list: %v", err)
	}
	if !strings.Contains(out, "list") && !strings.Contains(out, "List") {
		t.Errorf("expected the account listing, got %q", out)
	}
}

// TestRootWithNoArgsStillPrintsEveryCode keeps the bare `tess` behaviour.
func TestRootWithNoArgsStillPrintsEveryCode(t *testing.T) {
	path := withVault(t)
	sealVault(t, path, []account.Account{totp("a", "ACME", "x", 1), totp("b", "Bank", "y", 2)})
	stubTTY(t, false)

	out, err := runRoot(t)
	if err != nil {
		t.Fatalf("tess: %v", err)
	}
	if !strings.Contains(out, "ACME") || !strings.Contains(out, "Bank") {
		t.Errorf("expected every account, got %q", out)
	}
}

// TestRootUnknownQueryReportsNoMatch: a typo must not become "unknown command".
func TestRootUnknownQueryReportsNoMatch(t *testing.T) {
	path := withVault(t)
	sealVault(t, path, []account.Account{totp("a", "ACME", "x", 1)})
	stubTTY(t, false)

	_, err := runRoot(t, "nosuchthing")
	if err == nil || !strings.Contains(err.Error(), "no account matches") {
		t.Fatalf("expected a no-match error, got %v", err)
	}
}

// TestRootGroupsEveryVisibleCommand pins the grouped help: an ungrouped command
// falls into cobra's "Additional Commands" bucket and reads as an afterthought.
func TestRootGroupsEveryVisibleCommand(t *testing.T) {
	root := newRootCmd()
	want := map[string]string{
		"code": "codes", "watch": "codes",
		"add": "accounts", "list": "accounts", "show": "accounts", "rename": "accounts",
		"alias": "accounts", "move": "accounts", "tag": "accounts", "rm": "accounts",
		"import": "transfer", "export": "transfer", "merge": "transfer",
		"vault": "vault",
	}
	for name, group := range want {
		c, _, err := root.Find([]string{name})
		if err != nil {
			t.Fatalf("find %q: %v", name, err)
		}
		if c.GroupID != group {
			t.Errorf("tess %s is in group %q, want %q", name, c.GroupID, group)
		}
	}
}
