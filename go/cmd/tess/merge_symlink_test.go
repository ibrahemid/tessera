package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/store"
)

func TestTwoWayMergeWritesThroughASymlinkedSource(t *testing.T) {
	dst := withVault(t)
	sealVault(t, dst, []account.Account{totp("here", "Here", "a", 1)})
	target := filepath.Join(t.TempDir(), "other.json")
	sealAt(t, target, "testpass123", []account.Account{totp("there", "There", "b", 2)})
	linkDir := t.TempDir()
	link := filepath.Join(linkDir, "link.json")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	out := runMergeTwoWay(t, link)

	info, err := os.Lstat(link)
	if err != nil || info.Mode()&fs.ModeSymlink == 0 {
		t.Fatalf("the source link was replaced by a regular file (mode %v, err %v)", info.Mode(), err)
	}
	if _, ok := ids(openVaultFile(t, target, "testpass123"))["here"]; !ok {
		t.Fatal("the merge did not write into the file the link points at")
	}
	if _, err := os.Stat(store.LockPath(target)); err != nil {
		t.Errorf("no lock next to the link's target: %v", err)
	}
	if _, err := os.Stat(store.BackupPath(target)); err != nil {
		t.Errorf("no backup next to the link's target: %v", err)
	}
	entries, err := os.ReadDir(linkDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "link.json" {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Errorf("files written next to the link: %v", names)
	}
	if !strings.Contains(out, "Merged "+link+":") {
		t.Errorf("the report names %q, want the path as given: %q", out, link)
	}
}

func TestResolveMergeSourceNeverFallsBackToTheVaultPath(t *testing.T) {
	t.Setenv("TESSERA_VAULT", filepath.Join(t.TempDir(), "vault.json"))
	got, err := resolveMergeSource("")
	if err == nil || got != "" {
		t.Fatalf("resolveMergeSource(\"\") = %q, %v; want an error and no path", got, err)
	}
}

func TestResolveMergeSourceNamesTheArgumentInAPathError(t *testing.T) {
	_, err := resolveMergeSource(filepath.Join(t.TempDir(), "a\nb.json"))
	var ipe *store.InvalidPathError
	if !errors.As(err, &ipe) || ipe.Source != "the merge source argument" {
		t.Fatalf("err = %v, want an InvalidPathError naming the merge source argument", err)
	}
}

func TestTwoWayMergeWithADanglingSourceLinkNamesItsTarget(t *testing.T) {
	dst := withVault(t)
	sealVault(t, dst, []account.Account{totp("here", "Here", "a", 1)})
	missing := filepath.Join(t.TempDir(), "gone.json")
	linkDir := t.TempDir()
	link := filepath.Join(linkDir, "link.json")
	if err := os.Symlink(missing, link); err != nil {
		t.Fatal(err)
	}

	_, stderr, err := runRootStreams(t, "merge", "--two-way", link)
	if err == nil || !strings.Contains(stderr, link) || !strings.Contains(stderr, missing) {
		t.Fatalf("err = %v stderr = %q, want both the link and its target named", err, stderr)
	}
	if _, err := os.Lstat(store.LockPath(link)); err == nil {
		t.Error("a lock file was created next to a dangling source link")
	}
}
