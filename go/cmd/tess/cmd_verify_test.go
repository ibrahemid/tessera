package main

import (
	"bytes"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ibrahemid/tessera/go/internal/account"
)

func runVerify(t *testing.T, args ...string) (string, error) {
	t.Helper()
	c := newVerifyCmd()
	var stdout bytes.Buffer
	c.SetOut(&stdout)
	c.SetErr(&bytes.Buffer{})
	c.SilenceUsage, c.SilenceErrors = true, true
	c.SetArgs(args)
	err := c.Execute()
	return stdout.String(), err
}

// codeAtStep is the GitHub fixture's code offset steps from goldenTime.
func codeAtStep(t *testing.T, offset int) string {
	t.Helper()
	c, err := genCode(handled("a", "GitHub", "me", "gh"), goldenTime.Add(time.Duration(offset)*30*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func assertMismatch(t *testing.T, err error) {
	t.Helper()
	var exit *exitError
	var mismatch *codeMismatchError
	if !errors.As(err, &exit) || exit.code != 1 || !errors.As(err, &mismatch) {
		t.Fatalf("want a mismatch exiting 1, got %v", err)
	}
}

func TestVerifyTOTPWindowEdges(t *testing.T) {
	sealOutputVault(t)
	stubClock(t, goldenTime)
	for _, window := range []int{0, 1, 3} {
		for offset := -window - 1; offset <= window+1; offset++ {
			c := codeAtStep(t, offset)
			stdout, err := runVerify(t, "gh", c, "--window", strconv.Itoa(window))
			if offset < -window || offset > window {
				assertMismatch(t, err)
				if stdout != "" {
					t.Errorf("a mismatch printed %q", stdout)
				}
				continue
			}
			if err != nil {
				t.Fatalf("window %d offset %d: %v", window, offset, err)
			}
			if !strings.HasPrefix(stdout, "The code matches GitHub (me)") {
				t.Errorf("window %d offset %d: stdout = %q", window, offset, stdout)
			}
		}
	}
}

func TestVerifyNamesTheStep(t *testing.T) {
	sealOutputVault(t)
	stubClock(t, goldenTime)
	cases := map[int]string{
		0:  "The code matches GitHub (me).\n",
		-1: "The code matches GitHub (me), from 1 step before now.\n",
		2:  "The code matches GitHub (me), from 2 steps after now.\n",
	}
	for offset, want := range cases {
		stdout, err := runVerify(t, "gh", codeAtStep(t, offset), "--window", "2")
		if err != nil || stdout != want {
			t.Errorf("offset %d: err = %v stdout = %q, want %q", offset, err, stdout, want)
		}
	}
}

func TestVerifyDefaultWindowIsOne(t *testing.T) {
	sealOutputVault(t)
	stubClock(t, goldenTime)
	if _, err := runVerify(t, "gh", codeAtStep(t, 1)); err != nil {
		t.Fatalf("one step ahead: %v", err)
	}
	_, err := runVerify(t, "gh", codeAtStep(t, -2))
	assertMismatch(t, err)
	if got := userMessage(err); got != "the code does not match GitHub (me) within 1 step of now" {
		t.Errorf("message = %q", got)
	}
}

func TestVerifyHOTPChecksTheNextCountersAndNeverWrites(t *testing.T) {
	path := sealOutputVault(t)
	before := readVaultBytes(t, path)
	codes := []string{"755224", "287082", "359152", "969429", "338314", "254676", "287922", "162583", "399871", "520489"}
	cases := []struct {
		window  int
		counter int
		ok      bool
	}{
		{0, 5, true}, {0, 6, false},
		{1, 5, true}, {1, 6, true}, {1, 7, false},
		{3, 8, true}, {3, 9, false},
		{3, 4, false},
	}
	for _, c := range cases {
		stdout, err := runVerify(t, "ac", codes[c.counter], "--window", strconv.Itoa(c.window))
		if !c.ok {
			assertMismatch(t, err)
			continue
		}
		want := "The code matches Acme (ci) at counter " + strconv.Itoa(c.counter) + ".\n"
		if err != nil || stdout != want {
			t.Errorf("window %d counter %d: err = %v stdout = %q", c.window, c.counter, err, stdout)
		}
	}
	_, err := runVerify(t, "ac", codes[9], "--window", "1")
	if got := userMessage(err); got != "the code does not match Acme (ci) at its counter or the 1 after it" {
		t.Errorf("message = %q", got)
	}
	if !bytes.Equal(before, readVaultBytes(t, path)) {
		t.Fatal("tess verify wrote the vault")
	}
}

func TestVerifyHOTPStopsAtTheLastCounter(t *testing.T) {
	a := hotpAcct("h", "Acme", "ci")
	a.Counter = account.MaxCounter
	want, err := genCode(a, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if got, ok, err := matchCounter(a, want, 5); err != nil || !ok || got != account.MaxCounter {
		t.Fatalf("matchCounter = %d %v %v", got, ok, err)
	}
	if _, ok, err := matchCounter(a, "000000", 5); err != nil || ok {
		t.Fatalf("matchCounter past the last counter = %v %v", ok, err)
	}
}

func TestVerifyAcceptsAGroupedCode(t *testing.T) {
	sealOutputVault(t)
	stubClock(t, goldenTime)
	if _, err := runVerify(t, "gh", "081 804"); err != nil {
		t.Fatalf("grouped code: %v", err)
	}
}

func TestVerifyRejectsAMalformedCode(t *testing.T) {
	sealOutputVault(t)
	stubClock(t, goldenTime)
	for _, in := range []string{"08180", "0818045", "08180a", ""} {
		_, err := runVerify(t, "gh", in)
		var format *codeFormatError
		if !errors.As(err, &format) {
			t.Errorf("%q: want a format error, got %v", in, err)
			continue
		}
		if got := err.Error(); got != "GitHub (me) codes are 6 digits" {
			t.Errorf("%q: message = %q", in, got)
		}
	}
}

func TestVerifySteam(t *testing.T) {
	path := withVault(t)
	a := steamAccount("s")
	a.Handle = "st"
	sealVault(t, path, []account.Account{a})
	stubClock(t, goldenTime)
	want, err := genCode(a, goldenTime)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runVerify(t, "st", strings.ToLower(want)); err != nil {
		t.Fatalf("lowercase Steam code %q: %v", want, err)
	}
	_, err = runVerify(t, "st", "AAAAA")
	var format *codeFormatError
	if !errors.As(err, &format) || err.Error() != "Steam (gaben) codes are 5 letters and digits" {
		t.Fatalf("a letter Steam never uses = %v", err)
	}
}

func TestVerifyWindowBounds(t *testing.T) {
	sealOutputVault(t)
	stubClock(t, goldenTime)
	for _, w := range []string{"-1", "101"} {
		if _, err := runVerify(t, "gh", "081804", "--window", w); !errors.Is(err, errVerifyWindow) {
			t.Errorf("--window %s = %v, want errVerifyWindow", w, err)
		}
	}
	if _, err := runVerify(t, "gh", "081804", "--window", "100"); err != nil {
		t.Errorf("--window 100: %v", err)
	}
}

func TestVerifyNeverCopies(t *testing.T) {
	sealOutputVault(t)
	stubClock(t, goldenTime)
	stubTTY(t, true)
	failOnCopy(t)
	if _, err := runVerify(t, "gh", "081804"); err != nil {
		t.Fatal(err)
	}
}
