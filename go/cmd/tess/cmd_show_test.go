package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ibrahemid/tessera/go/internal/account"
)

func runShow(t *testing.T, args ...string) string {
	t.Helper()
	c := newShowCmd()
	var buf bytes.Buffer
	c.SetOut(&buf)
	c.SetArgs(args)
	if err := c.Execute(); err != nil {
		t.Fatalf("show: %v", err)
	}
	return buf.String()
}

func TestShowHidesSecretByDefault(t *testing.T) {
	path := withVault(t)
	a := totp("a", "ACME", "x@acme.io", 1)
	sealVault(t, path, []account.Account{a})

	out := runShow(t, "acme")
	if !strings.Contains(out, "ACME") || !strings.Contains(out, "x@acme.io") {
		t.Fatalf("expected issuer/account in output:\n%s", out)
	}
	if !strings.Contains(out, "totp") || !strings.Contains(out, "30s") {
		t.Fatalf("expected type/period in output:\n%s", out)
	}
	if strings.Contains(out, "secret") || strings.Contains(out, "otpauth://") {
		t.Fatalf("secret leaked without --secret/--uri:\n%s", out)
	}
}

func TestShowSecretAndURIOnRequest(t *testing.T) {
	path := withVault(t)
	sealVault(t, path, []account.Account{totp("a", "ACME", "x", 1)})

	out := runShow(t, "acme", "--secret", "--uri")
	if !strings.Contains(out, "secret") {
		t.Fatalf("expected base32 secret line:\n%s", out)
	}
	if !strings.Contains(out, "otpauth://") {
		t.Fatalf("expected otpauth URI line:\n%s", out)
	}
}

func TestShowHOTPPrintsCounterNotPeriod(t *testing.T) {
	path := withVault(t)
	h := account.Account{
		ID: "h", Type: account.HOTP, Issuer: "HBank", Account: "u",
		Secret: []byte("12345678901234567890"), Algorithm: "SHA1", Digits: 6,
		Counter: 7, CreatedAt: 1, UpdatedAt: 1,
	}
	sealVault(t, path, []account.Account{h})

	out := runShow(t, "hbank")
	if !strings.Contains(out, "counter") || !strings.Contains(out, "7") {
		t.Fatalf("expected counter for HOTP:\n%s", out)
	}
	if strings.Contains(out, "period") {
		t.Fatalf("period should be omitted for HOTP:\n%s", out)
	}
}
