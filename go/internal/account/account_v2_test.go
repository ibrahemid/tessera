package account

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
)

const v1Sample = `[{"account":"alice@example.com","algorithm":"SHA1","counter":0,"created_at":1700000000,` +
	`"digits":6,"folder":"","id":"id1","issuer":"ACME","period":30,"pinned":false,` +
	`"secret":"MTIzNDU2Nzg5MDEyMzQ1Njc4OTA=","tags":[],"type":"totp","updated_at":1700000000}]`

func v2Sample() Account {
	return Account{
		ID: "id1", Type: TOTP, Issuer: "ACME", Account: "alice@example.com",
		Secret: []byte("12345678901234567890"), Algorithm: "SHA1", Digits: 6, Period: 30,
		CreatedAt: 1700000000, UpdatedAt: 1700000000,
	}
}

func mustCanonical(t *testing.T, accts ...Account) string {
	t.Helper()
	b, err := CanonicalJSON(accts)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestV2FieldsAtDefaultSerializeAsV1(t *testing.T) {
	if got := mustCanonical(t, v2Sample()); got != v1Sample {
		t.Fatalf("defaults changed the bytes:\n got %s\nwant %s", got, v1Sample)
	}
	if FormatVersion([]Account{v2Sample()}) != 1 {
		t.Fatal("v1-only payload must be version 1")
	}
}

func TestV2FieldPositions(t *testing.T) {
	cases := []struct {
		name string
		set  func(*Account)
		want string
	}{
		{"deleted_at", func(a *Account) { a.DeletedAt = 1760000000 },
			`"created_at":1700000000,"deleted_at":1760000000,"digits":6,`},
		{"note", func(a *Account) { a.Note = "backup codes\nin the safe" },
			`"issuer":"ACME","note":"backup codes\nin the safe","period":30,`},
		{"order", func(a *Account) { a.Order = 3 },
			`"issuer":"ACME","order":3,"period":30,`},
	}
	for _, c := range cases {
		a := v2Sample()
		c.set(&a)
		got := mustCanonical(t, a)
		if !strings.Contains(got, c.want) {
			t.Errorf("%s: %s missing %s", c.name, got, c.want)
		}
		if FormatVersion([]Account{v2Sample(), a}) != 2 {
			t.Errorf("%s: version must be 2", c.name)
		}
		back, err := ParseCanonicalJSON([]byte(got))
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if again := mustCanonical(t, back...); again != got {
			t.Errorf("%s: round trip changed bytes:\n%s\n%s", c.name, got, again)
		}
	}
	a := v2Sample()
	a.DeletedAt, a.Note, a.Order = 1760000000, "n", 2
	want := `"created_at":1700000000,"deleted_at":1760000000,"digits":6,"folder":"","id":"id1","issuer":"ACME","note":"n","order":2,"period":30,`
	if got := mustCanonical(t, a); !strings.Contains(got, want) {
		t.Errorf("all three: %s", got)
	}
	a.DeletedAt, a.Note, a.Order = 0, "", 0
	if got := mustCanonical(t, a); got != v1Sample {
		t.Errorf("clearing v2 fields must restore v1 bytes: %s", got)
	}
}

func TestExtensionFieldsRoundTripSorted(t *testing.T) {
	ext := `"aaa_ext":{"z":[1,-2,{"k":null}],"a":"q\"\\/<>&\u0001` + "\u2028é" + `"},"zzz_ext":-9007199254740993`
	in := strings.Replace(v1Sample, `{"account"`, `{`+ext+`,"account"`, 1)
	accts, err := ParseCanonicalJSON([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(accts[0].Extra) != 2 {
		t.Fatalf("extra = %v", accts[0].Extra)
	}
	got := mustCanonical(t, accts...)
	want := `[{"aaa_ext":{"a":"q\"\\/<>&\u0001\u2028é","z":[1,-2,{"k":null}]},"account"`
	if !strings.HasPrefix(got, want) {
		t.Fatalf("got %s\nwant prefix %s", got, want)
	}
	if !strings.HasSuffix(got, `"updated_at":1700000000,"zzz_ext":-9007199254740993}]`) {
		t.Fatalf("extension not sorted after updated_at: %s", got)
	}
	again, err := ParseCanonicalJSON([]byte(got))
	if err != nil {
		t.Fatal(err)
	}
	if mustCanonical(t, again...) != got {
		t.Fatal("canonical output is not a fixed point")
	}
}

func TestPayloadStrictness(t *testing.T) {
	sub := func(old, new string) string { return strings.Replace(v1Sample, old, new, 1) }
	cases := []struct {
		name    string
		payload string
		want    error
	}{
		{"float in extension", sub(`"tags":[]`, `"tags":[],"x":1.5`), ErrNonCanonicalNumber},
		{"exponent", sub(`"counter":0`, `"counter":1e3`), ErrNonCanonicalNumber},
		{"negative zero", sub(`"counter":0`, `"counter":-0`), ErrNonCanonicalNumber},
		{"leading zero", sub(`"digits":6`, `"digits":06`), ErrMalformedPayload},
		{"beyond int64", sub(`"counter":0`, `"counter":9223372036854775808`), ErrNonCanonicalNumber},
		{"digits 6.0", sub(`"digits":6`, `"digits":6.0`), ErrNonCanonicalNumber},
		{"pinned 1", sub(`"pinned":false`, `"pinned":1`), ErrFieldType},
		{"pinned null", sub(`"pinned":false`, `"pinned":null`), ErrFieldType},
		{"counter string", sub(`"counter":0`, `"counter":"0"`), ErrFieldType},
		{"issuer number", sub(`"issuer":"ACME"`, `"issuer":5`), ErrFieldType},
		{"tags null", sub(`"tags":[]`, `"tags":null`), ErrFieldType},
		{"tag not string", sub(`"tags":[]`, `"tags":[1]`), ErrFieldType},
		{"secret url alphabet", sub(`"MTIzNDU2Nzg5MDEyMzQ1Njc4OTA="`, `"_w=="`), ErrNonCanonicalBase64},
		{"secret no padding", sub(`"MTIzNDU2Nzg5MDEyMzQ1Njc4OTA="`, `"MTIzNDU2Nzg5MDEyMzQ1Njc4OTA"`), ErrNonCanonicalBase64},
		{"secret trailing bits", sub(`"MTIzNDU2Nzg5MDEyMzQ1Njc4OTA="`, `"MTIzNDU2Nzg5MDEyMzQ1Njc4OTB="`), ErrNonCanonicalBase64},
		{"secret line break", sub(`"MTIzNDU2Nzg5MDEyMzQ1Njc4OTA="`, `"MTIzNDU2Nzg5\nMDEyMzQ1Njc4OTA="`), ErrNonCanonicalBase64},
		{"duplicate top key", sub(`"tags":[]`, `"tags":[],"tags":[]`), ErrDuplicateKey},
		{"duplicate in extension", sub(`"tags":[]`, `"tags":[],"x":{"a":1,"a":2}`), ErrDuplicateKey},
		{"duplicate after unescape", sub(`"tags":[]`, `"tags":[],"x":{"a":1,"a":2}`), ErrDuplicateKey},
		{"trailing data", v1Sample + `[]`, ErrMalformedPayload},
		{"not an array", `{}`, ErrMalformedPayload},
		{"null payload", `null`, ErrMalformedPayload},
		{"element not object", `[1]`, ErrMalformedPayload},
	}
	for _, c := range cases {
		_, err := ParseCanonicalJSON([]byte(c.payload))
		if err == nil {
			t.Errorf("%s: accepted", c.name)
			continue
		}
		if !errors.Is(err, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, err, c.want)
		}
	}
}

func TestExtensionKeyCollisionRejected(t *testing.T) {
	a := v2Sample()
	a.Extra = map[string]json.RawMessage{"note": json.RawMessage(`"x"`)}
	if _, err := CanonicalJSON([]Account{a}); !errors.Is(err, ErrExtensionKey) {
		t.Fatalf("got %v", err)
	}
	if err := a.Validate(); !errors.Is(err, ErrExtensionKey) {
		t.Fatalf("validate got %v", err)
	}
	a.Extra = map[string]json.RawMessage{"x": json.RawMessage(`1.0`)}
	if _, err := CanonicalJSON([]Account{a}); !errors.Is(err, ErrNonCanonicalNumber) {
		t.Fatalf("got %v", err)
	}
	a.Extra = map[string]json.RawMessage{"x": json.RawMessage(`{ "b" : " ", "a" : [ ] }`)}
	got := mustCanonical(t, a)
	if !strings.HasSuffix(got, `"updated_at":1700000000,"x":{"a":[],"b":"\u2028"}}]`) {
		t.Fatalf("extension not canonicalized: %s", got)
	}
}

func TestValidateV2Fields(t *testing.T) {
	a := v2Sample()
	a.Type = HOTP
	a.Counter = MaxCounter
	if err := a.Validate(); err != nil {
		t.Fatalf("max counter rejected: %v", err)
	}
	a.Counter = MaxCounter + 1
	if err := a.Validate(); !errors.Is(err, ErrCounterRange) {
		t.Fatalf("counter 2^63-1: %v", err)
	}
	a.Counter = -1
	if err := a.Validate(); !errors.Is(err, ErrCounterRange) {
		t.Fatalf("counter -1: %v", err)
	}
	a = v2Sample()
	a.DeletedAt = -1
	if err := a.Validate(); !errors.Is(err, ErrNegativeDeletedAt) {
		t.Fatalf("deleted_at: %v", err)
	}
	a = v2Sample()
	a.Order = -1
	if err := a.Validate(); !errors.Is(err, ErrNegativeOrder) {
		t.Fatalf("order: %v", err)
	}
	a = v2Sample()
	a.DeletedAt, a.Order, a.Note = 5, 1, "n"
	if err := a.Validate(); err != nil {
		t.Fatalf("valid v2 account rejected: %v", err)
	}
}

func TestTrash(t *testing.T) {
	accts := []Account{{ID: "a"}, {ID: "b", DeletedAt: 10}, {ID: "c"}, {ID: "d", DeletedAt: 1}}
	if accts[0].Trashed() || !accts[1].Trashed() {
		t.Fatal("Trashed method")
	}
	ids := func(as []Account) string {
		var s []string
		for _, a := range as {
			s = append(s, a.ID)
		}
		return strings.Join(s, ",")
	}
	if got := ids(Live(accts)); got != "a,c" {
		t.Errorf("Live = %s", got)
	}
	if got := ids(Trashed(accts)); got != "b,d" {
		t.Errorf("Trashed = %s", got)
	}
	if Live(nil) == nil || Trashed(nil) == nil {
		t.Error("filters must return a non-nil slice")
	}
}

func TestSortForDisplay(t *testing.T) {
	accts := []Account{
		{ID: "9", Issuer: "service10"},
		{ID: "8", Issuer: "Service2"},
		{ID: "7", Issuer: "acme", Account: "b"},
		{ID: "6", Issuer: "ACME", Account: "a"},
		{ID: "5", Issuer: "x", Order: 2},
		{ID: "4", Issuer: "y", Order: 1},
		{ID: "3", Issuer: "acme", Account: "B"},
		{ID: "2", Issuer: "z", Order: 2},
		{ID: "1", Issuer: "Service02"},
	}
	SortForDisplay(accts)
	var got []string
	for _, a := range accts {
		got = append(got, a.ID)
	}
	if s := strings.Join(got, ","); s != "4,2,5,6,3,7,1,8,9" {
		t.Fatalf("order = %s", s)
	}
}

func TestNaturalCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"Service2", "Service10", -1},
		{"a", "A", 0},
		{"a01", "a1", 0},
		{"a", "ab", -1},
		{"É", "é", 0},
		{"x9", "xa", -1},
		{"18446744073709551616", "18446744073709551617", -1},
	}
	for _, c := range cases {
		if got := naturalCompare(c.a, c.b); got != c.want {
			t.Errorf("naturalCompare(%q,%q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestCleanText(t *testing.T) {
	cases := []struct{ in, want string }{
		{"  ACME \t", "ACME"},
		{"Ac\x1b[31mme", "Ac[31mme"},
		{"a\u0000b\u007fc\u0085d\u009fe", "abcde"},
		{"\u202eevil\u202c", "evil"},
		{"\u2066x\u2069", "x"},
		{"\ufeffBOM", "BOM"},
		{"\u00a0nbsp\u3000", "nbsp"},
		{"a\nb\tc", "abc"},
		{"\u200ex\u200f\u061c\u200c\u200d", "\u200ex\u200f\u061c\u200c\u200d"},
		{"\u0634\u0631\u0643\u0629", "\u0634\u0631\u0643\u0629"},
	}
	for _, c := range cases {
		if got := CleanText(c.in); got != c.want {
			t.Errorf("CleanText(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestCleanNote(t *testing.T) {
	cases := []struct{ in, want string }{
		{"line1\r\nline2\rline3\n", "line1\nline2\nline3"},
		{"\tcol\tval\x07\u202e", "col\tval"},
		{"  keep\n\n inner  ", "keep\n\n inner"},
	}
	for _, c := range cases {
		if got := CleanNote(c.in); got != c.want {
			t.Errorf("CleanNote(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func assignHandlesReference(accounts []Account) {
	taken := map[string]bool{}
	var missing []int
	for i, a := range accounts {
		if a.Handle != "" {
			taken[a.Handle] = true
		} else {
			missing = append(missing, i)
		}
	}
	for _, i := range sortedMissing(accounts, missing) {
		base := handleBase(accounts[i].Issuer, accounts[i].Account)
		h := base
		for n := 2; taken[h]; n++ {
			h = base + strconv.Itoa(n)
		}
		accounts[i].Handle = h
		taken[h] = true
	}
}

func sortedMissing(accounts []Account, missing []int) []int {
	out := append([]int(nil), missing...)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0; j-- {
			a, b := accounts[out[j-1]], accounts[out[j]]
			if a.CreatedAt < b.CreatedAt || (a.CreatedAt == b.CreatedAt && a.ID <= b.ID) {
				break
			}
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}

func TestAssignHandlesMatchesReference(t *testing.T) {
	issuers := []string{"ACME", "Acme Corp", "GitHub", "1Password", "", "a1", "a"}
	preset := []string{"", "", "ac2", "ac4", "gi", "a12", "", "ac"}
	var accts []Account
	for i := 0; i < 400; i++ {
		accts = append(accts, Account{
			ID:        fmt.Sprintf("id%03d", (i*37)%400),
			Issuer:    issuers[i%len(issuers)],
			Account:   "user@example.com",
			CreatedAt: int64(i % 13),
			Handle:    preset[i%len(preset)],
		})
	}
	seen := map[string]bool{}
	for i := range accts {
		if h := accts[i].Handle; h != "" {
			if seen[h] {
				accts[i].Handle = ""
			}
			seen[h] = true
		}
	}
	ref := append([]Account(nil), accts...)
	assignHandlesReference(ref)
	AssignHandles(accts)
	for i := range accts {
		if accts[i].Handle != ref[i].Handle {
			t.Fatalf("account %d: got %q, reference %q", i, accts[i].Handle, ref[i].Handle)
		}
	}
	if err := CheckHandleUniqueness(accts); err != nil {
		t.Fatal(err)
	}
}

func TestAssignHandlesLargeCollisionChain(t *testing.T) {
	const n = 20000
	accts := make([]Account, n)
	for i := range accts {
		accts[i] = Account{ID: fmt.Sprintf("id%06d", i), Issuer: "ACME", CreatedAt: int64(i)}
	}
	AssignHandles(accts)
	if accts[0].Handle != "ac" || accts[1].Handle != "ac2" || accts[n-1].Handle != "ac"+strconv.Itoa(n) {
		t.Fatalf("chain: %q %q %q", accts[0].Handle, accts[1].Handle, accts[n-1].Handle)
	}
}
