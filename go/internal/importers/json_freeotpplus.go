package importers

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/ibrahemid/tessera/go/internal/account"
)

type freeOTPPlusFile struct {
	Tokens     []freeOTPPlusToken `json:"tokens"`
	TokenOrder []string           `json:"tokenOrder"`
}

type freeOTPPlusToken struct {
	IssuerInt string `json:"issuerInt"`
	IssuerExt string `json:"issuerExt"`
	IssuerAlt string `json:"issuerAlt"`
	Label     string `json:"label"`
	LabelAlt  string `json:"labelAlt"`
	Type      string `json:"type"`
	Algo      string `json:"algo"`
	Secret    []int  `json:"secret"`
	Digits    int    `json:"digits"`
	Counter   int64  `json:"counter"`
	Period    int    `json:"period"`
}

func parseFreeOTPPlus(data []byte) ([]account.Account, error) {
	var f freeOTPPlusFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("freeotp+: %w", err)
	}
	out := make([]account.Account, 0, len(f.Tokens))
	for _, tk := range f.Tokens {
		issuer := tk.IssuerExt
		if issuer == "" {
			issuer = tk.IssuerInt
		}
		if tk.IssuerAlt != "" {
			issuer = tk.IssuerAlt
		}
		label := tk.Label
		if tk.LabelAlt != "" {
			label = tk.LabelAlt
		}
		typ := tk.Type
		// FreeOTP+ has no Steam token type: a Steam account is stored as a
		// TOTP token whose issuer is "Steam" and whose codes come out of a
		// different alphabet.
		if strings.EqualFold(issuer, "Steam") && strings.EqualFold(typ, "TOTP") {
			typ = "steam"
		}
		secret, err := javaSignedBytes(tk.Secret)
		if err != nil {
			return nil, fmt.Errorf("freeotp+ token %q: %w", issuer, err)
		}
		counter := tk.Counter
		if strings.EqualFold(tk.Type, "HOTP") {
			// FreeOTP+ persists counter-1 and adds one back when it rebuilds
			// the URI, so the stored value is one step behind the real counter.
			counter++
		}
		a, err := buildAccountRaw(typ, issuer, label, secret,
			tk.Algo, tk.Digits, tk.Period, counter)
		if err != nil {
			return nil, fmt.Errorf("freeotp+ token %q: %w", issuer, err)
		}
		out = append(out, a)
	}
	return out, nil
}

// javaSignedBytes converts Gson's byte[] rendering — a JSON array of Java
// signed bytes, -128..127 — into raw key bytes.
func javaSignedBytes(vals []int) ([]byte, error) {
	if len(vals) == 0 {
		return nil, errors.New("missing secret")
	}
	out := make([]byte, len(vals))
	for _, v := range vals {
		if v < -128 || v > 255 {
			return nil, errors.New("secret byte out of range")
		}
	}
	for i, v := range vals {
		out[i] = byte(v)
	}
	return out, nil
}
