package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/store"
	"github.com/ibrahemid/tessera/go/internal/vault"
)

func accountIDs(accts []account.Account) string {
	ids := make([]string, len(accts))
	for i, a := range accts {
		ids[i] = a.ID
	}
	return strings.Join(ids, ",")
}

func TestVaultRestoreBakSwapsTheVaultAndItsBackup(t *testing.T) {
	path := withVault(t)
	sealVault(t, path, []account.Account{totp("a", "ACME", "x", 1)})
	sealVault(t, path, []account.Account{totp("a", "ACME", "x", 1), totp("b", "ACME", "y", 2)})

	stdout, stderr, err := runRootStreams(t, "vault", "restore", "--bak", "--yes")
	if err != nil {
		t.Fatalf("restore: %v\n%s", err, stderr)
	}
	if got := accountIDs(openVaultFile(t, path, "testpass123")); got != "a" {
		t.Errorf("vault after restore holds %q, want the backup's accounts", got)
	}
	if got := accountIDs(openVaultFile(t, store.BackupPath(path), "testpass123")); got != "a,b" {
		t.Errorf(".bak after restore holds %q, want the replaced vault", got)
	}
	for _, want := range []string{
		"Restored the vault at " + path + " from " + store.BackupPath(path) + " (1 account)",
		"The vault it replaced is at " + store.BackupPath(path),
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout lacks %q:\n%s", want, stdout)
		}
	}
}

func TestVaultRestoreFileWritesTheDayBackup(t *testing.T) {
	path := withVault(t)
	sealVault(t, path, nil)
	src := filepath.Join(t.TempDir(), "tessera-backup-20260301.json")
	sealAt(t, src, "testpass123", []account.Account{totp("a", "ACME", "x", 1)})
	dir := t.TempDir()
	t.Setenv(backupDirEnv, dir)

	if _, stderr, err := runRootStreams(t, "vault", "restore", src, "--yes"); err != nil {
		t.Fatalf("restore: %v\n%s", err, stderr)
	}
	if got := accountIDs(openVaultFile(t, path, "testpass123")); got != "a" {
		t.Errorf("vault after restore holds %q", got)
	}
	names, err := store.ListBackups(dir)
	if err != nil || len(names) != 1 {
		t.Fatalf("backup folder = %v, %v; want today's file", names, err)
	}
}

func TestVaultRestoreCreatesAMissingVault(t *testing.T) {
	path := withVault(t)
	src := filepath.Join(t.TempDir(), "saved.json")
	sealAt(t, src, "testpass123", []account.Account{totp("a", "ACME", "x", 1)})

	stdout, _, err := runRootStreams(t, "vault", "restore", src)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if got := accountIDs(openVaultFile(t, path, "testpass123")); got != "a" {
		t.Errorf("vault after restore holds %q", got)
	}
	if strings.Contains(stdout, "The vault it replaced") {
		t.Errorf("nothing was replaced:\n%s", stdout)
	}
}

