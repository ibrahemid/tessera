package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/store"
)

const (
	acmeTrashedAt    = 1_700_000_000
	dropboxTrashedAt = 1_750_000_000
)

func trashedAt(a account.Account, at int64) account.Account {
	a.DeletedAt, a.UpdatedAt = at, at
	return a
}

// sealTrashVault seals one live account (gi) and two trashed ones (ac, dr).
func sealTrashVault(t *testing.T) string {
	t.Helper()
	path := withVault(t)
	sealVault(t, path, []account.Account{
		handled("a", "GitHub", "me", "gi"),
		trashedAt(handled("b", "Acme", "ops", "ac"), acmeTrashedAt),
		trashedAt(handled("c", "Dropbox", "me", "dr"), dropboxTrashedAt),
	})
	return path
}

// stubConfirm answers every visible prompt with fn.
func stubConfirm(t *testing.T, fn func(prompt string) (string, error)) {
	t.Helper()
	old := promptLine
	promptLine = fn
	t.Cleanup(func() { promptLine = old })
}

func TestTrashListsTrashedAccountsNewestFirst(t *testing.T) {
	sealTrashVault(t)
	stdout, _, err := runRootStreams(t, "trash")
	if err != nil {
		t.Fatalf("trash: %v", err)
	}
	lines := strings.Split(strings.TrimRight(stdout, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("want a header and two rows, got %q", stdout)
	}
	for _, h := range []string{"alias", "issuer", "account", "trashed"} {
		if !strings.Contains(lines[0], h) {
			t.Errorf("header %q lacks %q", lines[0], h)
		}
	}
	if !strings.HasPrefix(lines[1], "dr ") || !strings.Contains(lines[1], time.Unix(dropboxTrashedAt, 0).Format(trashDateLayout)) {
		t.Errorf("first row = %q, want the most recently trashed", lines[1])
	}
	if !strings.HasPrefix(lines[2], "ac ") || !strings.Contains(lines[2], time.Unix(acmeTrashedAt, 0).Format(trashDateLayout)) {
		t.Errorf("second row = %q", lines[2])
	}
	if strings.Contains(stdout, "GitHub") {
		t.Errorf("the Trash lists a live account: %q", stdout)
	}
}

func TestTrashSaysWhenEmpty(t *testing.T) {
	sealGitHub(t)
	stdout, _, err := runRootStreams(t, "trash")
	if err != nil || stdout != "The Trash is empty.\n" {
		t.Fatalf("err = %v stdout = %q", err, stdout)
	}
}

func TestTrashedAccountsAreInvisibleToOtherCommands(t *testing.T) {
	hint := "error: no account matches \"dr\"; Dropbox (me) is in the Trash, `tess restore dr` brings it back\n"
	for _, args := range [][]string{
		{"code", "dr"}, {"dr"}, {"show", "dr"}, {"rename", "dr", "--issuer", "X"},
		{"alias", "dr", "box"}, {"move", "dr", "work"}, {"tag", "dr", "t"},
		{"pin", "dr"}, {"unpin", "dr"}, {"note", "dr", "x"}, {"order", "dr", "1"},
		{"edit", "dr", "--digits", "8"}, {"export", "--uri", "dr"}, {"rm", "dr"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			path := sealTrashVault(t)
			before := readVaultBytes(t, path)
			_, stderr, err := runRootStreams(t, args...)
			if err == nil || stderr != hint {
				t.Fatalf("err = %v stderr = %q", err, stderr)
			}
			if !bytes.Equal(before, readVaultBytes(t, path)) {
				t.Fatal("the vault was rewritten")
			}
		})
	}
}

func TestTrashedAccountsAreLeftOutOfListings(t *testing.T) {
	for _, args := range [][]string{
		{"list"}, {"list", "--json"}, {"code"}, {"code", "--json"}, {"export", "--uri"}, {"export", "--secret"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			path := withVault(t)
			sealVault(t, path, []account.Account{
				handled("a", "GitHub", "me", "gi"),
				trashedAt(handled("c", "Dropbox", "me", "dr"), dropboxTrashedAt),
			})
			stdout, _, err := runRootStreams(t, args...)
			if err != nil {
				t.Fatalf("%v: %v", args, err)
			}
			if strings.Contains(stdout, "Dropbox") {
				t.Errorf("a trashed account is listed: %q", stdout)
			}
			secretsOnly := len(args) == 2 && args[0] == "export" && args[1] == "--secret"
			switch {
			case secretsOnly && strings.Count(stdout, "\n") != 1:
				t.Errorf("want one secret, got %q", stdout)
			case !secretsOnly && !strings.Contains(stdout, "GitHub"):
				t.Errorf("the live account is missing: %q", stdout)
			}
		})
	}
}

