// Package code bridges the account model and the OTP engines to compute the
// current code for an account. It lives above both so neither needs to import
// the other.
package code

import (
	"fmt"
	"time"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/otp"
)

// For returns the current code for an account at time t.
func For(a account.Account, t time.Time) (string, error) {
	switch a.Type {
	case account.TOTP:
		alg, err := otp.ParseAlgorithm(a.Algorithm)
		if err != nil {
			return "", err
		}
		return otp.TOTP(a.Secret, t, a.Period, a.Digits, alg)
	case account.Steam:
		return otp.Steam(a.Secret, t)
	case account.HOTP:
		alg, err := otp.ParseAlgorithm(a.Algorithm)
		if err != nil {
			return "", err
		}
		if a.Counter < 0 || a.Counter > account.MaxCounter {
			return "", fmt.Errorf("account %q: %w, got %d", a.ID, account.ErrCounterRange, a.Counter)
		}
		return otp.HOTP(a.Secret, uint64(a.Counter), a.Digits, alg)
	default:
		return "", &UnknownTypeError{ID: a.ID, Type: a.Type}
	}
}

// UnknownTypeError reports an account whose type this build generates no code
// for: a type from a newer Tessera, or none at all.
type UnknownTypeError struct {
	ID   string
	Type account.Type
}

func (e *UnknownTypeError) Error() string {
	return fmt.Sprintf("account %q: unknown type %q; update Tessera to generate its codes", e.ID, string(e.Type))
}

// Remaining returns the seconds left in the current time-step (0 for HOTP).
func Remaining(a account.Account, t time.Time) int {
	if a.Type == account.HOTP {
		return 0
	}
	return otp.RemainingSeconds(t, a.Period)
}
