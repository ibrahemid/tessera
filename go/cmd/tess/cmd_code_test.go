package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ibrahemid/tessera/go/internal/account"
)

// stubClipboard replaces the clipboard writer for one test.
func stubClipboard(t *testing.T, fn func(string) error) {
	t.Helper()
	old := copyToClipboard
	copyToClipboard = fn
	t.Cleanup(func() { copyToClipboard = old })
}

// stubTTY forces the stdout-is-a-terminal answer. Tests write to a buffer, so
// without this every run looks piped and the copy path never executes.
func stubTTY(t *testing.T, isTTY bool) {
	t.Helper()
	old := stdoutIsTTY
	stdoutIsTTY = func() bool { return isTTY }
	t.Cleanup(func() { stdoutIsTTY = old })
}

// stubClock freezes the clock at t.
func stubClock(t *testing.T, at time.Time) {
	t.Helper()
	old := now
	now = func() time.Time { return at }
	t.Cleanup(func() { now = old })
}

// stubClipClear captures the clipboard-clear scheduling instead of spawning a
// real detached process.
func stubClipClear(t *testing.T, fn func(int, string) error) {
	t.Helper()
	old := startClipClear
	startClipClear = fn
	t.Cleanup(func() { startClipClear = old })
}

func runCodeCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	stdout, _, err := runCodeCmdStreams(t, args...)
	return stdout, err
}

// runCodeCmdStreams runs `tess code` and returns stdout and stderr separately.
func runCodeCmdStreams(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	// An inherited $TESSERA_CLIP_CLEAR would make the suite schedule real
	// clipboard wipes, so every code test runs with the feature off.
	t.Setenv("TESSERA_CLIP_CLEAR", "")
	c := newCodeCmd()
	var stdout, stderr bytes.Buffer
	c.SetOut(&stdout)
	c.SetErr(&stderr)
	c.SetArgs(args)
	err := c.Execute()
	return stdout.String(), stderr.String(), err
}

// TestCodeCopyFailureExitsNonZero pins that a clipboard error reaches the user.
// A silently discarded failure means the user pastes a stale code.
func TestCodeCopyFailureExitsNonZero(t *testing.T) {
	path := withVault(t)
	sealVault(t, path, []account.Account{totp("a", "ACME", "x", 1)})
	stubTTY(t, true)
	stubClipboard(t, func(string) error { return errors.New("no clipboard on this display") })

	_, err := runCodeCmd(t, "acme")
	if err == nil {
		t.Fatal("a failed clipboard write must not exit zero")
	}
	if !strings.Contains(err.Error(), "clipboard") {
		t.Errorf("error should name the clipboard, got %v", err)
	}
}

// TestCodeCopiesByDefault pins the default: a terminal run copies without being
// asked, and says so next to the code.
func TestCodeCopiesByDefault(t *testing.T) {
	path := withVault(t)
	sealVault(t, path, []account.Account{totp("a", "ACME", "x", 1)})
	stubTTY(t, true)
	var copied string
	stubClipboard(t, func(s string) error { copied = s; return nil })

	out, err := runCodeCmd(t, "acme")
	if err != nil {
		t.Fatalf("code: %v", err)
	}
	if len(copied) != 6 {
		t.Fatalf("copied %q, want a 6-digit code", copied)
	}
	if !strings.Contains(out, "copied") {
		t.Errorf("output should report the copy, got %q", out)
	}
	if !strings.Contains(out, "s") || !strings.Contains(out, "·") {
		t.Errorf("output should carry the remaining seconds, got %q", out)
	}
	grouped := copied[:3] + " " + copied[3:]
	if !strings.Contains(out, grouped) {
		t.Errorf("printed code %q does not group the copied code %q", out, copied)
	}
}

// TestCodeLegacyCopyFlagStillAccepted pins the long spelling of -c: older
// scripts pass --copy.
func TestCodeLegacyCopyFlagStillAccepted(t *testing.T) {
	path := withVault(t)
	sealVault(t, path, []account.Account{totp("a", "ACME", "x", 1)})
	stubTTY(t, true)
	var copied string
	stubClipboard(t, func(s string) error { copied = s; return nil })

	if _, err := runCodeCmd(t, "acme", "--copy"); err != nil {
		t.Fatalf("code --copy: %v", err)
	}
	if len(copied) != 6 {
		t.Errorf("copied %q, want a 6-digit code", copied)
	}
}

