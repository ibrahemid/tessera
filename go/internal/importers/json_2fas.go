package importers

import (
	"encoding/json"
	"fmt"

	"github.com/ibrahemid/tessera/go/internal/account"
)

type twofasFile struct {
	Services []twofasService `json:"services"`
}

type twofasService struct {
	Name   string `json:"name"`
	Secret string `json:"secret"`
	OTP    struct {
		Account   string `json:"account"`
		Issuer    string `json:"issuer"`
		Digits    int    `json:"digits"`
		Period    int    `json:"period"`
		Algorithm string `json:"algorithm"`
		TokenType string `json:"tokenType"`
		Counter   int64  `json:"counter"`
	} `json:"otp"`
}

func parse2FAS(data []byte) ([]account.Account, error) {
	var f twofasFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("2fas: %w", err)
	}
	out := make([]account.Account, 0, len(f.Services))
	for _, s := range f.Services {
		issuer := s.OTP.Issuer
		if issuer == "" {
			issuer = s.Name
		}
		a, err := buildAccount(s.OTP.TokenType, issuer, s.OTP.Account, s.Secret,
			s.OTP.Algorithm, s.OTP.Digits, s.OTP.Period, s.OTP.Counter)
		if err != nil {
			return nil, fmt.Errorf("2fas service %q: %w", s.Name, err)
		}
		out = append(out, a)
	}
	return out, nil
}
