package main

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ibrahemid/tessera/go/internal/account"
)

// goldenTime is RFC 6238's 1111111109, one second before its step ends.
var goldenTime = time.Unix(1111111109, 0)

// sealOutputVault seals accounts that already carry handles, so opening the
// vault writes nothing: a live TOTP account with a note, folder and tag, an
// ordered one, an HOTP one at counter 5, and a trashed one.
func sealOutputVault(t *testing.T) string {
	t.Helper()
	path := withVault(t)
	gh := handled("a", "GitHub", "me", "gh")
	gh.Folder, gh.Tags, gh.Note = "work", []string{"dev"}, "recovery codes in the safe"
	ze := handled("z", "Zeta", "ops", "ze")
	ze.Order = 1
	ac := hotpAcct("h", "Acme", "ci")
	ac.Handle, ac.Counter = "ac", 5
	dr := trashedAt(handled("d", "Dropbox", "me", "dr"), dropboxTrashedAt)
	sealVault(t, path, []account.Account{gh, ze, ac, dr})
	return path
}

func assertGolden(t *testing.T, name, got, want string) {
	t.Helper()
	if got != want {
		t.Fatalf("%s changed shape:\n got %s\nwant %s", name, got, want)
	}
}

const goldenTOTPRow = `{
    "id": "%s",
    "handle": "%s",
    "issuer": "%s",
    "account": "%s",
    "type": "totp",
    "code": "081804",
    "next_code": "050471",
    "expires_in": 1
  }`

func TestCodeJSONGolden(t *testing.T) {
	sealOutputVault(t)
	stubClock(t, goldenTime)
	stdout, _, err := runRootStreams(t, "code", "gh", "--json")
	if err != nil {
		t.Fatal(err)
	}
	want := strings.ReplaceAll(fmt.Sprintf(goldenTOTPRow, "a", "gh", "GitHub", "me"), "\n  ", "\n") + "\n"
	assertGolden(t, "code --json", stdout, want)
}

func TestCodeAllJSONGolden(t *testing.T) {
	sealOutputVault(t)
	stubClock(t, goldenTime)
	stdout, _, err := runRootStreams(t, "code", "--all", "--json")
	if err != nil {
		t.Fatal(err)
	}
	want := "[\n  " + fmt.Sprintf(goldenTOTPRow, "z", "ze", "Zeta", "ops") + `,
  {
    "id": "h",
    "handle": "ac",
    "issuer": "Acme",
    "account": "ci",
    "type": "hotp",
    "code": "254676"
  },
  ` + fmt.Sprintf(goldenTOTPRow, "a", "gh", "GitHub", "me") + "\n]\n"
	assertGolden(t, "code --all --json", stdout, want)
}

func TestListJSONGolden(t *testing.T) {
	sealOutputVault(t)
	stdout, _, err := runRootStreams(t, "list", "--json")
	if err != nil {
		t.Fatal(err)
	}
	assertGolden(t, "list --json", stdout, `[
  {
    "id": "z",
    "handle": "ze",
    "issuer": "Zeta",
    "account": "ops",
    "type": "totp",
    "pinned": false,
    "has_note": false,
    "order": 1
  },
  {
    "id": "h",
    "handle": "ac",
    "issuer": "Acme",
    "account": "ci",
    "type": "hotp",
    "pinned": false,
    "has_note": false
  },
  {
    "id": "a",
    "handle": "gh",
    "issuer": "GitHub",
    "account": "me",
    "type": "totp",
    "folder": "work",
    "tags": [
      "dev"
    ],
    "pinned": false,
    "has_note": true
  }
]
`)
	if strings.Contains(stdout, "recovery codes") {
		t.Fatal("list --json printed the note text")
	}
}

func TestShowJSONGolden(t *testing.T) {
	sealOutputVault(t)
	stdout, _, err := runRootStreams(t, "show", "gh", "--json")
	if err != nil {
		t.Fatal(err)
	}
	assertGolden(t, "show --json", stdout, `{
  "id": "a",
  "handle": "gh",
  "issuer": "GitHub",
  "account": "me",
  "type": "totp",
  "algorithm": "SHA1",
  "digits": 6,
  "period": 30,
  "folder": "work",
  "tags": [
    "dev"
  ],
  "pinned": false,
  "note": "recovery codes in the safe",
  "created_at": 1,
  "updated_at": 1
}
`)
}

func TestShowJSONGoldenForHOTP(t *testing.T) {
	sealOutputVault(t)
	stdout, _, err := runRootStreams(t, "show", "ac", "--json")
	if err != nil {
		t.Fatal(err)
	}
	assertGolden(t, "show --json (hotp)", stdout, `{
  "id": "h",
  "handle": "ac",
  "issuer": "Acme",
  "account": "ci",
  "type": "hotp",
  "algorithm": "SHA1",
  "digits": 6,
  "counter": 5,
  "pinned": false,
  "created_at": 1,
  "updated_at": 1
}
`)
}

func TestShowJSONKeepsACounterOfZero(t *testing.T) {
	path := withVault(t)
	a := hotpAcct("h", "Acme", "ci")
	a.Handle = "ac"
	sealVault(t, path, []account.Account{a})
	stdout, _, err := runRootStreams(t, "show", "ac", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, `"counter": 0,`) {
		t.Fatalf("an HOTP record carries its counter even at 0:\n%s", stdout)
	}
}

func TestShowJSONNeverCarriesTheSecret(t *testing.T) {
	sealOutputVault(t)
	stdout, _, err := runRootStreams(t, "show", "gh", "--json")
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{"secret", "GEZDGNBVGY3TQOJQ", "otpauth://"} {
		if strings.Contains(stdout, leak) {
			t.Fatalf("show --json leaks %q:\n%s", leak, stdout)
		}
	}
	if _, _, err := runRootStreams(t, "show", "gh", "--json", "--secret"); !errors.Is(err, errShowJSONSecret) {
		t.Fatalf("--json --secret = %v, want errShowJSONSecret", err)
	}
	if _, _, err := runRootStreams(t, "show", "gh", "--json", "--uri"); !errors.Is(err, errShowJSONSecret) {
		t.Fatalf("--json --uri = %v, want errShowJSONSecret", err)
	}
}

func TestTrashJSONGolden(t *testing.T) {
	sealOutputVault(t)
	stdout, _, err := runRootStreams(t, "trash", "--json")
	if err != nil {
		t.Fatal(err)
	}
	assertGolden(t, "trash --json", stdout, fmt.Sprintf(`[
  {
    "id": "d",
    "handle": "dr",
    "issuer": "Dropbox",
    "account": "me",
    "type": "totp",
    "deleted_at": %d
  }
]
`, dropboxTrashedAt))
}

func TestTrashJSONListsNewestFirst(t *testing.T) {
	sealTrashVault(t)
	stdout, _, err := runRootStreams(t, "trash", "--json")
	if err != nil {
		t.Fatal(err)
	}
	dr, ac := strings.Index(stdout, `"id": "c"`), strings.Index(stdout, `"id": "b"`)
	if dr < 0 || ac < 0 || dr > ac {
		t.Fatalf("want Dropbox (trashed last) before Acme:\n%s", stdout)
	}
}

func TestTrashJSONEmpty(t *testing.T) {
	path := withVault(t)
	sealVault(t, path, []account.Account{handled("a", "GitHub", "me", "gh")})
	stdout, _, err := runRootStreams(t, "trash", "--json")
	if err != nil || stdout != "[]\n" {
		t.Fatalf("err = %v stdout = %q, want []", err, stdout)
	}
}
