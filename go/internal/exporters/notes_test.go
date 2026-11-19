package exporters

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/importers"
)

func notedAccounts(t *testing.T) []account.Account {
	t.Helper()
	secret := mustSecret(t, "JBSWY3DPEHPK3PXP")
	return []account.Account{
		{ID: "n1", Type: account.TOTP, Issuer: "GitHub", Account: "me", Secret: secret,
			Algorithm: "SHA1", Digits: 6, Period: 30, Note: "recovery codes in the safe\nsecond line, with \"quotes\""},
		{ID: "n2", Type: account.TOTP, Issuer: "Dropbox", Account: "me", Secret: secret,
			Algorithm: "SHA1", Digits: 6, Period: 30},
	}
}

// TestExportsCarryTheNote: every format with a note field writes the note, and
// the importer reads it back unchanged.
func TestExportsCarryTheNote(t *testing.T) {
	accts := notedAccounts(t)
	for _, name := range []string{"aegis", "bitwarden", "proton", "apple-csv"} {
		t.Run(name, func(t *testing.T) {
			e, _ := Lookup(name)
			files, skipped, err := e.Render(accts)
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			if len(skipped) != 0 {
				t.Fatalf("nothing should be skipped, got %+v", skipped)
			}
			got, source, recognized, err := importers.Parse(files[0].Data)
			if !recognized || err != nil {
				t.Fatalf("re-import: recognized=%v err=%v", recognized, err)
			}
			if len(got) != len(accts) {
				t.Fatalf("re-imported %d accounts from %s, want %d", len(got), source, len(accts))
			}
			for i := range accts {
				if got[i].Note != accts[i].Note {
					t.Errorf("%s note = %q, want %q", accts[i].ID, got[i].Note, accts[i].Note)
				}
			}
		})
	}
}

// TestJSONExportsWriteNullForNoNote: an account without a note keeps the null
// the formats use for it.
func TestJSONExportsWriteNullForNoNote(t *testing.T) {
	accts := notedAccounts(t)[1:]
	cases := []struct {
		format string
		read   func([]byte) (any, error)
	}{
		{"bitwarden", func(data []byte) (any, error) {
			var v struct {
				Items []map[string]any `json:"items"`
			}
			err := json.Unmarshal(data, &v)
			return v.Items[0]["notes"], err
		}},
		{"proton", func(data []byte) (any, error) {
			var v struct {
				Entries []map[string]any `json:"entries"`
			}
			err := json.Unmarshal(data, &v)
			return v.Entries[0]["note"], err
		}},
	}
	for _, c := range cases {
		e, _ := Lookup(c.format)
		files, _, err := e.Render(accts)
		if err != nil {
			t.Fatalf("%s: render: %v", c.format, err)
		}
		got, err := c.read(files[0].Data)
		if err != nil {
			t.Fatalf("%s: parse: %v", c.format, err)
		}
		if got != nil {
			t.Errorf("%s: note = %v, want null", c.format, got)
		}
	}
}

// TestAppleCSVLeavesOutANoteASpreadsheetWouldEvaluate: the account is written,
// the note cell is empty, and the note is reported as skipped.
func TestAppleCSVLeavesOutANoteASpreadsheetWouldEvaluate(t *testing.T) {
	a := notedAccounts(t)[0]
	a.Note = "=HYPERLINK(\"http://x\")"
	files, skipped, err := appleCSVExporter{}.Render([]account.Account{a})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("the account should still be written, got %d files", len(files))
	}
	if strings.Contains(string(files[0].Data), "HYPERLINK") {
		t.Fatalf("export still carries the formula:\n%s", files[0].Data)
	}
	if len(skipped) != 1 || skipped[0].Label != "the note of GitHub (me)" || !strings.Contains(skipped[0].Reason, "formula") {
		t.Fatalf("want the note reported as skipped, got %+v", skipped)
	}
	got, _, recognized, err := importers.Parse(files[0].Data)
	if !recognized || err != nil || len(got) != 1 {
		t.Fatalf("re-import: recognized=%v err=%v n=%d", recognized, err, len(got))
	}
	if got[0].Note != "" {
		t.Fatalf("note = %q, want empty", got[0].Note)
	}
}
