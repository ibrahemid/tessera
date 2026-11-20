package main

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/store"
)

const editSecretB32 = "JBSWY3DPEHPK3PXP"

var editSecretRaw = []byte("Hello!\xde\xad\xbe\xef")

func TestEditChangesSettings(t *testing.T) {
	cases := []struct {
		name  string
		start account.Account
		args  []string
		check func(t *testing.T, a account.Account)
	}{
		{"digits", handled("a", "GitHub", "me", "gi"), []string{"--digits", "8"}, func(t *testing.T, a account.Account) {
			if a.Digits != 8 {
				t.Errorf("digits = %d", a.Digits)
			}
		}},
		{"period", handled("a", "GitHub", "me", "gi"), []string{"--period", "60"}, func(t *testing.T, a account.Account) {
			if a.Period != 60 {
				t.Errorf("period = %d", a.Period)
			}
		}},
		{"algorithm", handled("a", "GitHub", "me", "gi"), []string{"--algorithm", "sha512"}, func(t *testing.T, a account.Account) {
			if a.Algorithm != "SHA512" {
				t.Errorf("algorithm = %q", a.Algorithm)
			}
		}},
		{"to hotp", handled("a", "GitHub", "me", "gi"), []string{"--type", "HOTP", "--counter", "12"}, func(t *testing.T, a account.Account) {
			if a.Type != account.HOTP || a.Counter != 12 {
				t.Errorf("type = %s counter = %d", a.Type, a.Counter)
			}
		}},
		{"hotp counter", hotpAccount(5), []string{"--counter", "0"}, func(t *testing.T, a account.Account) {
			if a.Counter != 0 {
				t.Errorf("counter = %d", a.Counter)
			}
		}},
		{"hotp to totp", hotpAccount(5), []string{"--type", "totp"}, func(t *testing.T, a account.Account) {
			if a.Type != account.TOTP || a.Counter != 0 || a.Period != 30 {
				t.Errorf("account = %+v", a)
			}
		}},
		{"to steam", handled("a", "GitHub", "me", "gi"), []string{"--type", "steam"}, func(t *testing.T, a account.Account) {
			if a.Type != account.Steam || a.Digits != 5 {
				t.Errorf("type = %s digits = %d", a.Type, a.Digits)
			}
		}},
		{"steam to totp", steamEditAccount(), []string{"--type", "totp"}, func(t *testing.T, a account.Account) {
			if a.Type != account.TOTP || a.Digits != 6 {
				t.Errorf("type = %s digits = %d", a.Type, a.Digits)
			}
		}},
		{"steam to totp with digits", steamEditAccount(), []string{"--type", "totp", "--digits", "7"}, func(t *testing.T, a account.Account) {
			if a.Digits != 7 {
				t.Errorf("digits = %d", a.Digits)
			}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := withVault(t)
			sealVault(t, path, []account.Account{c.start})
			stubClock(t, time.Unix(1_800_000_000, 0))
			stdout, _, err := runRootStreams(t, append([]string{"edit", "gi"}, c.args...)...)
			if err != nil || stdout != "Updated GitHub (me). Its codes change from now on.\n" {
				t.Fatalf("err = %v stdout = %q", err, stdout)
			}
			a := ids(openVaultFile(t, path, "testpass123"))["a"]
			if a.UpdatedAt != 1_800_000_000 {
				t.Errorf("updated_at = %d", a.UpdatedAt)
			}
			if err := a.Validate(); err != nil {
				t.Errorf("saved an invalid account: %v", err)
			}
			c.check(t, a)
		})
	}
}

func hotpAccount(counter int64) account.Account {
	a := handled("a", "GitHub", "me", "gi")
	a.Type, a.Counter = account.HOTP, counter
	return a
}

func steamEditAccount() account.Account {
	a := handled("a", "GitHub", "me", "gi")
	a.Type, a.Digits = account.Steam, 5
	return a
}

func TestEditRejectsInvalidSettings(t *testing.T) {
	cases := []struct {
		start account.Account
		args  []string
		want  string
	}{
		{handled("a", "GitHub", "me", "gi"), []string{"--digits", "5"}, "--digits must be 6, 7 or 8"},
		{handled("a", "GitHub", "me", "gi"), []string{"--digits", "9"}, "--digits must be 6, 7 or 8"},
		{handled("a", "GitHub", "me", "gi"), []string{"--period", "0"}, "--period must be a positive number of seconds"},
		{handled("a", "GitHub", "me", "gi"), []string{"--period=-30"}, "--period must be a positive number of seconds"},
		{handled("a", "GitHub", "me", "gi"), []string{"--algorithm", "md5"}, `invalid --algorithm "md5": use SHA1, SHA256 or SHA512`},
		{handled("a", "GitHub", "me", "gi"), []string{"--type", "sms"}, `invalid --type "sms": use totp, hotp or steam`},
		{handled("a", "GitHub", "me", "gi"), []string{"--counter", "3"}, "--counter applies to hotp accounts only"},
		{hotpAccount(1), []string{"--period", "60"}, "--period does not apply to hotp accounts"},
		{hotpAccount(1), []string{"--counter=-1"}, fmt.Sprintf("--counter must be from 0 to %d", account.MaxCounter)},
		{hotpAccount(1), []string{"--counter", strconv.FormatInt(account.MaxCounter+1, 10)}, fmt.Sprintf("--counter must be from 0 to %d", account.MaxCounter)},
		{steamEditAccount(), []string{"--digits", "6"}, "steam codes have 5 digits; leave out --digits"},
		{handled("a", "GitHub", "me", "gi"), []string{"--type", "steam", "--digits", "8"}, "steam codes have 5 digits; leave out --digits"},
		{handled("a", "GitHub", "me", "gi"), nil, "nothing to change: pass --digits, --period, --algorithm, --type, --counter or --secret"},
	}
	for _, c := range cases {
		t.Run(strings.Join(c.args, " "), func(t *testing.T) {
			path := withVault(t)
			sealVault(t, path, []account.Account{c.start})
			before := readVaultBytes(t, path)
			_, stderr, err := runRootStreams(t, append([]string{"edit", "gi"}, c.args...)...)
			if err == nil || stderr != "error: "+c.want+"\n" {
				t.Fatalf("err = %v stderr = %q", err, stderr)
			}
			if !bytes.Equal(before, readVaultBytes(t, path)) {
				t.Fatal("a rejected edit rewrote the vault")
			}
		})
	}
}

