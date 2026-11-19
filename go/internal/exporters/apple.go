package exporters

import (
	"bytes"
	"encoding/csv"
	"fmt"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/otpauth"
)

// appleCSVExporter writes the CSV Apple Passwords imports: one row per account,
// the credential in the OTPAuth column as a full URI. Apple publishes no schema
// for this file, so the header is written exactly as real exports spell it.
type appleCSVExporter struct{}

func (appleCSVExporter) Name() string { return "apple-csv" }

func (appleCSVExporter) MultiFile() bool { return false }

func (appleCSVExporter) Description() string { return "Apple Passwords CSV (Title,URL,...,OTPAuth)" }

// appleCSVHeader must stay byte-identical to the header the importer matches.
var appleCSVHeader = []string{"Title", "URL", "Username", "Password", "Notes", "OTPAuth"}

func (appleCSVExporter) Render(accts []account.Account) ([]File, []Skipped, error) {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	if err := w.Write(appleCSVHeader); err != nil {
		return nil, nil, fmt.Errorf("apple csv: %w", err)
	}
	var skipped []Skipped
	written := 0
	for _, a := range accts {
		if startsAsFormula(a.Issuer) || startsAsFormula(a.Account) {
			skipped = append(skipped, Skipped{Label: label(a), Reason: appleFormulaReason})
			continue
		}
		note := a.Note
		if startsAsFormula(note) {
			skipped = append(skipped, Skipped{Label: "the note of " + label(a), Reason: appleNoteFormulaReason})
			note = ""
		}
		row := []string{a.Issuer, "", a.Account, "", note, otpauth.Format(a)}
		if err := w.Write(row); err != nil {
			return nil, nil, fmt.Errorf("apple csv: %w", err)
		}
		written++
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, nil, fmt.Errorf("apple csv: %w", err)
	}
	if written == 0 {
		return nil, skipped, nil
	}
	return []File{{Name: "apple-passwords-export.csv", Data: buf.Bytes()}}, skipped, nil
}

const (
	appleFormulaReason     = "title or username starts with =, +, - or @, which a spreadsheet runs as a formula"
	appleNoteFormulaReason = "it starts with =, +, - or @, which a spreadsheet runs as a formula"
)

// startsAsFormula reports whether a CSV cell would be evaluated as a formula
// when the file is opened in a spreadsheet.
func startsAsFormula(s string) bool {
	if s == "" {
		return false
	}
	switch s[0] {
	case '=', '+', '-', '@':
		return true
	}
	return false
}
