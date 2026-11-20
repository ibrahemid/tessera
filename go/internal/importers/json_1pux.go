package importers

import (
	"encoding/json"
	"fmt"

	"github.com/ibrahemid/tessera/go/internal/account"
)

type onePUXFile struct {
	Accounts []struct {
		Vaults []struct {
			Attrs json.RawMessage `json:"attrs"`
			Items []onePUXItem    `json:"items"`
		} `json:"vaults"`
	} `json:"accounts"`
}

type onePUXItem struct {
	Details struct {
		NotesPlain  string `json:"notesPlain"`
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
		Title string          `json:"title"`
		Tags  json.RawMessage `json:"tags"`
	} `json:"overview"`
	FavIndex json.RawMessage `json:"favIndex"`
}

type onePUXVaultAttrs struct {
	Name json.RawMessage `json:"name"`
}

// parse1PUXData reads the export.data document at the heart of a 1Password
// 1PUX archive.
func parse1PUXData(data []byte) ([]account.Account, error) {
	var f onePUXFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("1password: %w", err)
	}
	var out []account.Account
	for _, acct := range f.Accounts {
		for _, vault := range acct.Vaults {
			var attrs onePUXVaultAttrs
			rawObject(vault.Attrs, &attrs)
			vaultName, _ := rawString(attrs.Name)
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
							return nil, fmt.Errorf("1password item %s: %w", quoteBounded(item.Overview.Title), err)
						}
						if !ok {
							continue
						}
						a.Note = account.CleanNote(item.Details.NotesPlain)
						a.Folder = account.CleanText(vaultName)
						a.Tags = cleanTags(rawStrings(item.Overview.Tags))
						a.Pinned = rawIsPositive(item.FavIndex)
						out = append(out, a)
					}
				}
			}
		}
	}
	return out, nil
}
