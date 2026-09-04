package importers

import (
	"encoding/json"
	"fmt"

	"github.com/ibrahemid/tessera/go/internal/account"
)

type onePUXFile struct {
	Accounts []struct {
		Vaults []struct {
			Items []onePUXItem `json:"items"`
		} `json:"vaults"`
	} `json:"accounts"`
}

type onePUXItem struct {
	Details struct {
		LoginFields []struct {
			Value       string `json:"value"`
			Designation string `json:"designation"`
		} `json:"loginFields"`
		Sections []struct {
			Fields []struct {
				Value json.RawMessage `json:"value"`
			} `json:"fields"`
		} `json:"sections"`
	} `json:"details"`
	Overview struct {
		Title string `json:"title"`
	} `json:"overview"`
}

// parse1PUXData reads the export.data document at the heart of a 1Password
// 1PUX archive. Both cores parse this document; only the CLI opens the zip.
func parse1PUXData(data []byte) ([]account.Account, error) {
	var f onePUXFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("1password: %w", err)
	}
	var out []account.Account
	for _, acct := range f.Accounts {
		for _, vault := range acct.Vaults {
			for _, item := range vault.Items {
				username := ""
				for _, lf := range item.Details.LoginFields {
					if lf.Designation == "username" {
						username = lf.Value
						break
					}
				}
				for _, section := range item.Details.Sections {
					for _, field := range section.Fields {
						var v struct {
							TOTP string `json:"totp"`
						}
						if len(field.Value) == 0 || json.Unmarshal(field.Value, &v) != nil {
							continue
						}
						a, ok, err := parseOTPValue(v.TOTP, item.Overview.Title, username)
						if err != nil {
							return nil, fmt.Errorf("1password item %q: %w", item.Overview.Title, err)
						}
						if !ok {
							continue
						}
						out = append(out, a)
					}
				}
			}
		}
	}
	return out, nil
}