func TestVaultRestoreNeedsYesOffATerminal(t *testing.T) {
	path := withVault(t)
	sealVault(t, path, nil)
	src := filepath.Join(t.TempDir(), "saved.json")
	sealAt(t, src, "testpass123", []account.Account{totp("a", "ACME", "x", 1)})
	before, _ := os.ReadFile(path)

	_, _, err := runRootStreams(t, "vault", "restore", src)
	if err == nil || !strings.Contains(err.Error(), "run again with --yes") {
		t.Fatalf("err = %v", err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Error("an unconfirmed restore must leave the vault untouched")
	}
}

func TestVaultRestoreAsksWithoutHoldingTheLock(t *testing.T) {
	for _, tc := range []struct {
		answer   string
		replaced bool
	}{{"n", false}, {"y", true}} {
		t.Run(tc.answer, func(t *testing.T) {
			path := withVault(t)
			sealVault(t, path, nil)
			src := filepath.Join(t.TempDir(), "saved.json")
			sealAt(t, src, "testpass123", []account.Account{totp("a", "ACME", "x", 1)})
			stubStdinTTY(t, true)
			var asked string
			old := promptLine
			promptLine = func(p string) (string, error) {
				asked = p
				lock, err := store.TryAcquire(path)
				if err != nil {
					t.Errorf("the vault lock is held while waiting for an answer: %v", err)
				} else if err := lock.Release(); err != nil {
					t.Error(err)
				}
				return tc.answer, nil
			}
			t.Cleanup(func() { promptLine = old })

			_, _, err := runRootStreams(t, "vault", "restore", src)
			if !strings.Contains(asked, "Replace the vault at "+path+" with "+src+" (1 account)?") {
				t.Errorf("prompt = %q", asked)
			}
			n := len(openVaultFile(t, path, "testpass123"))
			if tc.replaced {
				if err != nil || n != 1 {
					t.Errorf("err = %v, accounts = %d; want the vault replaced", err, n)
				}
				return
			}
			if !errors.Is(err, errRestoreDeclined) || n != 0 {
				t.Errorf("err = %v, accounts = %d; want the vault kept", err, n)
			}
		})
	}
}

func TestVaultRestoreRefusesAFileTheVariableDoesNotOpen(t *testing.T) {
	path := withVault(t)
	sealVault(t, path, nil)
	src := filepath.Join(t.TempDir(), "saved.json")
	sealAt(t, src, "another-passphrase", []account.Account{totp("a", "ACME", "x", 1)})

	_, stderr, err := runRootStreams(t, "vault", "restore", src, "--yes")
	if !errors.Is(err, vault.ErrWrongPassphrase) {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(stderr, "the passphrase in $TESSERA_PASSPHRASE doesn't open "+src) {
		t.Errorf("stderr = %q", stderr)
	}
	if n := len(openVaultFile(t, path, "testpass123")); n != 0 {
		t.Error("a file that does not open must not replace the vault")
	}
}

func TestVaultRestoreUsesTheStoredPassphrase(t *testing.T) {
	path := withVault(t)
	t.Setenv("TESSERA_PASSPHRASE", "")
	sealAt(t, path, "testpass123", nil)
	src := filepath.Join(t.TempDir(), "saved.json")
	sealAt(t, src, "testpass123", []account.Account{totp("a", "ACME", "x", 1)})
	fake := secretService()
	fake.Put(path, "testpass123")
	swapStores(t, fake)

	if _, stderr, err := runRootStreams(t, "vault", "restore", src, "--yes"); err != nil {
		t.Fatalf("restore: %v\n%s", err, stderr)
	}
	if n := len(openVaultFile(t, path, "testpass123")); n != 1 {
		t.Errorf("accounts after restore = %d", n)
	}
}

func TestVaultRestoreWithoutAPassphraseOffATerminal(t *testing.T) {
	withVault(t)
	t.Setenv("TESSERA_PASSPHRASE", "")
	swapStores(t)
	src := filepath.Join(t.TempDir(), "saved.json")
	sealAt(t, src, "testpass123", nil)

	if _, _, err := runRootStreams(t, "vault", "restore", src, "--yes"); !errors.Is(err, errNoUnlockTerminal) {
		t.Fatalf("err = %v, want errNoUnlockTerminal", err)
	}
}

func TestVaultRestoreRejectsBadSources(t *testing.T) {
	path := withVault(t)
	sealVault(t, path, nil)
	missing := filepath.Join(t.TempDir(), "gone.json")
	notVault := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(notVault, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"--yes"}, "name the file to restore, or pass --bak"},
		{[]string{missing, "--bak", "--yes"}, "name a file or pass --bak, not both"},
		{[]string{missing, "--yes"}, "no file at " + missing},
		{[]string{"--bak", "--yes"}, "there is no " + store.BackupPath(path)},
		{[]string{path, "--yes"}, "is the vault itself"},
		{[]string{notVault, "--yes"}, "cannot read " + notVault},
	} {
		_, stderr, err := runRootStreams(t, append([]string{"vault", "restore"}, tc.args...)...)
		if err == nil || !strings.Contains(stderr, tc.want) {
			t.Errorf("restore %v: err = %v, stderr = %q; want %q", tc.args, err, stderr, tc.want)
		}
	}
}

func TestUnlockFailureNamesTheBak(t *testing.T) {
	path := withVault(t)
	initVault(t)
	t.Setenv("TESSERA_PASSPHRASE", "wrong-passphrase")

	_, stderr, err := runRootStreams(t, "list")
	if !errors.Is(err, vault.ErrWrongPassphrase) {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(stderr, "restore") {
		t.Errorf("without a .bak there is nothing to restore:\n%s", stderr)
	}

	resaveVault(t, path)
	_, stderr, err = runRootStreams(t, "list")
	if !errors.Is(err, vault.ErrWrongPassphrase) {
		t.Fatalf("err = %v", err)
	}
	for _, want := range []string{
		"the passphrase in $TESSERA_PASSPHRASE doesn't open " + path,
		"If the passphrase is right, the vault file is damaged",
		store.BackupPath(path),
		"restore it with `tess vault restore --bak --vault " + quoteArg(path) + "`",
	} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr lacks %q:\n%s", want, stderr)
		}
	}
}

