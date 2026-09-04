package importers

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/ibrahemid/tessera/go/internal/account"
)

type aegisFile struct {
	Version int             `json:"version"`
	DB      json.RawMessage `json:"db"`
}

type aegisDB struct {
	Entries []aegisEntry `json:"entries"`
}

type aegisEntry struct {
	Type   string `json:"type"`
	Name   string `json:"name"`
	Issuer string `json:"issuer"`
	Info   struct {
		Secret  string `json:"secret"`
		Algo    string `json:"algo"`
		Digits  int    `json:"digits"`
		Period  int    `json:"period"`
		Counter int64  `json:"counter"`
	} `json:"info"`
}

func parseAegis(data []byte) ([]account.Account, error) {
	var f aegisFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("aegis: %w", err)
	}
	// In an encrypted export "db" is a base64 string, not an object.
	if db := bytes.TrimSpace(f.DB); len(db) > 0 && db[0] == '"' {
		return nil, fmt.Errorf("Aegis export is encrypted; export with encryption off (Aegis: Settings > Import/Export > Export, untick encryption) and try again")
	}
	var db aegisDB
	if err := json.Unmarshal(f.DB, &db); err != nil {
		return nil, fmt.Errorf("aegis db: %w", err)
	}
	out := make([]account.Account, 0, len(db.Entries))
	for _, e := range db.Entries {
		a, err := buildAccount(e.Type, e.Issuer, e.Name, e.Info.Secret,
			e.Info.Algo, e.Info.Digits, e.Info.Period, e.Info.Counter)
		if err != nil {
			return nil, fmt.Errorf("aegis entry %q: %w", e.Name, err)
		}
		out = append(out, a)
	}
	return out, nil
}
