package exporters

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/base32x"
	"github.com/ibrahemid/tessera/go/internal/importers"
)

func mustSecret(t *testing.T, b32 string) []byte {
	t.Helper()
	s, err := base32x.Decode(b32)
	if err != nil {
		t.Fatalf("decode %q: %v", b32, err)
	}
	return s
}

// roundTripAccounts is the shared fixture every exporter is checked against.
// It deliberately avoids shapes the import ladder normalizes away, which are
// pinned separately: an issuer-less account (2FAS re-reads its service name as
// the issuer) and a Steam account whose issuer is not "Steam".
func roundTripAccounts(t *testing.T) []account.Account {
	t.Helper()
	return []account.Account{
		{ID: "a1", Type: account.TOTP, Issuer: "GitHub", Account: "user@example.com",
			Secret: mustSecret(t, "JBSWY3DPEHPK3PXP"), Algorithm: "SHA1", Digits: 6, Period: 30},
		{ID: "b2", Type: account.TOTP, Issuer: "Example Corp", Account: "bob",
			Secret: mustSecret(t, "GEZDGNBVGY3TQOJQ"), Algorithm: "SHA256", Digits: 8, Period: 60},
		{ID: "c3", Type: account.HOTP, Issuer: "Bank", Account: "ops",
			Secret: mustSecret(t, "MFRGGZDFMZTWQ2LK"), Algorithm: "SHA512", Digits: 8, Counter: 7},
		{ID: "d4", Type: account.Steam, Issuer: "Steam", Account: "gaben",
			Secret: mustSecret(t, "NBSWY3DPEB3W64TM"), Algorithm: "SHA1", Digits: 5, Period: 30},
		{ID: "e5", Type: account.TOTP, Issuer: "A, Inc", Account: "x:y@example.com",
			Secret: mustSecret(t, "KRSXG5CTMVRXEZLU"), Algorithm: "SHA1", Digits: 6, Period: 30},
	}
}

// sameAccount compares the fields an export can carry. An importer returns no
// id, timestamps, folder, handle or tags, so those are out of scope. Period is
// compared only for time-based accounts: an HOTP entry has no period to store,
// and the two import paths disagree on what to default it to (a source that
// stores no period yields 0, an otpauth URI yields 30).
func sameAccount(t *testing.T, got, want account.Account, ctx string) {
	t.Helper()
	if got.Type != want.Type {
		t.Errorf("%s: type = %q, want %q", ctx, got.Type, want.Type)
	}
	if got.Issuer != want.Issuer {
		t.Errorf("%s: issuer = %q, want %q", ctx, got.Issuer, want.Issuer)
	}
	if got.Account != want.Account {
		t.Errorf("%s: account = %q, want %q", ctx, got.Account, want.Account)
	}
	if base32x.EncodeNoPad(got.Secret) != base32x.EncodeNoPad(want.Secret) {
		t.Errorf("%s: secret does not round-trip", ctx)
	}
	if got.Algorithm != want.Algorithm {
		t.Errorf("%s: algorithm = %q, want %q", ctx, got.Algorithm, want.Algorithm)
	}
	if got.Digits != want.Digits {
		t.Errorf("%s: digits = %d, want %d", ctx, got.Digits, want.Digits)
	}
	if want.Type != account.HOTP && got.Period != want.Period {
		t.Errorf("%s: period = %d, want %d", ctx, got.Period, want.Period)
	}
	if got.Counter != want.Counter {
		t.Errorf("%s: counter = %d, want %d", ctx, got.Counter, want.Counter)
	}
}

// TestRoundTripThroughImporters is the contract for every file-based exporter:
// what Tessera writes, Tessera reads back unchanged.
func TestRoundTripThroughImporters(t *testing.T) {
	accts := roundTripAccounts(t)
	for _, name := range []string{"aegis", "2fas", "bitwarden", "proton", "andotp", "apple-csv"} {
		t.Run(name, func(t *testing.T) {
			e, ok := Lookup(name)
			if !ok {
				t.Fatalf("format %q is not registered", name)
			}
			files, skipped, err := e.Render(accts)
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			if len(files) != 1 {
				t.Fatalf("want 1 file, got %d", len(files))
			}
			want := accts
			if len(skipped) > 0 {
				want = nil
				for _, a := range accts {
					if !isSkipped(skipped, a) {
						want = append(want, a)
					}
				}
			}
			got, source, recognized, err := importers.Parse(files[0].Data)
			if !recognized {
				t.Fatalf("re-import did not recognize the export")
			}
			if err != nil {
				t.Fatalf("re-import (%s): %v", source, err)
			}
			if len(got) != len(want) {
				t.Fatalf("re-imported %d accounts from %s, want %d", len(got), source, len(want))
			}
			for i := range want {
				sameAccount(t, got[i], want[i], name+" #"+want[i].ID)
			}
		})
	}
}

