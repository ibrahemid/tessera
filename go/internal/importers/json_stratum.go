package importers

import (
	"encoding/json"
	"fmt"

	"github.com/ibrahemid/tessera/go/internal/account"
)

type stratumFile struct {
	Authenticators          []stratumEntry  `json:"Authenticators"`
	Categories              json.RawMessage `json:"Categories"`
	AuthenticatorCategories json.RawMessage `json:"AuthenticatorCategories"`
}

type stratumMembership struct {
	CategoryID          json.RawMessage `json:"CategoryId"`
	AuthenticatorSecret json.RawMessage `json:"AuthenticatorSecret"`
}

type stratumEntry struct {
	Type      int    `json:"Type"`
	Issuer    string `json:"Issuer"`
	Username  string `json:"Username"`
	Secret    string `json:"Secret"`
	Algorithm int    `json:"Algorithm"`
	Digits    int    `json:"Digits"`
	Period    int    `json:"Period"`
	Counter   int64  `json:"Counter"`
}

func parseStratum(data []byte) ([]account.Account, error) {
	var f stratumFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("stratum: %w", err)
	}
	categories := indexGroups[idGroup](f.Categories)
	memberships := map[string][]json.RawMessage{}
	for _, raw := range rawElements(f.AuthenticatorCategories) {
		var m stratumMembership
		if !rawObject(raw, &m) {
			continue
		}
		if secret, ok := rawString(m.AuthenticatorSecret); ok {
			memberships[secret] = append(memberships[secret], m.CategoryID)
		}
	}
	out := make([]account.Account, 0, len(f.Authenticators))
	for _, e := range f.Authenticators {
		typ, err := stratumType(e.Type)
		if err != nil {
			return nil, fmt.Errorf("stratum entry %s: %w", quoteBounded(e.Issuer), err)
		}
		algo, err := stratumAlgorithm(e.Algorithm)
		if err != nil {
			return nil, fmt.Errorf("stratum entry %s: %w", quoteBounded(e.Issuer), err)
		}
		a, err := buildAccount(typ, e.Issuer, e.Username, e.Secret,
			algo, e.Digits, e.Period, e.Counter)
		if err != nil {
			return nil, fmt.Errorf("stratum entry %s: %w", quoteBounded(e.Issuer), err)
		}
		var names []string
		for _, id := range memberships[e.Secret] {
			names = groupName(names, categories, id)
		}
		a.Folder, a.Tags = placeGroups(names)
		out = append(out, a)
	}
	return out, nil
}

func stratumType(t int) (string, error) {
	switch t {
	case 1:
		return "hotp", nil
	case 2:
		return "totp", nil
	case 3:
		return "", fmt.Errorf("unsupported account type mOTP")
	case 4:
		return "steam", nil
	case 5:
		return "", fmt.Errorf("unsupported account type Yandex OTP")
	default:
		return "", fmt.Errorf("unsupported account type %d", t)
	}
}

func stratumAlgorithm(a int) (string, error) {
	switch a {
	case 0:
		return "SHA1", nil
	case 1:
		return "SHA256", nil
	case 2:
		return "SHA512", nil
	default:
		return "", fmt.Errorf("unsupported algorithm %d", a)
	}
}
