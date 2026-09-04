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
	for _, a := range accts {
		row := []string{a.Issuer, "", a.Account, "", "", otpauth.Format(a)}
		if err := w.Write(row); err != nil {
			return nil, nil, fmt.Errorf("apple csv: %w", err)
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, nil, fmt.Errorf("apple csv: %w", err)
	}
	return []File{{Name: "apple-passwords-export.csv", Data: buf.Bytes()}}, nil, nil
}
