package importers

import (
	"encoding/json"
	"fmt"

	"github.com/ibrahemid/tessera/go/internal/account"
)

type andOTPEntry struct {
	Secret    string `json:"secret"`
	Issuer    string `json:"issuer"`
	Label     string `json:"label"`
	Digits    int    `json:"digits"`
	Type      string `json:"type"`
	Algorithm string `json:"algorithm"`
	Period    int    `json:"period"`
	Counter   int64  `json:"counter"`
}

func parseAndOTP(data []byte) ([]account.Account, error) {
	var entries []andOTPEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("andotp: %w", err)
	}
	out := make([]account.Account, 0, len(entries))
	for _, e := range entries {
		a, err := buildAccount(e.Type, e.Issuer, e.Label, e.Secret,
			e.Algorithm, e.Digits, e.Period, e.Counter)
		if err != nil {
			return nil, fmt.Errorf("andotp entry %q: %w", e.Issuer, err)
		}
		out = append(out, a)
	}
	return out, nil
}
