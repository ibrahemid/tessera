package main

import (
	"bytes"
	"os"
	"strings"
	"testing"

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

func TestRemoveAsksOnATerminal(t *testing.T) {
	path := sealGitHub(t)
	stubStdinTTY(t, true)
	shown := stubPrompts(t, nil, []string{"n", "y"})

	stdout, _, err := runRootStreams(t, "rm", "gi")
	if err != nil || stdout != "" {
		t.Fatalf("a declined removal is quiet: err = %v stdout = %q", err, stdout)
	}
	if got := openVaultFile(t, path, "testpass123"); len(got) != 1 {
		t.Fatalf("declining must keep the account, got %d", len(got))
	}
	want := "Remove GitHub (me) (gi)? Its secret is deleted from the vault. [y/N] "
	if len(*shown) != 1 || (*shown)[0] != want {
		t.Fatalf("prompts = %q, want %q", *shown, want)
	}

	stdout, _, err = runRootStreams(t, "rm", "gi")
	if err != nil {
		t.Fatalf("rm: %v", err)
	}
	if stdout != "Removed GitHub (me)\n" {
		t.Errorf("stdout = %q", stdout)
	}
	if got := openVaultFile(t, path, "testpass123"); len(got) != 0 {
		t.Fatalf("confirming must remove the account, got %d", len(got))
	}
}

func TestRemoveRefusesWithoutYesOffATerminal(t *testing.T) {
	path := sealGitHub(t)
	before := readVaultBytes(t, path)
	_, stderr, err := runRootStreams(t, "rm", "gi")
	if err == nil {
		t.Fatal("rm without --yes off a terminal must fail")
	}
	want := "error: refusing to remove GitHub (me) without --yes (stdin is not a terminal)\n"
	if stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
	if !bytes.Equal(before, readVaultBytes(t, path)) {
		t.Fatal("a refused removal rewrote the vault")
	}
}

func TestRemoveYesSkipsTheQuestion(t *testing.T) {
	for _, args := range [][]string{{"rm", "--yes", "gi"}, {"rm", "-y", "gi"}, {"remove", "-y", "gi"}, {"delete", "-y", "gi"}} {
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
				t.Fatalf("account not removed, %d left", len(got))
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
