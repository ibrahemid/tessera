package importers

import (
	"bytes"
	"encoding/csv"
	"fmt"

	"github.com/ibrahemid/tessera/go/internal/account"
)

// csvFormat describes one registered CSV export: its source name and the
// column indexes the shared OTP-value rule needs.
type csvFormat struct {
	name     string
	titleCol int
	userCol  int
	otpCol   int
}

// csvHeaders maps an exact header line to its format. Apple publishes no schema
// for its CSV, so the header comes from real exports: if Apple changes it,
// detection stops matching and the file is reported as unrecognized rather than
// parsed against the wrong columns.
var csvHeaders = map[string]csvFormat{
	"Title,URL,Username,Password,Notes,OTPAuth":                        {name: "Apple Passwords", titleCol: 0, userCol: 2, otpCol: 5},
	"Title,Url,Username,Password,OTPAuth,Favorite,Archived,Tags,Notes": {name: "1Password", titleCol: 0, userCol: 2, otpCol: 4},
}

var utf8BOM = []byte("\xef\xbb\xbf")

// matchCSVHeader compares the first line of data, byte for byte, against the
// registered export headers.
func matchCSVHeader(data []byte) (csvFormat, bool) {
	line := bytes.TrimPrefix(data, utf8BOM)
	if i := bytes.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	line = bytes.TrimSuffix(line, []byte("\r"))
	f, ok := csvHeaders[string(line)]
	return f, ok
}

// MatchCSVHeader reports whether data starts with a registered CSV export
// header. detect calls this so the header table lives in one place.
func MatchCSVHeader(data []byte) bool {
	_, ok := matchCSVHeader(data)
	return ok
}

func parseCSV(data []byte, f csvFormat) ([]account.Account, error) {
	r := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(data, utf8BOM)))
	r.FieldsPerRecord = -1
	rows, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("%s csv: %w", f.name, err)
	}
	out := make([]account.Account, 0, len(rows))
	for i, row := range rows {
		if i == 0 {
			continue
		}
		if f.otpCol >= len(row) {
			continue
		}
		title, user := "", ""
		if f.titleCol < len(row) {
			title = row[f.titleCol]
		}
		if f.userCol < len(row) {
			user = row[f.userCol]
		}
		a, ok, err := parseOTPValue(row[f.otpCol], title, user)
		if err != nil {
			return nil, fmt.Errorf("%s row %d: %w", f.name, i+1, err)
		}
		if !ok {
			continue
		}
		out = append(out, a)
	}
	return out, nil
}
