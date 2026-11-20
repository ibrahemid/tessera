package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ibrahemid/tessera/go/internal/account"
)

func TestNoteSetShowClear(t *testing.T) {
	path := sealGitHub(t)
	stubClock(t, time.Unix(1_800_000_000, 0))
	stdout, _, err := runRootStreams(t, "note", "gi", "recovery codes in the safe")
	if err != nil || stdout != "Saved the note on GitHub (me)\n" {
		t.Fatalf("err = %v stdout = %q", err, stdout)
	}
	a := ids(openVaultFile(t, path, "testpass123"))["a"]
	if a.Note != "recovery codes in the safe" || a.UpdatedAt != 1_800_000_000 {
		t.Fatalf("account = %+v", a)
	}

	before := readVaultBytes(t, path)
	stdout, _, err = runRootStreams(t, "note", "gi", "recovery codes in the safe")
	if err != nil || stdout != "GitHub (me) already has that note\n" {
		t.Fatalf("err = %v stdout = %q", err, stdout)
	}
	if !bytes.Equal(before, readVaultBytes(t, path)) {
		t.Fatal("an unchanged note rewrote the vault")
	}

	stdout, _, err = runRootStreams(t, "show", "gi")
	if err != nil || !strings.Contains(stdout, "note       recovery codes in the safe\n") {
		t.Fatalf("show: err = %v stdout = %q", err, stdout)
	}
	stdout, _, err = runRootStreams(t, "list")
	if err != nil || strings.Contains(stdout, "recovery") {
		t.Fatalf("list prints the note: err = %v stdout = %q", err, stdout)
	}
	stdout, _, err = runRootStreams(t, "list", "--json")
	if err != nil || strings.Contains(stdout, "recovery") {
		t.Fatalf("list --json prints the note: err = %v stdout = %q", err, stdout)
	}

	stdout, _, err = runRootStreams(t, "note", "--clear", "gi")
	if err != nil || stdout != "Cleared the note on GitHub (me)\n" {
		t.Fatalf("err = %v stdout = %q", err, stdout)
	}
	if a := ids(openVaultFile(t, path, "testpass123"))["a"]; a.Note != "" {
		t.Fatalf("note left = %q", a.Note)
	}
	stdout, _, err = runRootStreams(t, "note", "--clear", "gi")
	if err != nil || stdout != "GitHub (me) has no note\n" {
		t.Fatalf("err = %v stdout = %q", err, stdout)
	}
	stdout, _, err = runRootStreams(t, "show", "gi")
	if err != nil || strings.Contains(stdout, "note") {
		t.Fatalf("show prints an empty note row: %q", stdout)
	}
}

func TestNoteReadsStdin(t *testing.T) {
	path := sealGitHub(t)
	stubStdin(t, "first line\r\nsecond\tline\r\n")
	if _, _, err := runRootStreams(t, "note", "gi", "-"); err != nil {
		t.Fatalf("note -: %v", err)
	}
	if a := ids(openVaultFile(t, path, "testpass123"))["a"]; a.Note != "first line\nsecond\tline" {
		t.Fatalf("note = %q", a.Note)
	}
	stdout, _, err := runRootStreams(t, "show", "gi")
	if err != nil || !strings.Contains(stdout, "note       first line\n           second    line\n") {
		t.Fatalf("show: err = %v stdout = %q", err, stdout)
	}
}

func TestNoteIsCleaned(t *testing.T) {
	path := sealGitHub(t)
	if _, _, err := runRootStreams(t, "note", "gi", "  \x1b]0;title\x07pin\u202e 1234\x00  "); err != nil {
		t.Fatalf("note: %v", err)
	}
	if a := ids(openVaultFile(t, path, "testpass123"))["a"]; a.Note != "]0;titlepin 1234" {
		t.Fatalf("note = %q", a.Note)
	}
	stdout, _, err := runRootStreams(t, "show", "gi")
	if err != nil {
		t.Fatalf("show: %v", err)
	}
	assertNoRawControl(t, "show", stdout)
}

func TestNoteRejectsBadInput(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"note", "gi", " \x1b\x07 "}, "error: the note is empty; --clear removes a note\n"},
		{[]string{"note", "gi"}, "error: pass the note text, - to read it from stdin, or --clear\n"},
		{[]string{"note", "--clear", "gi", "text"}, "error: --clear takes no note text\n"},
	}
	for _, c := range cases {
		t.Run(strings.Join(c.args, " "), func(t *testing.T) {
			path := sealGitHub(t)
			before := readVaultBytes(t, path)
			_, stderr, err := runRootStreams(t, c.args...)
			if err == nil || stderr != c.want {
				t.Fatalf("err = %v stderr = %q", err, stderr)
			}
			if !bytes.Equal(before, readVaultBytes(t, path)) {
				t.Fatal("the vault was rewritten")
			}
		})
	}
	t.Run("empty stdin", func(t *testing.T) {
		sealGitHub(t)
		stubStdin(t, "\n\t\n")
		if _, _, err := runRootStreams(t, "note", "gi", "-"); !errors.Is(err, errEmptyNote) {
			t.Fatalf("err = %v", err)
		}
	})
}

// sealOrderVault seals four live accounts and one trashed one.
func sealOrderVault(t *testing.T) string {
	t.Helper()
	path := withVault(t)
	sealVault(t, path, []account.Account{
		handled("a", "Alpha", "me", "al"),
		handled("b", "Bravo", "me", "br"),
		handled("c", "Charlie", "me", "ch"),
		handled("d", "Delta", "me", "de"),
		trashedAt(handled("e", "Echo", "me", "ec"), dropboxTrashedAt),
	})
	return path
}

