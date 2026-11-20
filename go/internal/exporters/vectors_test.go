package exporters

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/qr"
	"github.com/ibrahemid/tessera/go/internal/spectest"
)

type exportFormatVector struct {
	Format string `json:"format"`
	Files  []struct {
		Name    string `json:"name"`
		Content string `json:"content"`
	} `json:"files"`
	Codes []struct {
		Name string `json:"name"`
		URI  string `json:"uri"`
	} `json:"codes"`
	Skipped []struct {
		Label  string `json:"label"`
		Kind   string `json:"kind"`
		Reason string `json:"reason"`
	} `json:"skipped"`
}

type exportVectors struct {
	Exports struct {
		BatchID              int32                `json:"batch_id"`
		CanonicalJSON        string               `json:"canonical_json"`
		Formats              []exportFormatVector `json:"formats"`
		MigrationEmptySecret struct {
			CanonicalJSON string `json:"canonical_json"`
			exportFormatVector
		} `json:"migration_empty_secret"`
	} `json:"exports"`
}

func loadExportVectors(t *testing.T) (exportVectors, []account.Account) {
	t.Helper()
	var v exportVectors
	spectest.Load(t, &v)
	accts, err := account.ParseCanonicalJSON([]byte(v.Exports.CanonicalJSON))
	if err != nil {
		t.Fatalf("parse exports canonical_json: %v", err)
	}
	return v, accts
}

func TestExportVectorsCoverEveryFormat(t *testing.T) {
	v, _ := loadExportVectors(t)
	var got []string
	for _, f := range v.Exports.Formats {
		got = append(got, f.Format)
	}
	if !slices.Equal(got, Names()) {
		t.Fatalf("exports vectors cover %v, want every registered format %v", got, Names())
	}
}

func TestExportVectorsMatchRender(t *testing.T) {
	v, accts := loadExportVectors(t)
	for _, f := range v.Exports.Formats {
		t.Run(f.Format, func(t *testing.T) {
			e, ok := Lookup(f.Format)
			if !ok {
				t.Fatalf("%s is not registered", f.Format)
			}
			var skipped []Skipped
			if f.Format == "google-migration" {
				var codes []MigrationCode
				codes, skipped = MigrationCodes(accts, v.Exports.BatchID)
				if len(codes) != len(f.Codes) {
					t.Fatalf("%d codes, want %d", len(codes), len(f.Codes))
				}
				for i, c := range codes {
					if c.Name != f.Codes[i].Name || c.URI != f.Codes[i].URI {
						t.Fatalf("code %d = %s %s\nwant %s %s", i, c.Name, c.URI, f.Codes[i].Name, f.Codes[i].URI)
					}
				}
			} else {
				var files []File
				var err error
				files, skipped, err = e.Render(accts)
				if err != nil {
					t.Fatalf("render: %v", err)
				}
				if len(files) != len(f.Files) {
					t.Fatalf("%d files, want %d", len(files), len(f.Files))
				}
				for i, file := range files {
					if file.Name != f.Files[i].Name || string(file.Data) != f.Files[i].Content {
						t.Fatalf("file %d = %s\n%s\nwant %s\n%s", i, file.Name, file.Data, f.Files[i].Name, f.Files[i].Content)
					}
				}
			}
			if len(skipped) != len(f.Skipped) {
				t.Fatalf("skipped %+v, want %+v", skipped, f.Skipped)
			}
			for i, s := range skipped {
				w := f.Skipped[i]
				if s.Label != w.Label || string(s.Kind) != w.Kind || s.Reason != w.Reason {
					t.Fatalf("skipped %d = %+v, want %+v", i, s, w)
				}
			}
		})
	}
}

// TestMigrationRenderEncodesTheVectorURIs: the PNGs Render writes with the
// vector batch id decode to exactly the URIs the vectors list.
func TestMigrationRenderEncodesTheVectorURIs(t *testing.T) {
	v, accts := loadExportVectors(t)
	i := slices.IndexFunc(v.Exports.Formats, func(f exportFormatVector) bool { return f.Format == "google-migration" })
	if i < 0 {
		t.Fatal("no google-migration vectors")
	}
	want := v.Exports.Formats[i].Codes
	e := migrationExporter{batchID: func() (int32, error) { return v.Exports.BatchID, nil }}
	files, _, err := e.Render(accts)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if len(files) != len(want) {
		t.Fatalf("%d files, want %d", len(files), len(want))
	}
	dir := t.TempDir()
	for i, f := range files {
		if f.Name != want[i].Name {
			t.Fatalf("file %d is %s, want %s", i, f.Name, want[i].Name)
		}
		path := filepath.Join(dir, f.Name)
		if err := os.WriteFile(path, f.Data, 0o600); err != nil {
			t.Fatal(err)
		}
		payloads, err := qr.DecodeFileAll(path)
		if err != nil || len(payloads) != 1 {
			t.Fatalf("decode %s: %v (%d codes)", f.Name, err, len(payloads))
		}
		if payloads[0] != want[i].URI {
			t.Fatalf("%s encodes %s, want %s", f.Name, payloads[0], want[i].URI)
		}
	}
}

var errNoBatchID = errors.New("no batch id")

func TestMigrationRenderReportsABatchIDFailure(t *testing.T) {
	eligible, _ := migrationFixture(t)
	e := migrationExporter{batchID: func() (int32, error) { return 0, errNoBatchID }}
	if _, _, err := e.Render(eligible); !errors.Is(err, errNoBatchID) {
		t.Fatalf("err = %v, want the batch id failure", err)
	}
}

