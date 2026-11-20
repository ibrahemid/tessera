package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/ibrahemid/tessera/go/internal/account"
)

// sealDuplicates seals two copies of Acme, three of Slack, one GitHub, and a
// trashed copy of GitHub.
func sealDuplicates(t *testing.T) string {
	t.Helper()
	path := withVault(t)
	acmeOld, acmeNew := handled("a1", "Acme", "me", "ac"), handled("a2", "Acme", "me", "ac2")
	acmeOld.CreatedAt, acmeNew.CreatedAt = 100, 200
	s1, s2, s3 := handled("s1", "Slack", "me", "sl"), handled("s2", "Slack", "me", "sl2"), handled("s3", "Slack", "me", "sl3")
	s1.CreatedAt, s2.CreatedAt, s3.CreatedAt = 300, 50, 300
	sealVault(t, path, []account.Account{
		acmeNew, acmeOld, s1, s2, s3,
		handled("g", "GitHub", "me", "gi"),
		trashedAt(handled("g2", "GitHub", "me", "gi2"), dropboxTrashedAt),
	})
	return path
}

func TestDedupeKeepsTheOldestOfEachSet(t *testing.T) {
	path := sealDuplicates(t)
	stubClock(t, time.Unix(1_800_000_000, 0))
	stdout, _, err := runRootStreams(t, "dedupe", "--yes")
	if err != nil {
		t.Fatalf("dedupe: %v", err)
	}
	want := "Acme (me): keeping ac, moving ac2 to the Trash\n" +
		"Slack (me): keeping sl2, moving sl, sl3 to the Trash\n" +
		"Moved 3 duplicates to the Trash\n" +
		"Restore with `tess restore ac2 sl sl3`\n"
	if stdout != want {
		t.Fatalf("stdout = %q, want %q", stdout, want)
	}
	got := ids(openVaultFile(t, path, "testpass123"))
	for _, id := range []string{"a2", "s1", "s3"} {
		if got[id].DeletedAt != 1_800_000_000 || got[id].UpdatedAt != 1_800_000_000 {
			t.Errorf("%s not trashed: %+v", id, got[id])
		}
	}
	for _, id := range []string{"a1", "s2", "g"} {
		if got[id].Trashed() {
			t.Errorf("%s was trashed", id)
		}
	}
	if got["g2"].DeletedAt != dropboxTrashedAt {
		t.Errorf("the trashed GitHub copy changed: %+v", got["g2"])
	}

	stdout, _, err = runRootStreams(t, "dedupe")
	if err != nil || stdout != "No duplicates.\n" {
		t.Fatalf("second run: err = %v stdout = %q", err, stdout)
	}
}

func TestDedupeIgnoresAccountsThatDifferInSecretOrType(t *testing.T) {
	path := withVault(t)
	a, b, c := handled("a", "Acme", "me", "ac"), handled("b", "Acme", "me", "ac2"), handled("c", "Acme", "me", "ac3")
	b.Secret = []byte("another secret value")
	c.Type = account.HOTP
	sealVault(t, path, []account.Account{a, b, c})
	stdout, _, err := runRootStreams(t, "dedupe", "-y")
	if err != nil || stdout != "No duplicates.\n" {
		t.Fatalf("err = %v stdout = %q", err, stdout)
	}
}

func TestDedupeAsksFirst(t *testing.T) {
	path := sealDuplicates(t)
	before := readVaultBytes(t, path)
	_, stderr, err := runRootStreams(t, "dedupe")
	if err == nil || !strings.HasSuffix(stderr, "error: refusing to move duplicates to the Trash without --yes (stdin is not a terminal)\n") {
		t.Fatalf("err = %v stderr = %q", err, stderr)
	}

	stubStdinTTY(t, true)
	shown := stubPrompts(t, nil, []string{"n"})
	stdout, _, err := runRootStreams(t, "dedupe")
	if err != nil {
		t.Fatalf("declined: %v", err)
	}
	if len(*shown) != 1 || (*shown)[0] != "Move 3 duplicates to the Trash? [y/N] " {
		t.Fatalf("prompts = %q", *shown)
	}
	if strings.Contains(stdout, "Moved") {
		t.Errorf("a declined question reported a move: %q", stdout)
	}
	if !bytes.Equal(before, readVaultBytes(t, path)) {
		t.Fatal("a declined question changed the vault")
	}

	stubPrompts(t, nil, []string{"y"})
	if _, _, err := runRootStreams(t, "dedupe"); err != nil {
		t.Fatalf("accepted: %v", err)
	}
	if got := openVaultFile(t, path, "testpass123"); len(account.Live(got)) != 3 {
		t.Fatalf("live = %+v", account.Live(got))
	}
}