func TestEditWithTheSameSettingsLeavesTheVaultAlone(t *testing.T) {
	path := sealGitHub(t)
	before := readVaultBytes(t, path)
	stdout, _, err := runRootStreams(t, "edit", "gi", "--digits", "6", "--algorithm", "sha1")
	if err != nil || stdout != "GitHub (me) already has those settings\n" {
		t.Fatalf("err = %v stdout = %q", err, stdout)
	}
	if !bytes.Equal(before, readVaultBytes(t, path)) {
		t.Fatal("the vault was rewritten")
	}
}

func TestEditReadsTheSecretFromStdin(t *testing.T) {
	path := sealGitHub(t)
	stubStdin(t, " "+editSecretB32+"\n")
	if _, _, err := runRootStreams(t, "edit", "gi", "--secret"); err != nil {
		t.Fatalf("edit: %v", err)
	}
	if a := ids(openVaultFile(t, path, "testpass123"))["a"]; !bytes.Equal(a.Secret, editSecretRaw) {
		t.Fatalf("secret = %x", a.Secret)
	}
}

func TestEditPromptsForTheSecretWithTheVaultUnlocked(t *testing.T) {
	path := sealGitHub(t)
	stubStdinTTY(t, true)
	var shown []string
	var lockErr error
	old := promptHidden
	promptHidden = func(p string) (string, error) {
		shown = append(shown, p)
		l, err := store.TryAcquire(path)
		if err != nil {
			lockErr = err
			return "", err
		}
		lockErr = l.Release()
		return editSecretB32, nil
	}
	t.Cleanup(func() { promptHidden = old })
	stdout, stderr, err := runRootStreams(t, "edit", "gi", "--secret")
	if err != nil {
		t.Fatalf("err = %v stderr = %q", err, stderr)
	}
	if lockErr != nil {
		t.Fatalf("the vault lock was held during the prompt: %v", lockErr)
	}
	if len(shown) != 1 || shown[0] != "Secret: " {
		t.Fatalf("prompts = %q", shown)
	}
	if strings.Contains(stdout+stderr, editSecretB32) {
		t.Fatal("the secret was printed")
	}
	if a := ids(openVaultFile(t, path, "testpass123"))["a"]; !bytes.Equal(a.Secret, editSecretRaw) {
		t.Fatalf("secret = %x", a.Secret)
	}
}

func TestEditNeverTakesTheSecretFromArgs(t *testing.T) {
	for _, args := range [][]string{
		{"edit", "gi", "--secret=" + editSecretB32},
		{"edit", "gi", "--secret", editSecretB32},
		{"edit", "--secret", editSecretB32, "gi"},
	} {
		t.Run(strings.Join(args[1:], " "), func(t *testing.T) {
			path := sealGitHub(t)
			before := readVaultBytes(t, path)
			stdout, stderr, err := runRootStreams(t, args...)
			if !errors.Is(err, errSecretFlagValue) {
				t.Fatalf("err = %v stderr = %q", err, stderr)
			}
			if strings.Contains(stdout+stderr, editSecretB32) {
				t.Fatalf("the secret was echoed: %q", stderr)
			}
			if !bytes.Equal(before, readVaultBytes(t, path)) {
				t.Fatal("the vault was rewritten")
			}
		})
	}
}

func TestEditRejectsABadSecret(t *testing.T) {
	cases := map[string]error{"\n  \n": errEmptySecret, "not base32 at all!\n": nil}
	for in, want := range cases {
		path := sealGitHub(t)
		before := readVaultBytes(t, path)
		stubStdin(t, in)
		_, stderr, err := runRootStreams(t, "edit", "gi", "--secret")
		if err == nil || (want != nil && !errors.Is(err, want)) {
			t.Fatalf("%q: err = %v stderr = %q", in, err, stderr)
		}
		if want == nil && stderr != "error: invalid setup key: base32 uses A-Z and 2-7\n" {
			t.Errorf("%q: stderr = %q", in, stderr)
		}
		if !bytes.Equal(before, readVaultBytes(t, path)) {
			t.Fatalf("%q: the vault was rewritten", in)
		}
	}
}