func TestListWithOnlyTrashedAccountsSuggestsAdding(t *testing.T) {
	path := withVault(t)
	sealVault(t, path, []account.Account{trashedAt(handled("c", "Dropbox", "me", "dr"), dropboxTrashedAt)})
	stdout, _, err := runRootStreams(t, "list")
	if err != nil || stdout != "No accounts. Add one with `tess add`.\n" {
		t.Fatalf("err = %v stdout = %q", err, stdout)
	}
}

func TestImportDoesNotTreatTrashedAccountsAsDuplicates(t *testing.T) {
	path := withVault(t)
	sealVault(t, path, []account.Account{trashedAt(totp("c", "Dropbox", "me", 1), dropboxTrashedAt)})
	stdout, _, err := runRootStreams(t, "import", "--otpauth",
		"otpauth://totp/Dropbox:me?secret=GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ&issuer=Dropbox")
	if err != nil || !strings.HasPrefix(stdout, "Added Dropbox (me) as ") || !strings.HasSuffix(stdout, "\nImported 1\n") {
		t.Fatalf("err = %v stdout = %q", err, stdout)
	}
	if got := openVaultFile(t, path, "testpass123"); len(account.Live(got)) != 1 || len(account.Trashed(got)) != 1 {
		t.Fatalf("want one live and one trashed account, got %+v", got)
	}
}

func TestWatchHandsTheViewTheTrashApartFromTheLiveAccounts(t *testing.T) {
	sealTrashVault(t)
	opts := stubWatchView(t)
	if err := runWatchCmd(t, ""); err != nil {
		t.Fatalf("watch: %v", err)
	}
	live, trashed := account.Live(opts.Accounts), account.Trashed(opts.Accounts)
	if len(live) != 1 || live[0].ID != "a" || len(trashed) != 2 {
		t.Fatalf("watch hands the view %+v", opts.Accounts)
	}
}

func TestRestoreBringsAnAccountBack(t *testing.T) {
	path := sealTrashVault(t)
	stubClock(t, time.Unix(1_800_000_000, 0))
	stdout, _, err := runRootStreams(t, "restore", "dr")
	if err != nil || stdout != "Restored Dropbox (me)\n" {
		t.Fatalf("err = %v stdout = %q", err, stdout)
	}
	a := ids(openVaultFile(t, path, "testpass123"))["c"]
	if a.DeletedAt != 0 || a.UpdatedAt != 1_800_000_000 || a.Handle != "dr" {
		t.Fatalf("restored = %+v", a)
	}
}

func TestRestoreMatchesTrashedAccountsOnly(t *testing.T) {
	sealTrashVault(t)
	_, stderr, err := runRootStreams(t, "restore", "gi")
	if err == nil || stderr != "error: nothing in the Trash matches \"gi\"; `tess trash` lists the Trash\n" {
		t.Fatalf("err = %v stderr = %q", err, stderr)
	}
}

func TestMultiQueryCommandsAreAllOrNothing(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"restore miss", []string{"restore", "dr", "zz"}},
		{"rm miss", []string{"rm", "gi", "zz"}},
		{"rm ambiguous", []string{"rm", "gi", "slack"}},
		{"purge miss", []string{"rm", "--purge", "-y", "dr", "zz"}},
		{"move miss", []string{"move", "gi", "zz", "work"}},
		{"tag ambiguous", []string{"tag", "gi", "slack", "t"}},
		{"pin miss", []string{"pin", "gi", "zz"}},
		{"unpin ambiguous", []string{"unpin", "gi", "slack"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := withVault(t)
			sealVault(t, path, []account.Account{
				handled("a", "GitHub", "me", "gi"),
				handled("s1", "Slack", "one", "sl"),
				handled("s2", "Slack", "two", "sl2"),
				trashedAt(handled("c", "Dropbox", "me", "dr"), dropboxTrashedAt),
			})
			before := readVaultBytes(t, path)
			if _, _, err := runRootStreams(t, c.args...); err == nil {
				t.Fatalf("%v must fail", c.args)
			}
			if !bytes.Equal(before, readVaultBytes(t, path)) {
				t.Fatal("a failed query still changed the vault")
			}
		})
	}
}