func isSkipped(skipped []Skipped, a account.Account) bool {
	for _, s := range skipped {
		if s.Label == label(a) {
			return true
		}
	}
	return false
}

// TestExportIsDeterministic pins that re-exporting an unchanged vault produces
// identical bytes, so an export can be diffed or checksummed.
func TestExportIsDeterministic(t *testing.T) {
	accts := roundTripAccounts(t)
	for _, e := range All() {
		if e.Name() == "google-migration" {
			continue // batch_id is random by design
		}
		first, _, err := e.Render(accts)
		if err != nil {
			t.Fatalf("%s: render: %v", e.Name(), err)
		}
		second, _, err := e.Render(accts)
		if err != nil {
			t.Fatalf("%s: render: %v", e.Name(), err)
		}
		for i := range first {
			if string(first[i].Data) != string(second[i].Data) {
				t.Errorf("%s: two renders of the same accounts differ", e.Name())
			}
		}
	}
}

// TestProtonSkipsHOTP: Proton Authenticator stores Totp and Steam entries only,
// so an HOTP account is reported, never silently rewritten as TOTP.
func TestProtonSkipsHOTP(t *testing.T) {
	e, _ := Lookup("proton")
	hotp := account.Account{ID: "h", Type: account.HOTP, Issuer: "Bank", Account: "ops",
		Secret: mustSecret(t, "JBSWY3DPEHPK3PXP"), Algorithm: "SHA1", Digits: 6, Counter: 3}
	_, skipped, err := e.Render([]account.Account{hotp})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if len(skipped) != 1 || skipped[0].Label != "Bank (ops)" {
		t.Fatalf("want the HOTP account reported as skipped, got %+v", skipped)
	}
}

// TestSkipLabelsCarryNoSecret guards the rule that a skip line names an account
// and never its key material.
func TestSkipLabelsCarryNoSecret(t *testing.T) {
	const b32 = "JBSWY3DPEHPK3PXP"
	a := account.Account{ID: "s", Type: account.Steam, Issuer: "Steam", Account: "gaben",
		Secret: mustSecret(t, b32), Algorithm: "SHA1", Digits: 5, Period: 30}
	for _, e := range All() {
		_, skipped, err := e.Render([]account.Account{a})
		if err != nil {
			t.Fatalf("%s: render: %v", e.Name(), err)
		}
		for _, s := range skipped {
			if strings.Contains(s.Label+s.Reason, b32) {
				t.Errorf("%s: skip line leaks the secret: %+v", e.Name(), s)
			}
		}
	}
}

// TestLookupAndNames pins the registry surface the CLI's --format flag reads.
func TestLookupAndNames(t *testing.T) {
	want := []string{"2fas", "aegis", "andotp", "apple-csv", "bitwarden", "google-migration", "proton"}
	got := Names()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("Names() = %v, want %v", got, want)
	}
	for _, n := range want {
		e, ok := Lookup(n)
		if !ok {
			t.Fatalf("Lookup(%q) missing", n)
		}
		if e.Description() == "" {
			t.Errorf("%s has no description", n)
		}
	}
	if _, ok := Lookup("nope"); ok {
		t.Errorf("Lookup of an unknown format should fail")
	}
}

// TestExportIDIsStableForAnyID: the id fields these formats carry are derived
// from the vault id, and a vault id is not required to be 32 hex characters.
func TestExportIDIsStableForAnyID(t *testing.T) {
	if exportID("a") == exportID("b") {
		t.Fatal("distinct ids must not collide")
	}
	first, second := exportID("a"), exportID("a")
	if first != second {
		t.Fatal("exportID must be deterministic")
	}
	if len(exportID("a")) != 36 {
		t.Fatalf("exportID should be uuid-shaped, got %q", exportID("a"))
	}
}

// renderAndReimport renders accts in format name, decodes the file into v and
// checks that the repo's importer still reads back every account the format
// carried.
func renderAndReimport(t *testing.T, name string, accts []account.Account, v any) []byte {
	t.Helper()
	e, ok := Lookup(name)
	if !ok {
		t.Fatalf("format %q is not registered", name)
	}
	files, skipped, err := e.Render(accts)
	if err != nil || len(files) != 1 {
		t.Fatalf("%s: render: %d files, %v", name, len(files), err)
	}
	if err := json.Unmarshal(files[0].Data, v); err != nil {
		t.Fatalf("%s: decode: %v", name, err)
	}
	got, source, recognized, err := importers.Parse(files[0].Data)
	if !recognized || err != nil {
		t.Fatalf("%s: re-import (%s): recognized %v, %v", name, source, recognized, err)
	}
	if len(got) != len(accts)-len(skipped) {
		t.Fatalf("%s: re-imported %d accounts, want %d", name, len(got), len(accts)-len(skipped))
	}
	return files[0].Data
}