func TestUnreadableVaultNamesTheRestoreCommand(t *testing.T) {
	path := withVault(t)
	initVault(t)
	resaveVault(t, path)
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, stderr, err := runRootStreams(t, "list")
	if err == nil {
		t.Fatal("list must fail on an unreadable vault")
	}
	if !strings.Contains(stderr, "cannot read the vault at "+path) || !strings.Contains(stderr, "`tess vault restore --bak --vault "+quoteArg(path)+"`") {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestRestoreCommandQuotesThePath(t *testing.T) {
	plain := filepath.Join(t.TempDir(), "vault.json")
	t.Setenv("TESSERA_VAULT", plain)
	if got := restoreCommand(plain); got != "tess vault restore --bak" {
		t.Errorf("default vault: %q", got)
	}
	spaced := filepath.Join(t.TempDir(), "my vault's.json")
	want := "tess vault restore --bak --vault '" + strings.ReplaceAll(spaced, "'", `'\''`) + "'"
	if runtime.GOOS == "windows" {
		want = `tess vault restore --bak --vault "` + spaced + `"`
	}
	if got := restoreCommand(spaced); got != want {
		t.Errorf("restoreCommand(%q) = %q, want %q", spaced, got, want)
	}
}

func TestQuotePOSIX(t *testing.T) {
	for in, want := range map[string]string{
		"/tmp/vault.json":               "/tmp/vault.json",
		"/tmp/a@b%c,d+e=f~g:h-i_j.json": "/tmp/a@b%c,d+e=f~g:h-i_j.json",
		"/home/me/my vault.json":        "'/home/me/my vault.json'",
		"/tmp/it's.json":                `'/tmp/it'\''s.json'`,
		"/tmp/$HOME/`id`.json":          "'/tmp/$HOME/`id`.json'",
		`C:\Users\me\vault.json`:        `'C:\Users\me\vault.json'`,
		"":                              "''",
	} {
		if got := quotePOSIX(in); got != want {
			t.Errorf("quotePOSIX(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestQuotePowerShell(t *testing.T) {
	for in, want := range map[string]string{
		`C:\Users\RUNNER~1\AppData\Local\Temp\vault.json`: `C:\Users\RUNNER~1\AppData\Local\Temp\vault.json`,
		`\\server\share\vault.json`:                       `\\server\share\vault.json`,
		`C:/Users/me/vault+1=a-b_c.json`:                  `C:/Users/me/vault+1=a-b_c.json`,
		`C:\My Files\vault.json`:                          `"C:\My Files\vault.json"`,
		`C:\Users\o'brien\vault.json`:                     `"C:\Users\o'brien\vault.json"`,
		`C:\a&b (1)\[x]{y};#!^\vault.json`:                `"C:\a&b (1)\[x]{y};#!^\vault.json"`,
		`C:\100%\vault.json`:                              `"C:\100%\vault.json"`,
		`C:\a,b\vault.json`:                               `"C:\a,b\vault.json"`,
		`C:\@home\vault.json`:                             `"C:\@home\vault.json"`,
		"C:\\Users\\J\u00f6rg\\vault.json":                "\"C:\\Users\\J\u00f6rg\\vault.json\"",
		`C:\$env\vault.json`:                              `'C:\$env\vault.json'`,
		"C:\\a`b\\vault.json":                             "'C:\\a`b\\vault.json'",
		`C:\it's $x\vault.json`:                           `'C:\it''s $x\vault.json'`,
		"C:\\it\u2019s $x\\vault.json":                    "'C:\\it\u2019\u2019s $x\\vault.json'",
		"C:\\\u201cq\u201d\\vault.json":                   "'C:\\\u201cq\u201d\\vault.json'",
		"C:\\\u201eq\u2018\\vault.json":                   "'C:\\\u201eq\u2018\u2018\\vault.json'",
		`C:\say "hi"\vault.json`:                          `'C:\say "hi"\vault.json'`,
		"":                                                `""`,
	} {
		if got := quotePowerShell(in); got != want {
			t.Errorf("quotePowerShell(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestQuoteArgFollowsThePlatform(t *testing.T) {
	in := "dir/my vault.json"
	want := quotePOSIX(in)
	if runtime.GOOS == "windows" {
		want = quotePowerShell(in)
	}
	if got := quoteArg(in); got != want {
		t.Errorf("quoteArg(%q) = %q, want %q", in, got, want)
	}
}

func TestVaultLoadRefusesAnOversizedFile(t *testing.T) {
	path := withVault(t)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(store.MaxVaultSize + 1); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	_, stderr, err := runRootStreams(t, "list")
	var tooLarge *store.TooLargeError
	if !errors.As(err, &tooLarge) {
		t.Fatalf("err = %v, want a TooLargeError", err)
	}
	if !strings.Contains(stderr, "larger than 64 MiB") {
		t.Errorf("stderr = %q", stderr)
	}
}
