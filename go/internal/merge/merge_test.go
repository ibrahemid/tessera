package merge

import (
	"encoding/json"
	"testing"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/spectest"
)

func acct(id, issuer, name, handle string, updated int64) account.Account {
	return account.Account{
		ID: id, Type: account.TOTP, Issuer: issuer, Account: name,
		Secret: []byte("secret-" + id), Algorithm: "SHA1", Digits: 6, Period: 30,
		Handle: handle, CreatedAt: 1, UpdatedAt: updated,
	}
}

func TestIntoNewerWinsAndCarriesDeletedAt(t *testing.T) {
	newer := acct("a", "Mail", "me", "other", 20)
	newer.DeletedAt = 20
	got, c := Into([]account.Account{acct("a", "Mail", "me", "mail", 10)}, []account.Account{newer})
	if c != (Counts{Trashed: 1}) || len(got) != 1 {
		t.Fatalf("counts %+v len %d, want one move to the Trash", c, len(got))
	}
	if got[0].DeletedAt != 20 || got[0].UpdatedAt != 20 || got[0].Handle != "mail" {
		t.Fatalf("got %+v, want the newer copy with the local handle", got[0])
	}
}

func TestIntoKeepsTheIncomingHandleWhenTheLocalHasNone(t *testing.T) {
	got, _ := Into([]account.Account{acct("a", "Mail", "me", "", 10)}, []account.Account{acct("a", "Mail", "me", "mail", 20)})
	if got[0].Handle != "mail" {
		t.Fatalf("handle = %q, want mail", got[0].Handle)
	}
}

func TestIntoEqualOrOlderChangesNothing(t *testing.T) {
	local := acct("a", "Mail", "me", "mail", 10)
	for _, updated := range []int64{10, 5} {
		incoming := acct("a", "Renamed", "me", "mail", updated)
		got, c := Into([]account.Account{local}, []account.Account{incoming})
		if c != (Counts{Unchanged: 1}) || c.Changed() || got[0].Issuer != "Mail" {
			t.Fatalf("updated_at %d: counts %+v issuer %q, want one unchanged and no write", updated, c, got[0].Issuer)
		}
	}
}

func TestIntoSkipsALiveContentDuplicate(t *testing.T) {
	local := acct("a", "GitHub", "octocat", "gh", 10)
	dup := local
	dup.ID, dup.Issuer, dup.Account = "b", "github", "OCTOCAT"
	trashedDup := local
	trashedDup.ID, trashedDup.DeletedAt = "c", 30
	got, c := Into([]account.Account{local}, []account.Account{dup, trashedDup})
	if c != (Counts{Added: 1, Duplicate: 1}) || len(got) != 2 || got[1].ID != "c" {
		t.Fatalf("counts %+v got %v, want the trashed copy added and the live duplicate skipped", c, ids(got))
	}
}

func TestIntoIgnoresATrashedLocalForTheDuplicateCheck(t *testing.T) {
	local := acct("a", "GitHub", "octocat", "gh", 10)
	local.DeletedAt = 10
	dup := local
	dup.ID, dup.DeletedAt = "b", 0
	_, c := Into([]account.Account{local}, []account.Account{dup})
	if c != (Counts{Added: 1}) {
		t.Fatalf("counts %+v, want the live copy added", c)
	}
}

func TestIntoSkipsAnInvalidNewAccount(t *testing.T) {
	bad := acct("b", "Broken", "x", "", 10)
	bad.Digits = 9
	got, c := Into(nil, []account.Account{bad})
	if c != (Counts{Invalid: 1}) || len(got) != 0 {
		t.Fatalf("counts %+v len %d, want the invalid account left out", c, len(got))
	}
}

func TestIntoCountsWhereTheNewerCopyEndsUp(t *testing.T) {
	live := acct("a", "Mail", "me", "mail", 10)
	trashed := live
	trashed.DeletedAt = 10
	newerLive := acct("a", "Mail", "me", "mail", 20)
	newerTrashed := newerLive
	newerTrashed.DeletedAt = 20
	cases := []struct {
		name          string
		local, source account.Account
		want          Counts
	}{
		{"live to the Trash", live, newerTrashed, Counts{Trashed: 1}},
		{"restored from the Trash", trashed, newerLive, Counts{Updated: 1}},
		{"edited in the Trash", trashed, newerTrashed, Counts{Updated: 1}},
		{"edited while live", live, newerLive, Counts{Updated: 1}},
	}
	for _, tc := range cases {
		got, c := Into([]account.Account{tc.local}, []account.Account{tc.source})
		if c != tc.want || !c.Changed() {
			t.Errorf("%s: counts %+v, want %+v", tc.name, c, tc.want)
		}
		if got[0].DeletedAt != tc.source.DeletedAt {
			t.Errorf("%s: deleted_at %d, want %d", tc.name, got[0].DeletedAt, tc.source.DeletedAt)
		}
	}
}

