package importers

import (
	"encoding/json"
	"fmt"

	"github.com/ibrahemid/tessera/go/internal/account"
)

type bitwardenFile struct {
	Items []bitwardenItem `json:"items"`
}

type bitwardenItem struct {
	Name  string `json:"name"`
	Login *struct {
		Username string `json:"username"`
		TOTP     string `json:"totp"`
	} `json:"login"`
}

// parseBitwardenItems walks the item list shared by the Bitwarden password
// manager and Bitwarden Authenticator exports. Items with no login object and
// items whose totp field is empty carry no second factor and are skipped.
func parseBitwardenItems(data []byte) ([]account.Account, error) {
	var f bitwardenFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("bitwarden: %w", err)
	}
	out := make([]account.Account, 0, len(f.Items))
	for _, it := range f.Items {
		if it.Login == nil {
			continue
		}
		a, ok, err := parseOTPValue(it.Login.TOTP, it.Name, it.Login.Username)
		if err != nil {
			return nil, fmt.Errorf("bitwarden item %q: %w", it.Name, err)
		}
		if !ok {
			continue
		}
		out = append(out, a)
	}
	return out, nil
}
