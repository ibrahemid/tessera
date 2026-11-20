package store

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveThroughASymlinkedVaultUpdatesTheTarget(t *testing.T) {
	lowerArgon2(t)
	dir := t.TempDir()
	target := filepath.Join(dir, "synced", "vault.json")
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := Save(target, sealOne(t, "Old")); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "vault.json")
	symlinkOrSkip(t, target, link)
	path, err := Resolve(link)
	if err != nil {
		t.Fatal(err)
	}
	if err := Save(path, sealOne(t, "New")); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if got := openIssuer(t, target); got != "New" {
		t.Fatalf("write through a symlinked vault path left the target at %q; link is now a symlink=%v", got, info.Mode()&os.ModeSymlink != 0)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("the write replaced the link with a regular file")
	}
	if got := openIssuer(t, BackupPath(path)); got != "Old" {
		t.Fatalf("backup next to the target holds %q, want Old", got)
	}
	if _, err := os.Lstat(BackupPath(link)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a backup was written next to the link: %v", err)
	}
}

func TestResolveUsesTheLinkTargetForTheLock(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real", "vault.json")
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatal(err)
	}
	touch(t, target)
	hop := filepath.Join(dir, "hop.json")
	symlinkOrSkip(t, target, hop)
	link := filepath.Join(dir, "vault.json")
	symlinkOrSkip(t, "hop.json", link)

	viaLink, err := Resolve(link)
	if err != nil {
		t.Fatal(err)
	}
	direct, err := Resolve(target)
	if err != nil {
		t.Fatal(err)
	}
	same, err := os.Stat(direct)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.Stat(viaLink)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(same, got) {
		t.Fatalf("Resolve(link) = %s, not the target %s", viaLink, direct)
	}
	if info, err := os.Lstat(viaLink); err != nil || info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("Resolve(link) = %s is still a link (%v)", viaLink, err)
	}

	l, err := TryAcquire(viaLink)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Release()
	if _, err := TryAcquire(direct); !errors.Is(err, ErrLocked) {
		t.Fatalf("lock through the link and lock on the target are different locks: %v", err)
	}
}

func TestResolveRefusesADanglingLink(t *testing.T) {
	dir := t.TempDir()
	link := filepath.Join(dir, "vault.json")
	symlinkOrSkip(t, "gone.json", link)
	_, err := Resolve(link)
	var dangling *DanglingLinkError
	if !errors.As(err, &dangling) {
		t.Fatalf("err = %v, want *DanglingLinkError", err)
	}
	want := filepath.Join(dir, "gone.json")
	if dangling.Link != link || dangling.Target != want {
		t.Fatalf("link %q target %q, want %q and %q", dangling.Link, dangling.Target, link, want)
	}
	if msg := err.Error(); !strings.Contains(msg, link) || !strings.Contains(msg, want) {
		t.Fatalf("message %q does not name the link and its target", msg)
	}
}

func TestResolveKeepsAMissingPlainPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new", "vault.json")
	got, err := Resolve(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != path {
		t.Fatalf("Resolve = %q, want %q", got, path)
	}
}
