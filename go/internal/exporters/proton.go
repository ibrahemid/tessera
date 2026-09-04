package exporters

import (
	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/base32x"
	"github.com/ibrahemid/tessera/go/internal/otpauth"
)

// protonExporter writes a Proton Authenticator export without a password.
// Proton's entry type is "Totp" or "Steam" only, so HOTP accounts are skipped
// rather than written as an entry Proton cannot read.
type protonExporter struct{}

func (protonExporter) Name() string { return "proton" }

func (protonExporter) MultiFile() bool { return false }

func (protonExporter) Description() string {
	return "Proton Authenticator JSON export (no password; TOTP and Steam only)"
}

type protonExport struct {
	Version int           `json:"version"`
	Entries []protonEntry `json:"entries"`
}

type protonEntry struct {
	ID      string        `json:"id"`
	Content protonContent `json:"content"`
	Note    *string       `json:"note"`
}

type protonContent struct {
	URI       string  `json:"uri"`
	EntryType string  `json:"entry_type"`
	Name      *string `json:"name"`
}

func (protonExporter) Render(accts []account.Account) ([]File, []Skipped, error) {
	out := protonExport{Version: 1, Entries: make([]protonEntry, 0, len(accts))}
	var skipped []Skipped
	for _, a := range accts {
		if a.Type == account.HOTP {
			skipped = append(skipped, Skipped{label(a), "Proton Authenticator stores only TOTP and Steam entries"})
			continue
		}
		e := protonEntry{ID: exportID(a.ID)}
		if a.Type == account.Steam {
			// A Steam entry holds nothing but the shared secret and a display
			// name; the issuer is implicit, so a Steam account whose issuer is
			// not "Steam" comes back named "Steam" on re-import.
			e.Content = protonContent{
				URI:       "steam://" + base32x.EncodeNoPad(a.Secret),
				EntryType: "Steam",
				Name:      strPtr(a.Account),
			}
		} else {
			e.Content = protonContent{
				URI:       otpauth.Format(a),
				EntryType: "Totp",
				Name:      strPtr(displayName(a)),
			}
		}
		out.Entries = append(out.Entries, e)
	}
	data, err := marshalJSON(out)
	if err != nil {
		return nil, nil, err
	}
	return []File{{Name: "proton-authenticator-export.json", Data: data}}, skipped, nil
}