func TestMultiQueryCommandsApplyToEveryAccount(t *testing.T) {
	path := sealTrashVault(t)
	if _, _, err := runRootStreams(t, "restore", "dr", "ac"); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if got := openVaultFile(t, path, "testpass123"); len(account.Trashed(got)) != 0 {
		t.Fatalf("still trashed: %+v", account.Trashed(got))
	}
	stdout, _, err := runRootStreams(t, "rm", "gi", "dr", "gi")
	if err != nil {
		t.Fatalf("rm: %v", err)
	}
	want := "Moved GitHub (me) to the Trash\nMoved Dropbox (me) to the Trash\nRestore with `tess restore gi dr`\n"
	if stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
	if got := openVaultFile(t, path, "testpass123"); len(account.Live(got)) != 1 {
		t.Fatalf("live = %+v", account.Live(got))
	}
}

func TestHandlesStayUniqueAcrossTheTrash(t *testing.T) {
	path := sealTrashVault(t)
	_, stderr, err := runRootStreams(t, "alias", "gi", "dr")
	if err == nil || stderr != "error: alias \"dr\" already used by Dropbox (me) in the Trash\n" {
		t.Fatalf("err = %v stderr = %q", err, stderr)
	}
	stdout, _, err := runRootStreams(t, "add", "otpauth://totp/Dropbox:work?secret=JBSWY3DPEHPK3PXP&issuer=Dropbox")
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if !strings.Contains(stdout, "dr2") {
		t.Errorf("a new Dropbox account must skip the trashed alias, got %q", stdout)
	}
	if _, _, err := runRootStreams(t, "restore", "dr"); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if err := account.CheckHandleUniqueness(openVaultFile(t, path, "testpass123")); err != nil {
		t.Fatalf("handles collide after a restore: %v", err)
	}
}

func TestRemovePurgeReachesTheTrash(t *testing.T) {
	path := sealTrashVault(t)
	stdout, _, err := runRootStreams(t, "rm", "--purge", "-y", "dr", "gi")
	if err != nil {
		t.Fatalf("rm --purge: %v", err)
	}
	if stdout != "Deleted Dropbox (me) for good\nDeleted GitHub (me) for good\n" {
		t.Errorf("stdout = %q", stdout)
	}
	got := ids(openVaultFile(t, path, "testpass123"))
	if len(got) != 1 || got["b"].ID != "b" {
		t.Fatalf("left = %+v", got)
	}
}

func TestTrashEmptyDeletesOnlyTheTrash(t *testing.T) {
	path := sealTrashVault(t)
	stdout, _, err := runRootStreams(t, "trash", "empty", "--yes")
	if err != nil || stdout != "Deleted 2 accounts for good\n" {
		t.Fatalf("err = %v stdout = %q", err, stdout)
	}
	got := openVaultFile(t, path, "testpass123")
	if len(got) != 1 || got[0].ID != "a" {
		t.Fatalf("left = %+v", got)
	}
	stdout, _, err = runRootStreams(t, "trash", "empty", "--yes")
	if err != nil || stdout != "The Trash is empty.\n" {
		t.Fatalf("err = %v stdout = %q", err, stdout)
	}
}

func TestTrashEmptyOlderThan(t *testing.T) {
	path := sealTrashVault(t)
	stubClock(t, time.Unix(dropboxTrashedAt+10*86400, 0))
	stdout, _, err := runRootStreams(t, "trash", "empty", "--older-than", "1000d", "-y")
	if err != nil || stdout != "Nothing in the Trash is older than 1000d\n" {
		t.Fatalf("err = %v stdout = %q", err, stdout)
	}
	stdout, _, err = runRootStreams(t, "trash", "empty", "--older-than", "30d", "-y")
	if err != nil || stdout != "Deleted Acme (ops) for good\n" {
		t.Fatalf("err = %v stdout = %q", err, stdout)
	}
	if got := ids(openVaultFile(t, path, "testpass123")); len(got) != 2 || got["c"].ID != "c" {
		t.Fatalf("left = %+v", got)
	}
}

