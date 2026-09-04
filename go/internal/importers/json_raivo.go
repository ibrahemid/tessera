package importers

import (
	"encoding/json"
	"fmt"

	"github.com/ibrahemid/tessera/go/internal/account"
)

type raivoEntry struct {
	Issuer    string `json:"issuer"`
	Account   string `json:"account"`
	Secret    string `json:"secret"`
	Algorithm string `json:"algorithm"`
	Digits    string `json:"digits"`
	Kind      string `json:"kind"`
	Timer     string `json:"timer"`
	Counter   string `json:"counter"`
}

func parseRaivo(data []byte) ([]account.Account, error) {
	var entries []raivoEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, err
	}
	out := make([]account.Account, 0, len(entries))
	for _, e := range entries {
		// Raivo stores numbers as strings.
		if e.Secret == "" {
			return nil, fmt.Errorf("raivo entry %q: missing secret", e.Issuer)
		}
		digits, err := atoiDefault(e.Digits, 6)
		if err != nil {
			return nil, fmt.Errorf("raivo entry %q: digits: %w", e.Issuer, err)
		}
		period, err := atoiDefault(e.Timer, 30)
		if err != nil {
			return nil, fmt.Errorf("raivo entry %q: timer: %w", e.Issuer, err)
		}
		counter, err := atoiDefault(e.Counter, 0)
		if err != nil {
			return nil, fmt.Errorf("raivo entry %q: counter: %w", e.Issuer, err)
		}
		a, err := buildAccount(e.Kind, e.Issuer, e.Account, e.Secret,
			e.Algorithm, digits, period, int64(counter))
		if err != nil {
			return nil, fmt.Errorf("raivo entry %q: %w", e.Issuer, err)
		}
		out = append(out, a)
	}
	return out, nil
}
