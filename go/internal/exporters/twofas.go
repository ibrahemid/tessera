package exporters

import (
	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/base32x"
	"github.com/ibrahemid/tessera/go/internal/otpauth"
)

// twofasExporter writes a 2FAS backup (schema version 4) with no backup
// password. Every optional key is written with an explicit null rather than
// omitted: 2FAS decodes with kotlinx.serialization, whose tolerance for absent
// keys depends on a Json{} configuration that is not published, and an explicit
// null is accepted under either setting.
type twofasExporter struct{}

func (twofasExporter) Name() string { return "2fas" }

func (twofasExporter) Description() string { return "2FAS Auth backup JSON (no backup password)" }

type twofasExport struct {
	SchemaVersion int             `json:"schemaVersion"`
	Services      []twofasService `json:"services"`
}

type twofasService struct {
	Name      string      `json:"name"`
	Secret    string      `json:"secret"`
	UpdatedAt int64       `json:"updatedAt"`
	OTP       twofasOTP   `json:"otp"`
	Order     twofasOrder `json:"order"`
}

type twofasOTP struct {
	Link      string  `json:"link"`
	Label     *string `json:"label"`
	Account   *string `json:"account"`
	Issuer    *string `json:"issuer"`
	Digits    int     `json:"digits"`
	Period    *int    `json:"period"`
	Algorithm string  `json:"algorithm"`
	TokenType string  `json:"tokenType"`
	Counter   *int64  `json:"counter"`
	Source    string  `json:"source"`
}

type twofasOrder struct {
	Position int `json:"position"`
}

func (twofasExporter) Render(accts []account.Account) ([]File, []Skipped, error) {
	out := twofasExport{SchemaVersion: 4, Services: make([]twofasService, 0, len(accts))}
	for i, a := range accts {
		s := twofasService{
			Name:      displayName(a),
			Secret:    base32x.EncodeNoPad(a.Secret),
			UpdatedAt: a.UpdatedAt * 1000, // 2FAS timestamps are milliseconds
			OTP: twofasOTP{
				Link:      otpauth.Format(a),
				Label:     strPtr(otpauthLabel(a)),
				Account:   strPtr(a.Account),
				Issuer:    strPtr(a.Issuer),
				Digits:    a.Digits,
				Algorithm: a.Algorithm,
				TokenType: tokenType(a.Type),
				Source:    "Link",
			},
			Order: twofasOrder{Position: i},
		}
		if a.Type == account.HOTP {
			s.OTP.Counter = int64Ptr(a.Counter)
		} else {
			s.OTP.Period = intPtr(a.Period)
		}
		out.Services = append(out.Services, s)
	}
	data, err := marshalJSON(out)
	if err != nil {
		return nil, nil, err
	}
	return []File{{Name: "2fas-export.json", Data: data}}, nil, nil
}
