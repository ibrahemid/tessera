package importers

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/base32x"
	"github.com/ibrahemid/tessera/go/internal/otpauth"
)

// buildAccount normalizes one imported entry whose secret is written in base32.
func buildAccount(typ, issuer, acct, secretB32, algo string, digits, period int, counter int64) (account.Account, error) {
	secret, err := base32x.Decode(secretB32)
	if err != nil {
		return account.Account{}, fmt.Errorf("decode secret: %w", err)
	}
	return buildAccountRaw(typ, issuer, acct, secret, algo, digits, period, counter)
}

// buildAccountRaw is buildAccount for sources that store the secret as raw key
// bytes instead of base32 (FreeOTP+ ships a Java byte array).
func buildAccountRaw(typ, issuer, acct string, secret []byte, algo string, digits, period int, counter int64) (account.Account, error) {
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

// parseOTPValue reads one opaque OTP field — the shape used by every password
// manager that stores a single string per item — under the shared rule in
// /spec/otpauth.md. ok is false when the field is empty, which means the item
// carries no second factor and is skipped silently rather than failing.
func parseOTPValue(v, issuerFallback, accountFallback string) (account.Account, bool, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return account.Account{}, false, nil
	}
	lower := strings.ToLower(v)
	switch {
	case strings.HasPrefix(lower, "otpauth://"):
		a, err := otpauth.Parse(v)
		if err != nil {
			return account.Account{}, false, err
		}
		return a, true, nil
	case strings.HasPrefix(lower, "steam://"):
		secret, err := base32x.Decode(strings.TrimSuffix(v[len("steam://"):], "/"))
		if err != nil {
			return account.Account{}, false, errors.New("unsupported Steam secret encoding")
		}
		acct := accountFallback
		if strings.TrimSpace(acct) == "" {
			acct = issuerFallback
		}
		return account.Account{
			Type:      account.Steam,
			Issuer:    "Steam",
			Account:   strings.TrimSpace(acct),
			Secret:    secret,
			Algorithm: "SHA1",
			Digits:    5,
			Period:    30,
		}, true, nil
	default:
		secret, err := base32x.Decode(v)
		if err != nil {
			return account.Account{}, false, fmt.Errorf("decode secret: %w", err)
		}
		return account.Account{
			Type:      account.TOTP,
			Issuer:    strings.TrimSpace(issuerFallback),
			Account:   strings.TrimSpace(accountFallback),
			Secret:    secret,
			Algorithm: "SHA1",
			Digits:    6,
			Period:    30,
		}, true, nil
	}
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