// TestCodeCopyRules pins who ends up on the clipboard. A launcher that runs
// tess with a pipe (Raycast, Alfred) asks with -c and must be served.
func TestCodeCopyRules(t *testing.T) {
	cases := []struct {
		name string
		tty  bool
		args []string
		want bool
	}{
		{"piped with -c copies", false, []string{"acme", "-c"}, true},
		{"piped without -c does not", false, []string{"acme"}, false},
		{"terminal copies", true, []string{"acme"}, true},
		{"no-copy on a terminal does not", true, []string{"acme", "--no-copy"}, false},
		{"no-copy beats -c", true, []string{"acme", "-c", "--no-copy"}, false},
		{"json with -c copies", false, []string{"acme", "--json", "-c"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := withVault(t)
			sealVault(t, path, []account.Account{totp("a", "ACME", "x", 1)})
			stubTTY(t, tc.tty)
			var copied string
			stubClipboard(t, func(s string) error { copied = s; return nil })

			if _, err := runCodeCmd(t, tc.args...); err != nil {
				t.Fatalf("code %v: %v", tc.args, err)
			}
			if got := copied != ""; got != tc.want {
				t.Errorf("copied %q, want copy = %v", copied, tc.want)
			}
		})
	}
}

func TestCodeNoCopyPrintsWithoutTouchingClipboard(t *testing.T) {
	path := withVault(t)
	sealVault(t, path, []account.Account{totp("a", "ACME", "x", 1)})
	stubTTY(t, true)
	stubClipboard(t, func(string) error {
		t.Error("--no-copy must not touch the clipboard")
		return nil
	})

	out, err := runCodeCmd(t, "acme", "--no-copy")
	if err != nil {
		t.Fatalf("code --no-copy: %v", err)
	}
	if strings.Contains(out, "copied") {
		t.Errorf("--no-copy must not claim a copy, got %q", out)
	}
}

// TestCodePipedPrintsRawDigits pins the script contract: piped output is the
// bare code and nothing is placed on the clipboard.
func TestCodePipedPrintsRawDigits(t *testing.T) {
	path := withVault(t)
	sealVault(t, path, []account.Account{totp("a", "ACME", "x", 1)})
	stubTTY(t, false)
	stubClipboard(t, func(string) error {
		t.Error("a piped run must not touch the clipboard")
		return nil
	})

	out, err := runCodeCmd(t, "acme")
	if err != nil {
		t.Fatalf("code: %v", err)
	}
	got := strings.TrimSpace(out)
	if len(got) != 6 {
		t.Fatalf("piped output = %q, want six bare digits", got)
	}
	for _, r := range got {
		if r < '0' || r > '9' {
			t.Fatalf("piped output = %q, want digits only", got)
		}
	}
}

// TestCodeJSONNeverCopies pins that --json is a machine path even on a terminal.
func TestCodeJSONNeverCopies(t *testing.T) {
	path := withVault(t)
	sealVault(t, path, []account.Account{totp("a", "ACME", "x", 1)})
	stubTTY(t, true)
	stubClipboard(t, func(string) error {
		t.Error("--json must not touch the clipboard")
		return nil
	})

	out, err := runCodeCmd(t, "acme", "--json")
	if err != nil {
		t.Fatalf("code --json: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output is not JSON: %v (%q)", err, out)
	}
	exp, ok := got["expires_in"].(float64)
	if !ok {
		t.Fatalf("expires_in missing from %v", got)
	}
	if exp < 1 || exp > 30 {
		t.Errorf("expires_in = %v, want 1..30", exp)
	}
}