func TestTrashEmptyAsksFirst(t *testing.T) {
	path := sealTrashVault(t)
	before := readVaultBytes(t, path)
	_, stderr, err := runRootStreams(t, "trash", "empty")
	if err == nil || stderr != "error: refusing to delete 2 accounts for good without --yes (stdin is not a terminal)\n" {
		t.Fatalf("err = %v stderr = %q", err, stderr)
	}
	stubStdinTTY(t, true)
	shown := stubPrompts(t, nil, []string{"n"})
	_, stderr, err = runRootStreams(t, "trash", "empty")
	if err != nil {
		t.Fatalf("declined: %v", err)
	}
	if !strings.Contains(stderr, "dr") || !strings.Contains(stderr, "Acme (ops)") {
		t.Errorf("the accounts are not listed before the question: %q", stderr)
	}
	want := "Delete 2 accounts for good? Their secrets are removed from the vault. [y/N] "
	if len(*shown) != 1 || (*shown)[0] != want {
		t.Fatalf("prompts = %q", *shown)
	}
	if !bytes.Equal(before, readVaultBytes(t, path)) {
		t.Fatal("a declined question changed the vault")
	}
}

func TestParseAge(t *testing.T) {
	good := map[string]time.Duration{"30d": 30 * 24 * time.Hour, "12h": 12 * time.Hour, " 1d ": 24 * time.Hour, "90m": 90 * time.Minute}
	for in, want := range good {
		if got, err := parseAge(in); err != nil || got != want {
			t.Errorf("parseAge(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"", "0d", "-1d", "d", "1.5d", "100001d", "0s", "-3h", "soon"} {
		if _, err := parseAge(in); err == nil {
			t.Errorf("parseAge(%q) must fail", in)
		}
	}
}

func TestPromptsRunWithTheVaultLockReleased(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"purge", []string{"rm", "--purge", "dr"}},
		{"trash empty", []string{"trash", "empty"}},
		{"dedupe", []string{"dedupe"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := sealTrashVault(t)
			if c.name == "dedupe" {
				sealVault(t, path, []account.Account{totp("x", "Acme", "me", 1), totp("y", "Acme", "me", 2)})
			}
			stubStdinTTY(t, true)
			var lockErr error
			asked := false
			stubConfirm(t, func(string) (string, error) {
				asked = true
				l, err := store.TryAcquire(path)
				if err != nil {
					lockErr = err
					return "n", nil
				}
				lockErr = l.Release()
				return "y", nil
			})
			if _, _, err := runRootStreams(t, c.args...); err != nil {
				t.Fatalf("%v: %v", c.args, err)
			}
			if !asked {
				t.Fatal("no question was asked")
			}
			if lockErr != nil {
				t.Fatalf("the vault lock was held during the question: %v", lockErr)
			}
		})
	}
}

func TestAChangeWhileAskingCancelsTheWrite(t *testing.T) {
	cases := []struct {
		name   string
		args   []string
		change []string
	}{
		{"purge", []string{"rm", "--purge", "dr"}, []string{"restore", "dr"}},
		{"trash empty", []string{"trash", "empty"}, []string{"restore", "ac"}},
		{"dedupe", []string{"dedupe"}, []string{"note", "acme2", "changed"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := sealTrashVault(t)
			if c.name == "dedupe" {
				a, b := totp("x", "Acme", "me", 1), totp("y", "Acme", "me", 2)
				a.Handle, b.Handle = "acme", "acme2"
				sealVault(t, path, []account.Account{a, b})
			}
			stubStdinTTY(t, true)
			stubConfirm(t, func(string) (string, error) {
				if _, _, err := runRootStreams(t, c.change...); err != nil {
					t.Errorf("concurrent %v: %v", c.change, err)
				}
				return "y", nil
			})
			_, stderr, err := runRootStreams(t, c.args...)
			if !errors.Is(err, errChangedWhileAsking) {
				t.Fatalf("err = %v stderr = %q", err, stderr)
			}
			if !strings.Contains(stderr, "nothing was changed, run the command again") {
				t.Errorf("stderr = %q", stderr)
			}
			got := openVaultFile(t, path, "testpass123")
			if c.name == "dedupe" && len(account.Live(got)) != 2 {
				t.Fatalf("dedupe trashed after a change: %+v", got)
			}
			if c.name != "dedupe" && len(got) != 3 {
				t.Fatalf("an account was deleted after a change: %+v", got)
			}
		})
	}
}