func folderFixture(t *testing.T) []account.Account {
	t.Helper()
	accts := roundTripAccounts(t)
	accts[0].Folder, accts[0].Tags = "Work", []string{"code", "oss"}
	accts[1].Folder = "Personal"
	accts[2].Folder, accts[2].UpdatedAt = "Work", 50
	return accts
}

func TestAegisWritesFoldersAsGroups(t *testing.T) {
	var got struct {
		DB struct {
			Entries []struct {
				Groups []string `json:"groups"`
			} `json:"entries"`
			Groups []aegisGroup `json:"groups"`
		} `json:"db"`
	}
	renderAndReimport(t, "aegis", folderFixture(t), &got)
	work, personal := folderID("Work"), folderID("Personal")
	if want := []aegisGroup{{work, "Work"}, {personal, "Personal"}}; !reflect.DeepEqual(got.DB.Groups, want) {
		t.Errorf("groups = %+v, want %+v", got.DB.Groups, want)
	}
	want := [][]string{{work}, {personal}, {work}, {}, {}}
	for i, e := range got.DB.Entries {
		if !reflect.DeepEqual(e.Groups, want[i]) {
			t.Errorf("entry %d groups = %v, want %v", i, e.Groups, want[i])
		}
	}
}

func Test2FASWritesFoldersAsGroups(t *testing.T) {
	var got struct {
		Services []struct {
			GroupID *string `json:"groupId"`
		} `json:"services"`
		Groups []twofasGroup `json:"groups"`
	}
	renderAndReimport(t, "2fas", folderFixture(t), &got)
	work, personal := folderID("Work"), folderID("Personal")
	if want := []twofasGroup{{work, "Work", true, 50_000}, {personal, "Personal", true, 0}}; !reflect.DeepEqual(got.Groups, want) {
		t.Errorf("groups = %+v, want %+v", got.Groups, want)
	}
	want := []*string{&work, &personal, &work, nil, nil}
	for i, s := range got.Services {
		if !reflect.DeepEqual(s.GroupID, want[i]) {
			t.Errorf("service %d groupId = %v, want %v", i, s.GroupID, want[i])
		}
	}
}

func TestBitwardenWritesFolders(t *testing.T) {
	var got struct {
		Folders []bitwardenFolder `json:"folders"`
		Items   []struct {
			FolderID *string `json:"folderId"`
		} `json:"items"`
	}
	renderAndReimport(t, "bitwarden", folderFixture(t), &got)
	work, personal := folderID("Work"), folderID("Personal")
	if want := []bitwardenFolder{{work, "Work"}, {personal, "Personal"}}; !reflect.DeepEqual(got.Folders, want) {
		t.Errorf("folders = %+v, want %+v", got.Folders, want)
	}
	want := []*string{&work, &personal, &work, nil, nil}
	for i, it := range got.Items {
		if !reflect.DeepEqual(it.FolderID, want[i]) {
			t.Errorf("item %d folderId = %v, want %v", i, it.FolderID, want[i])
		}
	}
}

func TestBitwardenOmitsFoldersWithNone(t *testing.T) {
	var got map[string]json.RawMessage
	data := renderAndReimport(t, "bitwarden", roundTripAccounts(t), &got)
	if _, has := got["folders"]; has || bytes.Contains(data, []byte(`"folders"`)) {
		t.Fatal("an export with no folders carries a folders key")
	}
}

func TestAndOTPWritesTags(t *testing.T) {
	var got []struct {
		Tags []string `json:"tags"`
	}
	renderAndReimport(t, "andotp", folderFixture(t), &got)
	want := [][]string{{"code", "oss"}, {}, {}, {}, {}}
	for i, e := range got {
		if !reflect.DeepEqual(e.Tags, want[i]) {
			t.Errorf("entry %d tags = %v, want %v", i, e.Tags, want[i])
		}
	}
}

func TestFolderIDIsDerivedFromTheName(t *testing.T) {
	if got, want := folderID("Work"), "d1cfa62f-e404-0831-4c85-4f64b43c1f3b"; got != want {
		t.Fatalf("folderID(Work) = %q, want %q", got, want)
	}
	if folderID("Work") == folderID("work") {
		t.Fatal("folder ids that differ only in case are the same id")
	}
	if folderID("a") == exportID("a") {
		t.Fatal("a folder name and an account id gave the same id")
	}
	if len(folderID("Work")) != 36 {
		t.Fatalf("folderID should be uuid-shaped, got %q", folderID("Work"))
	}
}
