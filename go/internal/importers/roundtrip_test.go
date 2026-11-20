package importers_test

import (
	"slices"
	"testing"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/detect"
	"github.com/ibrahemid/tessera/go/internal/exporters"
	"github.com/ibrahemid/tessera/go/internal/importers"
)

type carried struct {
	folder, tags, note, pinned bool
}

// orgCarried is what each Tessera export format holds of an account's
// organisation; everything else is lost on the way back in.
var orgCarried = map[string]carried{
	"aegis":            {folder: true, note: true, pinned: true},
	"2fas":             {folder: true},
	"bitwarden":        {folder: true, note: true, pinned: true},
	"proton":           {note: true},
	"andotp":           {tags: true},
	"apple-csv":        {note: true},
	"google-migration": {},
}

func orgRoundTripAccounts() []account.Account {
	totp := func(id, issuer, name string) account.Account {
		return account.Account{ID: id, Type: account.TOTP, Issuer: issuer, Account: name,
			Secret: []byte("12345678901234567890"), Algorithm: "SHA1", Digits: 6, Period: 30}
	}
	a := totp("a1", "GitHub", "user@example.com")
	a.Folder, a.Tags, a.Note, a.Pinned = "Work", []string{"dev", "oss"}, "Recovery codes in the safe\nsecond line", true
	b := totp("b2", "Example Corp", "bob")
	b.Folder, b.Tags = "Work", []string{"ops"}
	c := totp("c3", "Bank", "ops")
	c.Folder, c.Note, c.Pinned = "Finance", "Desk 12", true
	d := totp("d4", "Plain", "none")
	return []account.Account{a, b, c, d}
}

func importOrgRoundTrip(t *testing.T, format string, accts []account.Account) []account.Account {
	t.Helper()
	if format == "google-migration" {
		codes, skipped := exporters.MigrationCodes(accts, 1)
		if len(skipped) > 0 {
			t.Fatalf("skipped %v", skipped)
		}
		var out []account.Account
		for _, code := range codes {
			got, errs := detect.ParseText(code.URI)
			if len(errs) > 0 {
				t.Fatalf("parse %s: %v", code.Name, errs)
			}
			out = append(out, got...)
		}
		return out
	}
	e, ok := exporters.Lookup(format)
	if !ok {
		t.Fatalf("format %q is not registered", format)
	}
	files, skipped, err := e.Render(accts)
	if err != nil || len(skipped) > 0 || len(files) != 1 {
		t.Fatalf("render: files=%d skipped=%v err=%v", len(files), skipped, err)
	}
	got, source, ok, err := importers.Parse(files[0].Data)
	if !ok || err != nil {
		t.Fatalf("re-import (%s): ok=%v err=%v", source, ok, err)
	}
	return got
}

func TestExportImportKeepsOrganisation(t *testing.T) {
	accts := orgRoundTripAccounts()
	for _, format := range exporters.Names() {
		c, ok := orgCarried[format]
		if !ok {
			t.Fatalf("format %q has no organisation round-trip entry", format)
		}
		t.Run(format, func(t *testing.T) {
			got := importOrgRoundTrip(t, format, accts)
			if len(got) != len(accts) {
				t.Fatalf("re-imported %d accounts, want %d", len(got), len(accts))
			}
			for i, want := range accts {
				g := got[i]
				if g.Issuer != want.Issuer {
					t.Fatalf("#%d issuer %q, want %q", i, g.Issuer, want.Issuer)
				}
				var w account.Account
				if c.folder {
					w.Folder = want.Folder
				}
				if c.tags {
					w.Tags = want.Tags
				}
				if c.note {
					w.Note = want.Note
				}
				if c.pinned {
					w.Pinned = want.Pinned
				}
				if g.Folder != w.Folder || !slices.Equal(g.Tags, w.Tags) || g.Note != w.Note || g.Pinned != w.Pinned {
					t.Errorf("%s: folder=%q tags=%q note=%q pinned=%v, want folder=%q tags=%q note=%q pinned=%v",
						want.ID, g.Folder, g.Tags, g.Note, g.Pinned, w.Folder, w.Tags, w.Note, w.Pinned)
				}
			}
		})
	}
}
