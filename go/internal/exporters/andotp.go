package exporters

import (
	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/base32x"
)

// andOTPExporter writes a plain (unencrypted) andOTP backup: a top-level array
// of entries. andOTP is also the format Aegis and several forks read.
type andOTPExporter struct{}

func (andOTPExporter) Name() string { return "andotp" }

func (andOTPExporter) MultiFile() bool { return false }

func (andOTPExporter) Description() string { return "andOTP plain backup JSON" }

type andOTPExportEntry struct {
	Secret        string   `json:"secret"`
	Issuer        string   `json:"issuer"`
	Label         string   `json:"label"`
	Digits        int      `json:"digits"`
	Type          string   `json:"type"`
	Algorithm     string   `json:"algorithm"`
	Thumbnail     string   `json:"thumbnail"`
	LastUsed      int64    `json:"last_used"`
	UsedFrequency int      `json:"used_frequency"`
	Period        *int     `json:"period,omitempty"`
	Counter       *int64   `json:"counter,omitempty"`
	Tags          []string `json:"tags"`
}

func (andOTPExporter) Render(accts []account.Account) ([]File, []Skipped, error) {
	out := make([]andOTPExportEntry, 0, len(accts))
	for _, a := range accts {
		e := andOTPExportEntry{
			Secret:    base32x.EncodeNoPad(a.Secret),
			Issuer:    a.Issuer,
			Label:     a.Account,
			Digits:    a.Digits,
			Type:      tokenType(a.Type),
			Algorithm: a.Algorithm,
			Thumbnail: "Default",
			Tags:      []string{},
		}
		if a.Type == account.HOTP {
			e.Counter = int64Ptr(a.Counter)
		} else {
			e.Period = intPtr(a.Period)
		}
		out = append(out, e)
	}
	data, err := marshalJSON(out)
	if err != nil {
		return nil, nil, err
	}
	return []File{{Name: "andotp-export.json", Data: data}}, nil, nil
}
