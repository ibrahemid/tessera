package exporters

import (
	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/base32x"
)

// aegisExporter writes an unencrypted Aegis vault export (version 1, db
// version 3). "header" carries null slots and params, which is how Aegis marks
// an export written without encryption.
type aegisExporter struct{}

func (aegisExporter) Name() string { return "aegis" }

func (aegisExporter) MultiFile() bool { return false }

func (aegisExporter) Description() string { return "Aegis Authenticator vault JSON (unencrypted)" }

type aegisExport struct {
	Version int         `json:"version"`
	Header  aegisHeader `json:"header"`
	DB      aegisDB     `json:"db"`
}

type aegisHeader struct {
	Slots  *struct{} `json:"slots"`
	Params *struct{} `json:"params"`
}

type aegisDB struct {
	Version int          `json:"version"`
	Entries []aegisEntry `json:"entries"`
}

type aegisEntry struct {
	Type     string    `json:"type"`
	UUID     string    `json:"uuid"`
	Name     string    `json:"name"`
	Issuer   string    `json:"issuer"`
	Note     string    `json:"note"`
	Favorite bool      `json:"favorite"`
	Icon     *string   `json:"icon"`
	Info     aegisInfo `json:"info"`
	Groups   []string  `json:"groups"`
}

type aegisInfo struct {
	Secret string `json:"secret"`
	Algo   string `json:"algo"`
	Digits int    `json:"digits"`
	// Aegis writes period for time-based entries and counter for hotp; a key
	// that does not apply to the type is absent, not null.
	Period  *int   `json:"period,omitempty"`
	Counter *int64 `json:"counter,omitempty"`
}

func (aegisExporter) Render(accts []account.Account) ([]File, []Skipped, error) {
	out := aegisExport{Version: 1, DB: aegisDB{Version: 3, Entries: make([]aegisEntry, 0, len(accts))}}
	for _, a := range accts {
		e := aegisEntry{
			Type:     string(a.Type),
			UUID:     exportID(a.ID),
			Name:     a.Account,
			Issuer:   a.Issuer,
			Favorite: a.Pinned,
			Groups:   []string{},
			Info: aegisInfo{
				Secret: base32x.EncodeNoPad(a.Secret),
				Algo:   a.Algorithm,
				Digits: a.Digits,
			},
		}
		if a.Type == account.HOTP {
			e.Info.Counter = int64Ptr(a.Counter)
		} else {
			e.Info.Period = intPtr(a.Period)
		}
		out.DB.Entries = append(out.DB.Entries, e)
	}
	data, err := marshalJSON(out)
	if err != nil {
		return nil, nil, err
	}
	return []File{{Name: "aegis-export.json", Data: data}}, nil, nil
}