// TestCodeJSONOmitsExpiryForHOTP: an HOTP code has no time window, so reporting
// one would be a lie a script could act on.
func TestCodeJSONOmitsExpiryForHOTP(t *testing.T) {
	path := withVault(t)
	sealVault(t, path, []account.Account{hotpAcct("h", "Bank", "ops")})
	stubTTY(t, true)

	out, err := runCodeCmd(t, "bank", "--json")
	if err != nil {
		t.Fatalf("code --json: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output is not JSON: %v (%q)", err, out)
	}
	if _, present := got["expires_in"]; present {
		t.Errorf("HOTP output must not carry expires_in: %v", got)
	}
}

// TestCodeNextPersistsCounterEvenIfCopyFails pins the ordering: the advanced
// HOTP counter is written before the copy is attempted, so a clipboard failure
// never leaves the user's counter behind the code they were shown.
func TestCodeNextPersistsCounterEvenIfCopyFails(t *testing.T) {
	path := withVault(t)
	sealVault(t, path, []account.Account{hotpAcct("h", "Bank", "ops")})
	stubTTY(t, true)
	stubClipboard(t, func(string) error { return errors.New("no clipboard") })

	if _, err := runCodeCmd(t, "bank", "--next"); err == nil {
		t.Fatal("expected the clipboard failure to surface")
	}
	s, err := openSession()
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	i := indexByID(s.accounts, "h")
	if i < 0 || s.accounts[i].Counter != 1 {
		t.Fatalf("counter not persisted: %+v", s.accounts)
	}
}

// TestCodeAmbiguousListsMatchesAndExitsZero: with no single code to copy, the
// table of matching codes is the answer, not an error.
func TestCodeAmbiguousListsMatchesAndExitsZero(t *testing.T) {
	path := withVault(t)
	sealVault(t, path, []account.Account{
		totp("a", "GitHub", "dev", 1), totp("b", "GitHub", "ops", 2),
	})
	stubTTY(t, true)
	stubClipboard(t, func(string) error {
		t.Error("an ambiguous query must not copy anything")
		return nil
	})

	out, err := runCodeCmd(t, "github")
	if err != nil {
		t.Fatalf("ambiguous query should exit zero, got %v", err)
	}
	if !strings.Contains(out, "dev") || !strings.Contains(out, "ops") {
		t.Errorf("both matches should be listed, got %q", out)
	}
}

// TestCodeAmbiguousJSONStaysMachineReadable: a script asking for JSON gets JSON
// even when the query matched more than one account.
func TestCodeAmbiguousJSONStaysMachineReadable(t *testing.T) {
	path := withVault(t)
	sealVault(t, path, []account.Account{
		totp("a", "GitHub", "dev", 1), totp("b", "GitHub", "ops", 2),
	})
	stubTTY(t, false)

	out, err := runCodeCmd(t, "github", "--json")
	if err != nil {
		t.Fatalf("code --json: %v", err)
	}
	var rows []codeRow
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("output is not a JSON array: %v (%q)", err, out)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(rows))
	}
}

// TestCodeWaitsForAFreshCode pins the freshness guard: a code with under four
// seconds left is one the user pastes after the server has retired it.
func TestCodeWaitsForAFreshCode(t *testing.T) {
	path := withVault(t)
	acct := totp("a", "ACME", "x", 1)
	sealVault(t, path, []account.Account{acct})
	stubTTY(t, true)

	// 2 seconds left in the window that starts at 60.
	clock := time.Unix(88, 0)
	oldNow := now
	now = func() time.Time { return clock }
	t.Cleanup(func() { now = oldNow })

	var slept time.Duration
	oldSleep := sleepFn
	sleepFn = func(d time.Duration) { slept = d; clock = clock.Add(d) }
	t.Cleanup(func() { sleepFn = oldSleep })

	var copied string
	stubClipboard(t, func(s string) error { copied = s; return nil })

	stdout, stderr, err := runCodeCmdStreams(t, "acme")
	if err != nil {
		t.Fatalf("code: %v", err)
	}
	// The wait note must stay off stdout, or `$(tess acme)` captures it.
	if !strings.Contains(stderr, "waiting 2s for a fresh code") {
		t.Errorf("stderr should explain the wait, got %q", stderr)
	}
	if strings.Contains(stdout, "waiting") {
		t.Errorf("the wait note leaked into stdout: %q", stdout)
	}
	if slept != 2*time.Second {
		t.Errorf("slept %v, want 2s", slept)
	}
	want, gerr := genCode(acct, time.Unix(90, 0))
	if gerr != nil {
		t.Fatal(gerr)
	}
	if copied != want {
		t.Errorf("copied %q, want the next window's code %q", copied, want)
	}
}

