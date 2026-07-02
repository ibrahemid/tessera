// Package importers parses plaintext (unencrypted) account exports from other
// authenticator apps — Aegis, 2FAS, and Raivo — into the canonical account
// model. Encrypted exports are detected and rejected with a clear error rather
// than parsed wrong; decrypt them in the source app and re-export, or export
// unencrypted. Secrets are decoded from base32 to raw bytes here.
package importers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/base32x"
)

// Parse detects a supported app export and returns its accounts. ok is false
// when data is not a recognized app export (the caller may fall back to parsing
// otpauth lines). A recognized-but-encrypted export returns ok=true with an
// error so the caller surfaces the real reason.
func Parse(data []byte) (accts []account.Account, source string, ok bool, err error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil, "", false, nil
	}
	switch trimmed[0] {
	case '[':
		// A top-level JSON array is a Raivo export; malformed entries surface
		// their real error instead of falling through to the otpauth parser.
		if !json.Valid(trimmed) {
			return nil, "", false, nil
		}
		a, e := parseRaivo(trimmed)
		return a, "Raivo", true, e
	case '{':
		// Probe the keys to pick the format.
		var probe map[string]json.RawMessage
		if json.Unmarshal(trimmed, &probe) != nil {
			return nil, "", false, nil
		}
		if _, has := probe["db"]; has {
			a, e := parseAegis(trimmed)
			return a, "Aegis", true, e
		}
		// Encrypted 2FAS backups carry BOTH an empty "services" array and the
		// ciphertext in "servicesEncrypted" — check the ciphertext first.
		var enc string
		if raw, has := probe["servicesEncrypted"]; has && json.Unmarshal(raw, &enc) == nil && enc != "" {
			return nil, "2FAS", true, fmt.Errorf("2FAS export is encrypted; in 2FAS turn off the backup password (or decrypt) and export again")
		}
		if _, has := probe["services"]; has {
			a, e := parse2FAS(trimmed)
			return a, "2FAS", true, e
		}
		return nil, "", false, nil
	default:
		return nil, "", false, nil
	}
}

// ---- Aegis ----

type aegisFile struct {
	Version int             `json:"version"`
	DB      json.RawMessage `json:"db"`
}

type aegisDB struct {
	Entries []aegisEntry `json:"entries"`
}

type aegisEntry struct {
	Type   string `json:"type"`
	Name   string `json:"name"`
	Issuer string `json:"issuer"`
	Info   struct {
		Secret  string `json:"secret"`
		Algo    string `json:"algo"`
		Digits  int    `json:"digits"`
		Period  int    `json:"period"`
		Counter int64  `json:"counter"`
	} `json:"info"`
}

func parseAegis(data []byte) ([]account.Account, error) {
	var f aegisFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("aegis: %w", err)
	}
	// In an encrypted export "db" is a base64 string, not an object.
	if db := bytes.TrimSpace(f.DB); len(db) > 0 && db[0] == '"' {
		return nil, fmt.Errorf("Aegis export is encrypted; export with encryption off (Aegis: Settings > Import/Export > Export, untick encryption) and try again")
	}
	var db aegisDB
	if err := json.Unmarshal(f.DB, &db); err != nil {
		return nil, fmt.Errorf("aegis db: %w", err)
	}
	out := make([]account.Account, 0, len(db.Entries))
	for _, e := range db.Entries {
		a, err := buildAccount(e.Type, e.Issuer, e.Name, e.Info.Secret,
			e.Info.Algo, e.Info.Digits, e.Info.Period, e.Info.Counter)
		if err != nil {
			return nil, fmt.Errorf("aegis entry %q: %w", e.Name, err)
		}
		out = append(out, a)
	}
	return out, nil
}

// ---- 2FAS ----

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

// ---- Raivo ----

type raivoEntry struct {
	Issuer    string `json:"issuer"`
	Account   string `json:"account"`
	Secret    string `json:"secret"`
	Algorithm string `json:"algorithm"`
	Digits    string `json:"digits"`
	Kind      string `json:"kind"`
	Timer     string `json:"timer"`
	Counter   string `json:"counter"`
}

func parseRaivo(data []byte) ([]account.Account, error) {
	var entries []raivoEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("raivo: no entries")
	}
	out := make([]account.Account, 0, len(entries))
	for _, e := range entries {
		// Raivo stores numbers as strings.
		if e.Secret == "" {
			return nil, fmt.Errorf("raivo entry %q: missing secret", e.Issuer)
		}
		digits, err := atoiDefault(e.Digits, 6)
		if err != nil {
			return nil, fmt.Errorf("raivo entry %q: digits: %w", e.Issuer, err)
		}
		period, err := atoiDefault(e.Timer, 30)
		if err != nil {
			return nil, fmt.Errorf("raivo entry %q: timer: %w", e.Issuer, err)
		}
		counter, err := atoiDefault(e.Counter, 0)
		if err != nil {
			return nil, fmt.Errorf("raivo entry %q: counter: %w", e.Issuer, err)
		}
		a, err := buildAccount(e.Kind, e.Issuer, e.Account, e.Secret,
			e.Algorithm, digits, period, int64(counter))
		if err != nil {
			return nil, fmt.Errorf("raivo entry %q: %w", e.Issuer, err)
		}
		out = append(out, a)
	}
	return out, nil
}

// ---- shared mapping ----

func buildAccount(typ, issuer, acct, secretB32, algo string, digits, period int, counter int64) (account.Account, error) {
	secret, err := base32x.Decode(secretB32)
	if err != nil {
		return account.Account{}, fmt.Errorf("decode secret: %w", err)
	}
	t, err := mapType(typ)
	if err != nil {
		return account.Account{}, err
	}
	algorithm, err := mapAlgo(algo)
	if err != nil {
		return account.Account{}, err
	}
	if digits == 0 {
		digits = 6
	}
	if t == account.Steam {
		digits = 5
	}
	if t != account.HOTP && period == 0 {
		period = 30
	}
	return account.Account{
		Type:      t,
		Issuer:    strings.TrimSpace(issuer),
		Account:   strings.TrimSpace(acct),
		Secret:    secret,
		Algorithm: algorithm,
		Digits:    digits,
		Period:    period,
		Counter:   counter,
	}, nil
}

// mapType rejects OTP schemes Tessera can't generate (Yandex, mOTP, ...) so an
// import never silently produces wrong codes.
func mapType(s string) (account.Type, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "totp":
		return account.TOTP, nil
	case "hotp":
		return account.HOTP, nil
	case "steam", "steam_totp", "steamtotp":
		return account.Steam, nil
	default:
		return "", fmt.Errorf("unsupported account type %q", s)
	}
}

func mapAlgo(s string) (string, error) {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "", "SHA1":
		return "SHA1", nil
	case "SHA256":
		return "SHA256", nil
	case "SHA512":
		return "SHA512", nil
	default:
		return "", fmt.Errorf("unsupported algorithm %q", s)
	}
}

func atoiDefault(s string, def int) (int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return def, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("not a number: %q", s)
	}
	return n, nil
}