func TestIntoLeavesOutAnInvalidReplacement(t *testing.T) {
	local := acct("a", "Mail", "me", "mail", 10)
	bad := acct("a", "Renamed", "me", "mail", 20)
	bad.Digits = 9
	got, c := Into([]account.Account{local}, []account.Account{bad})
	if c != (Counts{Invalid: 1}) || c.Changed() {
		t.Fatalf("counts %+v, want one invalid and no write", c)
	}
	if got[0].Issuer != "Mail" || got[0].Digits != 6 || got[0].UpdatedAt != 10 {
		t.Fatalf("got %+v, want the local account kept", got[0])
	}
}

func TestIntoValidatesTheReplacementWithTheLocalHandle(t *testing.T) {
	incoming := acct("a", "Mail", "me", "Not A Handle", 20)
	got, c := Into([]account.Account{acct("a", "Mail", "me", "mail", 10)}, []account.Account{incoming})
	if c != (Counts{Updated: 1}) || got[0].Handle != "mail" {
		t.Fatalf("counts %+v handle %q, want the copy applied under the local handle", c, got[0].Handle)
	}
}

func TestIntoNeverAliasesItsInputs(t *testing.T) {
	shared := acct("a", "Mail", "me", "mail", 10)
	shared.Tags = []string{"work"}
	shared.Extra = map[string]json.RawMessage{"x_note": json.RawMessage(`"v"`)}
	dst := make([]account.Account, 1, 8)
	dst[0] = shared
	newer := acct("a", "Mail", "me", "", 20)
	newer.Tags = []string{"home"}
	added := acct("b", "Bank", "me", "mail", 10)
	added.Tags = []string{"money"}
	added.Extra = map[string]json.RawMessage{"x_note": json.RawMessage(`"w"`)}
	src := []account.Account{newer, added}
	dstBefore := snapshot(t, dst[:cap(dst)])
	srcBefore := snapshot(t, src)

	got, c := Into(dst, src)
	if c != (Counts{Added: 1, Updated: 1}) || len(got) != 2 {
		t.Fatalf("counts %+v len %d", c, len(got))
	}
	for i := range got {
		got[i].Secret[0] ^= 0xff
		got[i].Tags[0] = "changed"
		got[i].Handle = "changed"
		for k := range got[i].Extra {
			got[i].Extra[k][0] = 'X'
		}
	}
	if after := snapshot(t, dst[:cap(dst)]); after != dstBefore {
		t.Fatalf("dst changed:\n%s\nwant\n%s", after, dstBefore)
	}
	if after := snapshot(t, src); after != srcBefore {
		t.Fatalf("src changed:\n%s\nwant\n%s", after, srcBefore)
	}
}

func TestTwoWayLeavesBothInputsUntouched(t *testing.T) {
	local := make([]account.Account, 2, 8)
	local[0] = acct("a", "Mail", "me", "mail", 10)
	local[1] = acct("c", "Chat", "me", "bank", 10)
	source := make([]account.Account, 2, 8)
	source[0] = acct("a", "Mail Renamed", "me", "other", 20)
	source[1] = acct("b", "Bank", "me", "bank", 10)
	localBefore, sourceBefore := snapshot(t, local[:cap(local)]), snapshot(t, source[:cap(source)])

	localOut, sourceOut, _, _ := TwoWay(local, source)
	if snapshot(t, local[:cap(local)]) != localBefore || snapshot(t, source[:cap(source)]) != sourceBefore {
		t.Fatal("TwoWay modified an input")
	}
	if len(localOut) != 3 || len(sourceOut) != 3 || sourceOut[0].Issuer != "Mail Renamed" || localOut[0].Issuer != "Mail Renamed" {
		t.Fatalf("local %v source %v", ids(localOut), ids(sourceOut))
	}
}

