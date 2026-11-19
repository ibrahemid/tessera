package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ibrahemid/tessera/go/internal/account"
)

func sealGitHub(t *testing.T) string {
	t.Helper()
	path := withVault(t)
	sealVault(t, path, []account.Account{handled("a", "GitHub", "me", "gi")})
	return path
}

func readVaultBytes(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestRemoveMovesToTheTrashWithoutAsking(t *testing.T) {
	path := sealGitHub(t)
	stubStdinTTY(t, true)
	shown := stubPrompts(t, nil, nil)
	stubClock(t, time.Unix(1_800_000_000, 0))

	stdout, _, err := runRootStreams(t, "rm", "gi")
	if err != nil {
		t.Fatalf("rm: %v", err)
	}
	if len(*shown) != 0 {
		t.Errorf("moving to the Trash must not ask, prompted %q", *shown)
	}
	want := "Moved GitHub (me) to the Trash\nRestore with `tess restore gi`\n"
	if stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
	got := openVaultFile(t, path, "testpass123")
	if len(got) != 1 {
		t.Fatalf("the account must stay in the vault, got %d", len(got))
	}
	if got[0].DeletedAt != 1_800_000_000 || got[0].UpdatedAt != 1_800_000_000 || got[0].Handle != "gi" {
		t.Errorf("trashed = %+v", got[0])
	}
}

func TestRemoveAcceptsYesAndWorksOffATerminal(t *testing.T) {
	for _, args := range [][]string{{"rm", "gi"}, {"rm", "--yes", "gi"}, {"rm", "-y", "gi"}, {"remove", "-y", "gi"}, {"delete", "-y", "gi"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			path := sealGitHub(t)
			if _, _, err := runRootStreams(t, args...); err != nil {
				t.Fatalf("%v: %v", args, err)
			}
			if got := openVaultFile(t, path, "testpass123"); len(got) != 1 || !got[0].Trashed() {
				t.Fatalf("account not trashed: %+v", got)
			}
		})
	}
}

func TestRemovePurgeAsksOnATerminal(t *testing.T) {
	path := sealGitHub(t)
	stubStdinTTY(t, true)
	shown := stubPrompts(t, nil, []string{"n", "y"})

	stdout, _, err := runRootStreams(t, "rm", "--purge", "gi")
	if err != nil || stdout != "" {
		t.Fatalf("a declined purge is quiet: err = %v stdout = %q", err, stdout)
	}
	if got := openVaultFile(t, path, "testpass123"); len(got) != 1 || got[0].Trashed() {
		t.Fatalf("declining must keep the account live, got %+v", got)
	}
	want := "Delete GitHub (me) (gi) for good? Its secret is removed from the vault. [y/N] "
	if len(*shown) != 1 || (*shown)[0] != want {
		t.Fatalf("prompts = %q, want %q", *shown, want)
	}

	stdout, _, err = runRootStreams(t, "rm", "--purge", "gi")
	if err != nil {
		t.Fatalf("rm --purge: %v", err)
	}
	if stdout != "Deleted GitHub (me) for good\n" {
		t.Errorf("stdout = %q", stdout)
	}
	if got := openVaultFile(t, path, "testpass123"); len(got) != 0 {
		t.Fatalf("confirming must delete the account, got %d", len(got))
	}
}

func TestRemovePurgeRefusesWithoutYesOffATerminal(t *testing.T) {
	path := sealGitHub(t)
	before := readVaultBytes(t, path)
	_, stderr, err := runRootStreams(t, "rm", "--purge", "gi")
	if err == nil {
		t.Fatal("rm --purge without --yes off a terminal must fail")
	}
	want := "error: refusing to delete GitHub (me) for good without --yes (stdin is not a terminal)\n"
	if stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
	if !bytes.Equal(before, readVaultBytes(t, path)) {
		t.Fatal("a refused purge rewrote the vault")
	}
}

func TestRemovePurgeYesSkipsTheQuestion(t *testing.T) {
	for _, args := range [][]string{{"rm", "--purge", "--yes", "gi"}, {"rm", "--purge", "-y", "gi"}, {"delete", "--purge", "-y", "gi"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			path := sealGitHub(t)
			stubStdinTTY(t, true)
			shown := stubPrompts(t, nil, nil)
			if _, _, err := runRootStreams(t, args...); err != nil {
				t.Fatalf("%v: %v", args, err)
			}
			if len(*shown) != 0 {
				t.Errorf("--yes must not ask, prompted %q", *shown)
			}
			if got := openVaultFile(t, path, "testpass123"); len(got) != 0 {
				t.Fatalf("account not deleted, %d left", len(got))
			}
		})
	}
}

func TestRenameWithoutFlagsChangesNothing(t *testing.T) {
	path := sealGitHub(t)
	before := readVaultBytes(t, path)
	_, stderr, err := runRootStreams(t, "rename", "gi")
	if err == nil {
		t.Fatal("rename with no flags must fail")
	}
	if stderr != "error: nothing to change: pass --issuer, --account or --alias\n" {
		t.Errorf("stderr = %q", stderr)
	}
	if !bytes.Equal(before, readVaultBytes(t, path)) {
		t.Fatal("rename with no flags rewrote the vault")
	}
}

func TestRenameSetsTheAlias(t *testing.T) {
	path := sealGitHub(t)
	stdout, _, err := runRootStreams(t, "rename", "gi", "--alias", "gh")
	if err != nil {
		t.Fatalf("rename --alias: %v", err)
	}
	if stdout != "Renamed GitHub (me) (alias gh)\n" {
		t.Errorf("stdout = %q", stdout)
	}
	if _, _, err := runRootStreams(t, "rename", "gh", "--handle", "gx"); err != nil {
		t.Fatalf("rename --handle: %v", err)
	}
	if got := openVaultFile(t, path, "testpass123"); got[0].Handle != "gx" {
		t.Fatalf("alias = %q, want gx", got[0].Handle)
	}
	f := newRenameCmd().Flags()
	if f.Lookup("alias").Hidden || !f.Lookup("handle").Hidden {
		t.Error("--alias must be listed and --handle hidden")
	}
}

func TestAliasErrorsSayAlias(t *testing.T) {
	withVault(t)
	sealVault(t, vaultPath, []account.Account{handled("a", "GitHub", "me", "gi"), handled("b", "ACME", "x", "ac")})
	_, stderr, _ := runRootStreams(t, "alias", "gi", "ac")
	if !strings.Contains(stderr, `alias "ac" already used by ACME (x)`) {
		t.Errorf("stderr = %q", stderr)
	}
	_, stderr, _ = runRootStreams(t, "alias", "gi", "9x")
	if !strings.Contains(stderr, `invalid alias "9x"`) {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestTagRemoveOfAnAbsentTagSaysSo(t *testing.T) {
	path := sealGitHub(t)
	before := readVaultBytes(t, path)
	stdout, _, err := runRootStreams(t, "tag", "gi", "--remove", "work")
	if err != nil {
		t.Fatalf("tag --remove: %v", err)
	}
	if stdout != "GitHub (me) has no tag work\n" {
		t.Errorf("stdout = %q", stdout)
	}
	if !bytes.Equal(before, readVaultBytes(t, path)) {
		t.Fatal("removing an absent tag rewrote the vault")
	}
}

func TestListOfAnEmptyVaultSaysHowToAdd(t *testing.T) {
	emptyVault(t)
	stdout, _, err := runRootStreams(t, "list")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(stdout, "No accounts. Add one with `tess add`.") {
		t.Errorf("stdout = %q", stdout)
	}
}
