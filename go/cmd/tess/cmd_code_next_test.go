package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/ui"
)

// TestCodeNextOnTimeBasedAccounts pins --next on TOTP and Steam: text mode
// prints and copies the following step's code without the freshness wait, and
// the JSON row keeps its meaning while -c --next copies next_code.
func TestCodeNextOnTimeBasedAccounts(t *testing.T) {
	// Two seconds left in the window that starts at 60.
	clock := time.Unix(88, 0)
	for _, acct := range []account.Account{totp("g", "GitHub", "dev", 1), steamAccount("s")} {
		current, err := genCode(acct, clock)
		if err != nil {
			t.Fatal(err)
		}
		next, err := genCode(acct, clock.Add(30*time.Second))
		if err != nil {
			t.Fatal(err)
		}
		if current == next {
			t.Fatalf("%s: the fixture's current and next codes are equal", acct.Type)
		}
		query := strings.ToLower(acct.Issuer)

		setup := func(t *testing.T, tty bool) (*fakeClipboard, *[]clipClearCall) {
			t.Helper()
			path := withVault(t)
			sealVault(t, path, []account.Account{acct})
			stubTTY(t, tty)
			stubClock(t, clock)
			oldSleep := sleepFn
			sleepFn = func(time.Duration) { t.Error("--next must not wait for a fresh code") }
			t.Cleanup(func() { sleepFn = oldSleep })
			return stubBackend(t, &fakeClipboard{}), recordClipClear(t)
		}
		decodeRow := func(t *testing.T, stdout string) codeRow {
			t.Helper()
			var row codeRow
			if err := json.Unmarshal([]byte(stdout), &row); err != nil {
				t.Fatalf("output is not a code row: %v (%q)", err, stdout)
			}
			if row.Code != current || row.NextCode != next || row.Expires != 2 {
				t.Errorf("row = %+v, want code %s, next_code %s, expires_in 2", row, current, next)
			}
			return row
		}

		t.Run(string(acct.Type)+"/terminal prints and copies the next step", func(t *testing.T) {
			fake, calls := setup(t, true)
			stdout, err := runCodeCmd(t, query, "--next")
			if err != nil {
				t.Fatalf("code --next: %v", err)
			}
			if !strings.Contains(stdout, ui.GroupCode(next)) || strings.Contains(stdout, ui.GroupCode(current)) {
				t.Errorf("stdout = %q, want the next code %s only", stdout, next)
			}
			if !strings.Contains(stdout, "valid in 2s") || !strings.Contains(stdout, "copied") {
				t.Errorf("stdout = %q, want copied and valid in 2s", stdout)
			}
			if fake.text != next || !fake.sensitive {
				t.Errorf("clipboard = %q (sensitive %v), want %q sensitive", fake.text, fake.sensitive, next)
			}
			if len(*calls) != 1 || (*calls)[0] != (clipClearCall{30, next}) {
				t.Errorf("scheduled %+v, want one clear of the next code after 30s", *calls)
			}
		})

		t.Run(string(acct.Type)+"/piped prints the next step's bare code", func(t *testing.T) {
			fake, _ := setup(t, false)
			stdout, err := runCodeCmd(t, query, "--next")
			if err != nil {
				t.Fatalf("code --next: %v", err)
			}
			if stdout != next+"\n" {
				t.Errorf("stdout = %q, want %q", stdout, next+"\n")
			}
			if fake.writes != 0 {
				t.Errorf("a piped run without -c wrote the clipboard %d times", fake.writes)
			}
		})

		t.Run(string(acct.Type)+"/piped -c copies the next step", func(t *testing.T) {
			fake, _ := setup(t, false)
			stdout, err := runCodeCmd(t, query, "--next", "-c")
			if err != nil {
				t.Fatalf("code --next -c: %v", err)
			}
			if stdout != next+"\n" || fake.text != next {
				t.Errorf("stdout = %q, clipboard = %q, want %q for both", stdout, fake.text, next)
			}
		})

		t.Run(string(acct.Type)+"/json row is unchanged", func(t *testing.T) {
			fake, _ := setup(t, true)
			stdout, err := runCodeCmd(t, query, "--next", "--json")
			if err != nil {
				t.Fatalf("code --next --json: %v", err)
			}
			decodeRow(t, stdout)
			if fake.writes != 0 {
				t.Errorf("--json without -c wrote the clipboard %d times", fake.writes)
			}
		})

		t.Run(string(acct.Type)+"/json -c --next copies next_code", func(t *testing.T) {
			fake, calls := setup(t, false)
			stdout, err := runCodeCmd(t, query, "--json", "-c", "--next", "--clear", "30")
			if err != nil {
				t.Fatalf("code --json -c --next: %v", err)
			}
			row := decodeRow(t, stdout)
			if fake.text != row.NextCode || !fake.sensitive {
				t.Errorf("clipboard = %q (sensitive %v), want next_code %q", fake.text, fake.sensitive, row.NextCode)
			}
			if len(*calls) != 1 || (*calls)[0] != (clipClearCall{30, next}) {
				t.Errorf("scheduled %+v, want one clear of next_code after 30s", *calls)
			}
		})

		t.Run(string(acct.Type)+"/json -c copies code", func(t *testing.T) {
			fake, _ := setup(t, false)
			stdout, err := runCodeCmd(t, query, "--json", "-c")
			if err != nil {
				t.Fatalf("code --json -c: %v", err)
			}
			if row := decodeRow(t, stdout); fake.text != row.Code {
				t.Errorf("clipboard = %q, want code %q", fake.text, row.Code)
			}
		})
	}
}

// TestCodeNextOnHOTPCopiesTheAdvancedCode pins that HOTP keeps its meaning of
// --next: the counter advances and -c copies the row's code.
func TestCodeNextOnHOTPCopiesTheAdvancedCode(t *testing.T) {
	path := withVault(t)
	acct := hotpAcct("h", "Bank", "ops")
	sealVault(t, path, []account.Account{acct})
	stubTTY(t, false)
	fake := stubBackend(t, &fakeClipboard{})
	recordClipClear(t)

	stdout, err := runCodeCmd(t, "bank", "--json", "-c", "--next")
	if err != nil {
		t.Fatalf("code --json -c --next: %v", err)
	}
	acct.Counter = 1
	want, err := genCode(acct, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var row codeRow
	if err := json.Unmarshal([]byte(stdout), &row); err != nil {
		t.Fatalf("output is not a code row: %v (%q)", err, stdout)
	}
	if row.Code != want || row.NextCode != "" || fake.text != want {
		t.Errorf("row = %+v, clipboard = %q, want code %q at counter 1 for both", row, fake.text, want)
	}
}

func TestCodeAtWithNextNamesTheConflict(t *testing.T) {
	sealOutputVault(t)
	_, _, err := runRootStreams(t, "code", "gh", "--at", "59", "--next")
	if err == nil || err.Error() != "--at already picks the time; drop --next" {
		t.Fatalf("err = %v", err)
	}
}

func TestCodeNextHelpStatesBothMeanings(t *testing.T) {
	usage := newCodeCmd().Flags().Lookup("next").Usage
	if strings.Contains(usage, "\n") || !strings.Contains(usage, "TOTP and Steam") || !strings.Contains(usage, "HOTP") {
		t.Errorf("--next usage = %q, want one line naming both meanings", usage)
	}
}
