package exporters

import (
	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/otpauth"
)

// bitwardenExporter writes a Bitwarden Authenticator JSON export: the two-key
// {encrypted, items} shape, with the whole credential in login.totp as a URI.
// Bitwarden Authenticator also accepts a bare secret there, but every real
// export carries a URI, which is the only form that keeps the algorithm,
// digits and period.
type bitwardenExporter struct{}

func (bitwardenExporter) Name() string { return "bitwarden" }

func (bitwardenExporter) Description() string { return "Bitwarden Authenticator JSON export" }

type bitwardenExport struct {
	Encrypted bool            `json:"encrypted"`
	Items     []bitwardenItem `json:"items"`
}

type bitwardenItem struct {
	ID             string         `json:"id"`
	Name           string         `json:"name"`
	FolderID       *string        `json:"folderId"`
	OrganizationID *string        `json:"organizationId"`
	CollectionIDs  []string       `json:"collectionIds"`
	Notes          *string        `json:"notes"`
	Type           int            `json:"type"`
	Login          bitwardenLogin `json:"login"`
	Favorite       bool           `json:"favorite"`
}

type bitwardenLogin struct {
	TOTP string `json:"totp"`
}

func (bitwardenExporter) Render(accts []account.Account) ([]File, []Skipped, error) {
	out := bitwardenExport{Items: make([]bitwardenItem, 0, len(accts))}
	for _, a := range accts {
		out.Items = append(out.Items, bitwardenItem{
			ID:       exportID(a.ID),
			Name:     displayName(a),
			Type:     1, // Bitwarden cipher type 1 = login
			Login:    bitwardenLogin{TOTP: otpauth.Format(a)},
			Favorite: a.Pinned,
		})
	}
	data, err := marshalJSON(out)
	if err != nil {
		return nil, nil, err
	}
	return []File{{Name: "bitwarden-authenticator-export.json", Data: data}}, nil, nil
}
