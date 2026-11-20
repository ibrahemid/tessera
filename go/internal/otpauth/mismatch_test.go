package otpauth

import (
	"errors"
	"fmt"
	"testing"
)

func TestParseReportsAnIssuerMismatchAsATypedError(t *testing.T) {
	_, err := Parse("otpauth://totp/GitHub:me?secret=JBSWY3DPEHPK3PXP&issuer=GitHub%20Inc")
	var mismatch *IssuerMismatchError
	if !errors.As(fmt.Errorf("wrapped: %w", err), &mismatch) {
		t.Fatalf("err = %v, want *IssuerMismatchError", err)
	}
	if mismatch.Label != "GitHub" || mismatch.Param != "GitHub Inc" {
		t.Errorf("mismatch = %+v", mismatch)
	}
	if got, want := err.Error(), `otpauth: issuer mismatch (label "GitHub", param "GitHub Inc")`; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestParseAcceptsAMatchingIssuerParameter(t *testing.T) {
	a, err := Parse("otpauth://totp/GitHub:me?secret=JBSWY3DPEHPK3PXP&issuer=GitHub")
	if err != nil {
		t.Fatal(err)
	}
	if a.Issuer != "GitHub" {
		t.Errorf("issuer = %q", a.Issuer)
	}
}