func TestMigrationRenderDrawsNoBatchIDWithNothingToEncode(t *testing.T) {
	steam := account.Account{ID: "s", Type: account.Steam, Issuer: "Steam", Account: "gaben",
		Secret: mustSecret(t, "NBSWY3DPEB3W64TM"), Algorithm: "SHA1", Digits: 5, Period: 30}
	e := migrationExporter{batchID: func() (int32, error) { return 0, errNoBatchID }}
	files, skipped, err := e.Render([]account.Account{steam})
	if err != nil || len(files) != 0 || len(skipped) != 1 {
		t.Fatalf("files %d skipped %d err %v; want no files, one skip, no error", len(files), len(skipped), err)
	}
}

func TestRegisteredMigrationExporterDrawsARandomBatchID(t *testing.T) {
	e, ok := Lookup("google-migration")
	if !ok {
		t.Fatal("google-migration is not registered")
	}
	m, ok := e.(migrationExporter)
	if !ok || m.batchID == nil {
		t.Fatal("the registered google-migration exporter has no batch id source")
	}
	seen := map[int32]bool{}
	for range 8 {
		id, err := m.batchID()
		if err != nil {
			t.Fatal(err)
		}
		seen[id] = true
	}
	if len(seen) < 2 {
		t.Fatal("eight draws gave one batch id; the registered source is not random")
	}
}

func TestMigrationCodesWithNothingEligible(t *testing.T) {
	steam := account.Account{ID: "s", Type: account.Steam, Issuer: "Steam", Account: "gaben",
		Secret: mustSecret(t, "NBSWY3DPEB3W64TM"), Algorithm: "SHA1", Digits: 5, Period: 30}
	codes, skipped := MigrationCodes([]account.Account{steam}, 1)
	if codes != nil || len(skipped) != 1 || skipped[0].Kind != SkipUnsupportedType {
		t.Fatalf("codes %v skipped %+v; want no codes and one unsupported_type skip", codes, skipped)
	}
}

func TestMigrationEmptySecretVector(t *testing.T) {
	v, _ := loadExportVectors(t)
	w := v.Exports.MigrationEmptySecret
	accts, err := account.ParseCanonicalJSON([]byte(w.CanonicalJSON))
	if err != nil {
		t.Fatalf("parse migration_empty_secret canonical_json: %v", err)
	}
	codes, skipped := MigrationCodes(accts, v.Exports.BatchID)
	if len(codes) != len(w.Codes) || len(codes) == 0 {
		t.Fatalf("%d codes, want %d", len(codes), len(w.Codes))
	}
	for i, c := range codes {
		if c.Name != w.Codes[i].Name || c.URI != w.Codes[i].URI {
			t.Fatalf("code %d = %s %s, want %s %s", i, c.Name, c.URI, w.Codes[i].Name, w.Codes[i].URI)
		}
	}
	if len(skipped) != len(w.Skipped) {
		t.Fatalf("skipped %+v, want %+v", skipped, w.Skipped)
	}
	for i, s := range skipped {
		want := w.Skipped[i]
		if s.Label != want.Label || string(s.Kind) != want.Kind || s.Reason != want.Reason {
			t.Fatalf("skipped %d = %+v, want %+v", i, s, want)
		}
	}
	if !slices.ContainsFunc(skipped, func(s Skipped) bool { return s.Kind == SkipEmptySecret }) {
		t.Fatalf("no empty_secret skip in %+v", skipped)
	}
}

func TestSkipKinds(t *testing.T) {
	base := account.Account{ID: "k", Type: account.TOTP, Issuer: "Kind", Account: "x",
		Secret: mustSecret(t, "JBSWY3DPEHPK3PXP"), Algorithm: "SHA1", Digits: 6, Period: 30}
	with := func(f func(*account.Account)) account.Account {
		a := base
		f(&a)
		return a
	}
	cases := []struct {
		format string
		acct   account.Account
		want   SkipKind
	}{
		{"proton", with(func(a *account.Account) { a.Type = account.HOTP }), SkipUnsupportedType},
		{"apple-csv", with(func(a *account.Account) { a.Issuer = "@home" }), SkipFormulaName},
		{"apple-csv", with(func(a *account.Account) { a.Account = "-x" }), SkipFormulaName},
		{"apple-csv", with(func(a *account.Account) { a.Note = "=SUM(1)" }), SkipFormulaNote},
		{"google-migration", with(func(a *account.Account) { a.Type, a.Digits = account.Steam, 5 }), SkipUnsupportedType},
		{"google-migration", with(func(a *account.Account) { a.Algorithm = "MD5" }), SkipUnsupportedAlgorithm},
		{"google-migration", with(func(a *account.Account) { a.Digits = 7 }), SkipUnsupportedDigits},
		{"google-migration", with(func(a *account.Account) { a.Period = 60 }), SkipUnsupportedPeriod},
		{"google-migration", with(func(a *account.Account) { a.Secret = []byte{} }), SkipEmptySecret},
		{"google-migration", with(func(a *account.Account) { a.Secret = nil }), SkipEmptySecret},
	}
	for _, c := range cases {
		e, _ := Lookup(c.format)
		_, skipped, err := e.Render([]account.Account{c.acct})
		if err != nil {
			t.Fatalf("%s: %v", c.format, err)
		}
		if len(skipped) != 1 || skipped[0].Kind != c.want {
			t.Errorf("%s %+v: skipped %+v, want one %s", c.format, c.acct, skipped, c.want)
		}
	}
}
