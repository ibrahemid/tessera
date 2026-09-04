package importers

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ibrahemid/tessera/go/internal/account"
)

type protonFile struct {
	Entries []protonEntry `json:"entries"`
}

type protonEntry struct {
	Content struct {
		URI       string `json:"uri"`
		EntryType string `json:"entry_type"`
		Name      string `json:"name"`
	} `json:"content"`
}

func parseProton(data []byte) ([]account.Account, error) {
	var f protonFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("proton: %w", err)
	}
	out := make([]account.Account, 0, len(f.Entries))
	for _, e := range f.Entries {
		// Every parameter lives in the uri; entry_type only sanity-checks that
		// an entry marked Steam really carries a Steam secret.
		a, ok, err := parseOTPValue(e.Content.URI, "", e.Content.Name)
		if err != nil {
			return nil, fmt.Errorf("proton entry %q: %w", e.Content.Name, err)
		}
		if !ok {
			continue
		}
		if strings.EqualFold(e.Content.EntryType, "Steam") && a.Type != account.Steam {
			return nil, fmt.Errorf("proton entry %q: marked Steam but its uri is not a Steam secret", e.Content.Name)
		}
		out = append(out, a)
	}
	return out, nil
}