// snapshot renders every field of accts, so any write through shared memory
// shows up as a difference.
func snapshot(t *testing.T, accts []account.Account) string {
	t.Helper()
	out, err := json.Marshal(accts)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestIntoClearsACollidingIncomingHandle(t *testing.T) {
	got, c := Into([]account.Account{acct("b", "GitHub", "work", "gi", 10)}, []account.Account{acct("a", "GitHub", "home", "gi", 10)})
	if c != (Counts{Added: 1}) || got[0].Handle != "gi" || got[1].Handle != "" {
		t.Fatalf("handles %q %q, want the existing one kept and the incoming one cleared", got[0].Handle, got[1].Handle)
	}
}

func TestTwoWayFoldsThePreRunLocalIntoTheSource(t *testing.T) {
	local := make([]account.Account, 1, 4)
	local[0] = acct("a", "Mail", "me", "mail", 10)
	source := []account.Account{acct("b", "Bank", "me", "bank", 10)}
	localOut, sourceOut, here, there := TwoWay(local, source)
	if here != (Counts{Added: 1}) || there != (Counts{Added: 1}) {
		t.Fatalf("here %+v there %+v, want one add each way and no skip", here, there)
	}
	if len(localOut) != 2 || len(sourceOut) != 2 {
		t.Fatalf("local %v source %v, want both to hold both accounts", ids(localOut), ids(sourceOut))
	}
}

func TestDedupeHandlesKeepsTheFirst(t *testing.T) {
	accts := []account.Account{{Handle: "a"}, {Handle: "b"}, {Handle: "a"}, {}, {Handle: "b"}}
	DedupeHandles(accts)
	want := []string{"a", "b", "", "", ""}
	for i, a := range accts {
		if a.Handle != want[i] {
			t.Fatalf("handle %d = %q, want %q", i, a.Handle, want[i])
		}
	}
}

func TestContentKeyIgnoresCaseOfNamesButNotOfTheSecret(t *testing.T) {
	a := acct("a", "GitHub", "Octocat", "", 1)
	b := acct("b", "github", "octocat", "", 1)
	b.Secret = a.Secret
	if ContentKey(a) != ContentKey(b) {
		t.Fatal("issuer and account case changed the content key")
	}
	b.Secret = []byte("another secret")
	if ContentKey(a) == ContentKey(b) {
		t.Fatal("a different secret gave the same content key")
	}
	b.Secret, b.Type = a.Secret, account.HOTP
	if ContentKey(a) == ContentKey(b) {
		t.Fatal("a different type gave the same content key")
	}
}

func TestContentKeyKeepsFieldsApart(t *testing.T) {
	a := acct("a", "a|b", "c", "", 1)
	b := acct("b", "a", "b|c", "", 1)
	b.Secret = a.Secret
	if ContentKey(a) == ContentKey(b) {
		t.Fatal("issuer a|b with account c gave the same key as issuer a with account b|c")
	}
	got, c := Into([]account.Account{a}, []account.Account{b})
	if c != (Counts{Added: 1}) || len(got) != 2 {
		t.Fatalf("counts %+v len %d, want the second account added", c, len(got))
	}
}

func hotp(id string, counter, updated int64) account.Account {
	a := acct(id, "Bank", "token", "tok", updated)
	a.Type, a.Counter = account.HOTP, counter
	return a
}

func TestIntoNeverMovesAnHOTPCounterBackwards(t *testing.T) {
	cases := []struct {
		name          string
		local, source account.Account
		want          Counts
		counter       int64
		updated       int64
		issuer        string
	}{
		{"older copy with a larger counter", hotp("a", 7, 20), hotp("a", 12, 10), Counts{Updated: 1}, 12, 20, "Bank"},
		{"equal copy with a larger counter", hotp("a", 7, 20), hotp("a", 12, 20), Counts{Updated: 1}, 12, 20, "Bank"},
		{"older copy with a smaller counter", hotp("a", 7, 20), hotp("a", 3, 10), Counts{Unchanged: 1}, 7, 20, "Bank"},
		{"newer copy with a smaller counter", hotp("a", 7, 10), func() account.Account { a := hotp("a", 3, 20); a.Issuer = "Renamed"; return a }(), Counts{Updated: 1}, 7, 20, "Renamed"},
		{"newer copy with a larger counter", hotp("a", 7, 10), hotp("a", 9, 20), Counts{Updated: 1}, 9, 20, "Bank"},
		{"older copy with another secret", hotp("a", 7, 20), func() account.Account { a := hotp("a", 12, 10); a.Secret = []byte("other"); return a }(), Counts{Unchanged: 1}, 7, 20, "Bank"},
		{"newer copy with another secret", hotp("a", 7, 10), func() account.Account { a := hotp("a", 3, 20); a.Secret = []byte("other"); return a }(), Counts{Updated: 1}, 3, 20, "Bank"},
	}
	for _, tc := range cases {
		got, c := Into([]account.Account{tc.local}, []account.Account{tc.source})
		if c != tc.want || got[0].Counter != tc.counter || got[0].UpdatedAt != tc.updated || got[0].Issuer != tc.issuer {
			t.Errorf("%s: counts %+v counter %d updated_at %d issuer %q, want %+v %d %d %q",
				tc.name, c, got[0].Counter, got[0].UpdatedAt, got[0].Issuer, tc.want, tc.counter, tc.updated, tc.issuer)
		}
	}
}

func TestTwoWayCounterSettlesWithoutPingPong(t *testing.T) {
	local, source := []account.Account{hotp("a", 5, 100)}, []account.Account{hotp("a", 7, 90)}
	localOut, sourceOut, here, there := TwoWay(local, source)
	if localOut[0].Counter != 7 || sourceOut[0].Counter != 7 || !here.Changed() || !there.Changed() {
		t.Fatalf("first run: counters %d/%d changed %v/%v, want 7 on both sides and both written", localOut[0].Counter, sourceOut[0].Counter, here.Changed(), there.Changed())
	}
	_, _, here, there = TwoWay(localOut, sourceOut)
	if here.Changed() || there.Changed() {
		t.Fatalf("second run wrote again: %+v / %+v", here, there)
	}
}

func ids(accts []account.Account) []string {
	out := make([]string, len(accts))
	for i, a := range accts {
		out[i] = a.ID
	}
	return out
}

type mergeVectors struct {
	Merge struct {
		Cases []struct {
			Name          string `json:"name"`
			Local         string `json:"local"`
			Source        string `json:"source"`
			LocalAfter    string `json:"local_after"`
			SourceAfter   string `json:"source_after"`
			LocalWritten  bool   `json:"local_written"`
			SourceWritten bool   `json:"source_written"`
			LocalCounts   counts `json:"local_counts"`
			SourceCounts  counts `json:"source_counts"`
		} `json:"cases"`
	} `json:"merge"`
}

type counts struct {
	Added     int `json:"added"`
	Updated   int `json:"updated"`
	Trashed   int `json:"trashed"`
	Unchanged int `json:"unchanged"`
	Duplicate int `json:"duplicate"`
	Invalid   int `json:"invalid"`
}

func TestMergeVectors(t *testing.T) {
	var v mergeVectors
	spectest.Load(t, &v)
	if len(v.Merge.Cases) == 0 {
		t.Fatal("merge vectors are empty")
	}
	for _, c := range v.Merge.Cases {
		t.Run(c.Name, func(t *testing.T) {
			local := parse(t, c.Local)
			source := parse(t, c.Source)
			if account.AssignHandles(local) {
				t.Fatal("a local input account has no handle")
			}
			localOut, sourceOut, here, there := TwoWay(local, source)
			if got := counts(here); got != c.LocalCounts {
				t.Errorf("local counts %+v, want %+v", got, c.LocalCounts)
			}
			if got := counts(there); got != c.SourceCounts {
				t.Errorf("source counts %+v, want %+v", got, c.SourceCounts)
			}
			if here.Changed() != c.LocalWritten || there.Changed() != c.SourceWritten {
				t.Fatalf("written %v/%v, want %v/%v", here.Changed(), there.Changed(), c.LocalWritten, c.SourceWritten)
			}
			if got := written(t, localOut, here.Changed(), c.Local); got != c.LocalAfter {
				t.Errorf("local after\n%s\nwant\n%s", got, c.LocalAfter)
			}
			if got := written(t, sourceOut, there.Changed(), c.Source); got != c.SourceAfter {
				t.Errorf("source after\n%s\nwant\n%s", got, c.SourceAfter)
			}
		})
	}
}

func parse(t *testing.T, s string) []account.Account {
	t.Helper()
	accts, err := account.ParseCanonicalJSON([]byte(s))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return accts
}

// written is the file a side holds after the run: unchanged when nothing was
// written, otherwise the merged list after the handle pass every save runs.
func written(t *testing.T, accts []account.Account, changed bool, input string) string {
	t.Helper()
	if !changed {
		return input
	}
	DedupeHandles(accts)
	account.AssignHandles(accts)
	if err := account.CheckHandleUniqueness(accts); err != nil {
		t.Fatal(err)
	}
	out, err := account.CanonicalJSON(accts)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}