// assertListOrder fails unless the issuers appear in stdout of tess list in
// exactly this order.
func assertListOrder(t *testing.T, want ...string) {
	t.Helper()
	stdout, _, err := runRootStreams(t, "list")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var got []string
	for _, line := range strings.Split(stdout, "\n") {
		for _, issuer := range []string{"Alpha", "Bravo", "Charlie", "Delta", "Echo"} {
			if strings.Contains(line, issuer) {
				got = append(got, issuer)
			}
		}
	}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("list order = %v, want %v\n%s", got, want, stdout)
	}
}

func TestPinMovesAccountsFirst(t *testing.T) {
	path := sealOrderVault(t)
	stubClock(t, time.Unix(1_800_000_000, 0))
	stdout, _, err := runRootStreams(t, "pin", "de", "ch")
	if err != nil || stdout != "Pinned Delta (me)\nPinned Charlie (me)\n" {
		t.Fatalf("err = %v stdout = %q", err, stdout)
	}
	assertListOrder(t, "Charlie", "Delta", "Alpha", "Bravo")
	if a := ids(openVaultFile(t, path, "testpass123"))["d"]; !a.Pinned || a.UpdatedAt != 1_800_000_000 {
		t.Fatalf("pinned = %+v", a)
	}

	before := readVaultBytes(t, path)
	stdout, _, err = runRootStreams(t, "pin", "de")
	if err != nil || stdout != "Delta (me) is already pinned\n" {
		t.Fatalf("err = %v stdout = %q", err, stdout)
	}
	if !bytes.Equal(before, readVaultBytes(t, path)) {
		t.Fatal("pinning a pinned account rewrote the vault")
	}

	stdout, _, err = runRootStreams(t, "unpin", "de", "al")
	if err != nil || stdout != "Unpinned Delta (me)\nAlpha (me) is not pinned\n" {
		t.Fatalf("err = %v stdout = %q", err, stdout)
	}
	assertListOrder(t, "Charlie", "Alpha", "Bravo", "Delta")
}

func TestOrderMovesAnAccount(t *testing.T) {
	path := sealOrderVault(t)
	stubClock(t, time.Unix(1_800_000_000, 0))
	stdout, _, err := runRootStreams(t, "order", "de", "1")
	if err != nil || stdout != "Moved Delta (me) to position 1\n" {
		t.Fatalf("err = %v stdout = %q", err, stdout)
	}
	assertListOrder(t, "Delta", "Alpha", "Bravo", "Charlie")
	got := ids(openVaultFile(t, path, "testpass123"))
	for id, want := range map[string]int{"d": 1, "a": 2, "b": 3, "c": 4, "e": 0} {
		if got[id].Order != want {
			t.Errorf("order of %s = %d, want %d", id, got[id].Order, want)
		}
		if got[id].UpdatedAt == 1_800_000_000 {
			t.Errorf("a reorder changed updated_at of %s", id)
		}
	}

	if _, _, err := runRootStreams(t, "order", "al", "4"); err != nil {
		t.Fatalf("order: %v", err)
	}
	assertListOrder(t, "Delta", "Bravo", "Charlie", "Alpha")

	stdout, _, err = runRootStreams(t, "order", "--clear", "al")
	if err != nil || stdout != "Cleared the position of Alpha (me)\n" {
		t.Fatalf("err = %v stdout = %q", err, stdout)
	}
	assertListOrder(t, "Delta", "Bravo", "Charlie", "Alpha")
	if a := ids(openVaultFile(t, path, "testpass123"))["a"]; a.Order != 0 {
		t.Fatalf("order left = %d", a.Order)
	}
	stdout, _, err = runRootStreams(t, "order", "--clear", "al")
	if err != nil || stdout != "Alpha (me) has no set position\n" {
		t.Fatalf("err = %v stdout = %q", err, stdout)
	}
}

func TestOrderKeepsPinnedAccountsFirst(t *testing.T) {
	sealOrderVault(t)
	if _, _, err := runRootStreams(t, "pin", "ch"); err != nil {
		t.Fatalf("pin: %v", err)
	}
	stdout, _, err := runRootStreams(t, "order", "de", "1")
	if err != nil || stdout != "Moved Delta (me) to position 2; pinned accounts stay first\n" {
		t.Fatalf("err = %v stdout = %q", err, stdout)
	}
	assertListOrder(t, "Charlie", "Delta", "Alpha", "Bravo")
}

func TestOrderRejectsABadPosition(t *testing.T) {
	for _, pos := range []string{"0", "5", "-1", "two", "1.5"} {
		t.Run(pos, func(t *testing.T) {
			path := sealOrderVault(t)
			before := readVaultBytes(t, path)
			_, stderr, err := runRootStreams(t, "order", "de", "--", pos)
			if err == nil || stderr != "error: position must be a whole number from 1 to 4\n" {
				t.Fatalf("err = %v stderr = %q", err, stderr)
			}
			if !bytes.Equal(before, readVaultBytes(t, path)) {
				t.Fatal("the vault was rewritten")
			}
		})
	}
}

func TestCodeListingFollowsDisplayOrder(t *testing.T) {
	sealOrderVault(t)
	if _, _, err := runRootStreams(t, "order", "ch", "1"); err != nil {
		t.Fatalf("order: %v", err)
	}
	if _, _, err := runRootStreams(t, "pin", "br"); err != nil {
		t.Fatalf("pin: %v", err)
	}
	stdout, _, err := runRootStreams(t, "code")
	if err != nil {
		t.Fatalf("code: %v", err)
	}
	b, c, a := strings.Index(stdout, "Bravo"), strings.Index(stdout, "Charlie"), strings.Index(stdout, "Alpha")
	if b < 0 || c < 0 || a < 0 || b > c || c > a || strings.Contains(stdout, "Echo") {
		t.Fatalf("code listing order is wrong:\n%s", stdout)
	}
}
