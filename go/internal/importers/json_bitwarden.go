package importers

import (
	"encoding/json"
	"fmt"

	"github.com/ibrahemid/tessera/go/internal/account"
)

type bitwardenFile struct {
	Items       []bitwardenItem `json:"items"`
	Folders     json.RawMessage `json:"folders"`
	Collections json.RawMessage `json:"collections"`
}

type bitwardenItem struct {
	Name  string `json:"name"`
	Notes string `json:"notes"`
	Login *struct {
		Username string `json:"username"`
		TOTP     string `json:"totp"`
	} `json:"login"`
	FolderID      json.RawMessage `json:"folderId"`
	CollectionIDs json.RawMessage `json:"collectionIds"`
	Favorite      json.RawMessage `json:"favorite"`
}

// parseBitwardenItems walks the item list shared by the Bitwarden password
// manager and Bitwarden Authenticator exports. Items with no login object and
// items whose totp field is empty carry no second factor and are skipped.
func parseBitwardenItems(data []byte) ([]account.Account, error) {
	var f bitwardenFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("bitwarden: %w", err)
	}
	folders := indexGroups[idGroup](f.Folders)
	collections := indexGroups[idGroup](f.Collections)
	out := make([]account.Account, 0, len(f.Items))
	for _, it := range f.Items {
		if it.Login == nil {
			continue
		}
		a, ok, err := parseOTPValue(it.Login.TOTP, it.Name, it.Login.Username)
		if err != nil {
			return nil, fmt.Errorf("bitwarden item %s: %w", quoteBounded(it.Name), err)
		}
		if !ok {
			continue
		}
		a.Note = account.CleanNote(it.Notes)
		names := groupName(nil, folders, it.FolderID)
		a.Folder, a.Tags = placeGroups(groupNames(names, collections, it.CollectionIDs))
		a.Pinned = rawIsTrue(it.Favorite)
		out = append(out, a)
	}
	return out, nil
}
