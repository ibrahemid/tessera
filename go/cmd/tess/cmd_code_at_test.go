package main

import (
	"bytes"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/spectest"
)

func failOnCopy(t *testing.T) {
	t.Helper()
	stubClipboard(t, func(string) error {
		t.Error("the clipboard was written")
		return nil
	})
}

func TestCodeAllNeverCopiesOrAdvances(t *testing.T) {
	path := sealOutputVault(t)
	stubClock(t, goldenTime)
	stubTTY(t, true)
	failOnCopy(t)
	before := readVaultBytes(t, path)
	for _, args := range [][]string{{"code", "--all"}, {"code", "--all", "--json"}, {"code"}} {
		stdout, _, err := runRootStreams(t, args...)
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if strings.Contains(stdout, "287922") {
			t.Fatalf("%v showed the code after the stored counter:\n%s", args, stdout)
		}
	}
	if !bytes.Equal(before, readVaultBytes(t, path)) {
		t.Fatal("tess code --all wrote the vault")
	}
}

func TestCodeAllPrintsEveryLiveAccountInDisplayOrder(t *testing.T) {
	sealOutputVault(t)
	stubClock(t, goldenTime)
	stdout, _, err := runRootStreams(t, "code", "--all")
	if err != nil {
		t.Fatal(err)
	}
	ze, ac, gh := strings.Index(stdout, "Zeta"), strings.Index(stdout, "Acme"), strings.Index(stdout, "GitHub")
	if ze < 0 || ac < ze || gh < ac {
		t.Fatalf("want Zeta, Acme, GitHub in that order:\n%s", stdout)
	}
	if strings.Contains(stdout, "Dropbox") {
		t.Fatalf("a trashed account is listed:\n%s", stdout)
	}
	if strings.Count(stdout, "081 804") != 2 {
		t.Fatalf("want both time-based codes:\n%s", stdout)
	}
}

func TestCodeAllRejectsFlagsForOneAccount(t *testing.T) {
	sealOutputVault(t)
	cases := []struct {
		args []string
		want error
	}{
		{[]string{"code", "gh", "--all"}, errAllWithQuery},
		{[]string{"code", "--all", "--next"}, errAllWithNext},
		{[]string{"code", "--all", "-c"}, errAllWithCopy},
	}
	for _, c := range cases {
		if _, _, err := runRootStreams(t, c.args...); !errors.Is(err, c.want) {
			t.Errorf("%v = %v, want %v", c.args, err, c.want)
		}
	}
}

// TestCodeAtMatchesRFC6238 checks --at against every RFC 6238 vector, given as
// unix seconds and as an RFC 3339 time.
func TestCodeAtMatchesRFC6238(t *testing.T) {
	var v struct {
		TOTP struct {
			Cases []struct {
				Algorithm   string   `json:"algorithm"`
				SecretASCII string   `json:"secret_ascii"`
				Times       []int64  `json:"times"`
				Codes       []string `json:"codes"`
			} `json:"cases"`
		} `json:"totp_rfc6238"`
	}
	spectest.Load(t, &v)
	if len(v.TOTP.Cases) == 0 {
		t.Fatal("no RFC 6238 vectors loaded")
	}
	for _, c := range v.TOTP.Cases {
		t.Run(c.Algorithm, func(t *testing.T) {
			path := withVault(t)
			a := handled("r", "RFC", "6238", "rf")
			a.Secret, a.Algorithm, a.Digits = []byte(c.SecretASCII), c.Algorithm, 8
			sealVault(t, path, []account.Account{a})
			for i, ts := range c.Times {
				for _, at := range []string{strconv.FormatInt(ts, 10), time.Unix(ts, 0).UTC().Format(time.RFC3339)} {
					stdout, _, err := runCodeCmdStreams(t, "rf", "--at", at)
					if err != nil {
						t.Fatalf("--at %s: %v", at, err)
					}
					if stdout != c.Codes[i]+"\n" {
						t.Errorf("--at %s = %q, want %s", at, stdout, c.Codes[i])
					}
				}
			}
		})
	}
}

func TestCodeAtJSONIsTheRowAtThatTime(t *testing.T) {
	sealOutputVault(t)
	stubClock(t, time.Unix(1234567890, 0))
	stdout, _, err := runCodeCmdStreams(t, "gh", "--at", "2005-03-18T01:58:29Z", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, `"code": "081804"`) || !strings.Contains(stdout, `"next_code": "050471"`) ||
		!strings.Contains(stdout, `"expires_in": 1`) {
		t.Fatalf("want the code at 1111111109:\n%s", stdout)
	}
}

func TestCodeAtNeverCopies(t *testing.T) {
	path := sealOutputVault(t)
	stubTTY(t, true)
	failOnCopy(t)
	before := readVaultBytes(t, path)
	stdout, _, err := runCodeCmdStreams(t, "gh", "--at", "1111111109")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "081 804") || strings.Contains(stdout, "copied") {
		t.Fatalf("stdout = %q", stdout)
	}
	if !bytes.Equal(before, readVaultBytes(t, path)) {
		t.Fatal("tess code --at wrote the vault")
	}
}

func TestCodeAtRejects(t *testing.T) {
	sealOutputVault(t)
	cases := []struct {
		args []string
		want error
	}{
		{[]string{"--at", "59"}, errAtNoQuery},
		{[]string{"gh", "--at", "59", "--next"}, errAtWithNext},
		{[]string{"gh", "--at", "59", "-c"}, errAtWithCopy},
		{[]string{"gh", "--at", "yesterday"}, errAtTime},
		{[]string{"gh", "--at", "-5"}, errAtTime},
		{[]string{"gh", "--at", "1960-01-01T00:00:00Z"}, errAtTime},
		{[]string{"gh", "--at", "2026-01-02 15:04:05"}, errAtTime},
		{[]string{"ac", "--at", "59"}, errAtCounter},
	}
	for _, c := range cases {
		stdout, _, err := runRootStreams(t, append([]string{"code"}, c.args...)...)
		if !errors.Is(err, c.want) {
			t.Errorf("%v = %v, want %v", c.args, err, c.want)
		}
		if stdout != "" {
			t.Errorf("%v printed %q", c.args, stdout)
		}
	}
}