// TestCodeNowSkipsTheFreshnessWait: --now means "the code as it is", for a
// caller that would rather have a short-lived code than block.
func TestCodeNowSkipsTheFreshnessWait(t *testing.T) {
	path := withVault(t)
	acct := totp("a", "ACME", "x", 1)
	sealVault(t, path, []account.Account{acct})
	stubTTY(t, true)
	stubClock(t, time.Unix(88, 0))
	stubClipboard(t, func(string) error { return nil })

	oldSleep := sleepFn
	sleepFn = func(time.Duration) { t.Error("--now must not wait") }
	t.Cleanup(func() { sleepFn = oldSleep })

	if _, err := runCodeCmd(t, "acme", "--now"); err != nil {
		t.Fatalf("code --now: %v", err)
	}
}

// TestCodeJSONSkipsTheFreshnessWait keeps scripted callers unblocked.
func TestCodeJSONSkipsTheFreshnessWait(t *testing.T) {
	path := withVault(t)
	sealVault(t, path, []account.Account{totp("a", "ACME", "x", 1)})
	stubTTY(t, false)
	stubClock(t, time.Unix(88, 0))

	oldSleep := sleepFn
	sleepFn = func(time.Duration) { t.Error("--json must not wait") }
	t.Cleanup(func() { sleepFn = oldSleep })

	if _, err := runCodeCmd(t, "acme", "--json"); err != nil {
		t.Fatalf("code --json: %v", err)
	}
}

// TestCodeClearSchedulesTheWipe pins that --clear hands the copied code's delay
// and fingerprint to the detached clearer.
func TestCodeClearSchedulesTheWipe(t *testing.T) {
	path := withVault(t)
	sealVault(t, path, []account.Account{totp("a", "ACME", "x", 1)})
	stubTTY(t, true)
	var copied string
	stubClipboard(t, func(s string) error { copied = s; return nil })
	var gotSeconds int
	var gotCode string
	stubClipClear(t, func(seconds int, c string) error {
		gotSeconds, gotCode = seconds, c
		return nil
	})

	if _, err := runCodeCmd(t, "acme", "--clear", "20"); err != nil {
		t.Fatalf("code --clear: %v", err)
	}
	if gotSeconds != 20 {
		t.Errorf("scheduled clear after %ds, want 20", gotSeconds)
	}
	if gotCode != copied {
		t.Errorf("scheduled the wipe for %q, want the copied code %q", gotCode, copied)
	}
}

// TestCodeWithoutClearSchedulesNothing: the clipboard is the user's, and tess
// only takes it back when asked.
func TestCodeWithoutClearSchedulesNothing(t *testing.T) {
	path := withVault(t)
	sealVault(t, path, []account.Account{totp("a", "ACME", "x", 1)})
	stubTTY(t, true)
	stubClipboard(t, func(string) error { return nil })
	stubClipClear(t, func(int, string) error {
		t.Error("no --clear and no $TESSERA_CLIP_CLEAR must schedule no wipe")
		return nil
	})

	if _, err := runCodeCmd(t, "acme"); err != nil {
		t.Fatalf("code: %v", err)
	}
}

func TestDefaultClipClearReadsTheEnvironment(t *testing.T) {
	for _, tc := range []struct {
		env  string
		want int
	}{
		{"", 0}, {"15", 15}, {" 30 ", 30}, {"0", 0}, {"-5", 0}, {"soon", 0},
	} {
		t.Setenv("TESSERA_CLIP_CLEAR", tc.env)
		if got := defaultClipClear(); got != tc.want {
			t.Errorf("defaultClipClear() with %q = %d, want %d", tc.env, got, tc.want)
		}
	}
}
