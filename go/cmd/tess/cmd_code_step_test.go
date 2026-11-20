package main

import (
	"math"
	"testing"
	"time"

	"github.com/ibrahemid/tessera/go/internal/account"
)

func TestNextCodeForALongPeriodIsTheFollowingStep(t *testing.T) {
	a := totp("a", "ACME", "x", 1)
	a.Period = 10_000_000_000
	now := time.Unix(1_800_000_000, 0)
	following := time.Unix((now.Unix()/int64(a.Period)+1)*int64(a.Period), 0)
	want, err := genCode(a, following)
	if err != nil {
		t.Fatal(err)
	}
	row, err := newCodeRow(a, now)
	if err != nil {
		t.Fatal(err)
	}
	if row.NextCode != want {
		t.Fatalf("next_code for period %d is %q, want the following step's code %q", a.Period, row.NextCode, want)
	}
}

func TestNextCodeForTheLongestPeriod(t *testing.T) {
	a := totp("a", "ACME", "x", 1)
	a.Period = math.MaxInt64
	want, err := genCode(a, time.Unix(math.MaxInt64, 0))
	if err != nil {
		t.Fatal(err)
	}
	row, err := newCodeRow(a, time.Unix(1_800_000_000, 0))
	if err != nil {
		t.Fatal(err)
	}
	if row.NextCode != want {
		t.Fatalf("next_code = %q, want %q", row.NextCode, want)
	}
}

func TestCodeRowRefusesAnAccountWithoutACode(t *testing.T) {
	a := totp("a", "ACME", "x", 1)
	a.Algorithm = "MD5"
	row, err := newCodeRow(a, time.Unix(1_800_000_000, 0))
	if err == nil {
		t.Fatalf("row = %+v, want an error", row)
	}
	if _, err := codeRows([]account.Account{totp("b", "B", "y", 1), a}, time.Unix(1_800_000_000, 0)); err == nil {
		t.Fatal("codeRows returned rows with an empty code")
	}
}

func TestVerifyWindowFindsTheNeighbourStepOfALongPeriod(t *testing.T) {
	a := totp("a", "ACME", "x", 1)
	a.Period = 10_000_000_000
	now := time.Unix(1_800_000_000, 0)
	next, err := genCode(a, time.Unix(int64(a.Period), 0))
	if err != nil {
		t.Fatal(err)
	}
	current, err := genCode(a, now)
	if err != nil {
		t.Fatal(err)
	}
	if next == current {
		t.Skip("the two steps share a code")
	}
	offset, ok, err := matchStep(a, next, now, 1)
	if err != nil || !ok || offset != 1 {
		t.Fatalf("matchStep = %d, %v, %v; want the following step", offset, ok, err)
	}
}

func TestStepStartStaysInWholeSeconds(t *testing.T) {
	cases := []struct {
		period int
		t      int64
		offset int64
		want   int64
		ok     bool
	}{
		{30, 59, 0, 30, true},
		{30, 59, 1, 60, true},
		{30, 59, -1, 0, true},
		{30, 59, -2, 0, false},
		{10_000_000_000, 1_800_000_000, 1, 10_000_000_000, true},
		{math.MaxInt64, 1_800_000_000, 1, math.MaxInt64, true},
		{math.MaxInt64, 1_800_000_000, 2, 0, false},
		{math.MaxInt64 / 2, 1_800_000_000, 2, math.MaxInt64 - 1, true},
		{math.MaxInt64 / 2, 1_800_000_000, 3, 0, false},
		{30, -31, 0, 0, false},
	}
	for _, c := range cases {
		got, ok := stepStart(time.Unix(c.t, 0), c.period, c.offset)
		if ok != c.ok || (ok && got.Unix() != c.want) {
			t.Errorf("stepStart(%d, %d, %d) = %d, %v; want %d, %v", c.t, c.period, c.offset, got.Unix(), ok, c.want, c.ok)
		}
	}
}
