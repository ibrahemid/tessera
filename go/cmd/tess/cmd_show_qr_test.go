package main

import (
	"bytes"
	"errors"
	"testing"

	"github.com/ibrahemid/tessera/go/internal/otpauth"
	"github.com/ibrahemid/tessera/go/internal/qr"
)

func terminalQR(t *testing.T, text string) string {
	t.Helper()
	var buf bytes.Buffer
	if err := qr.WriteTerminal(&buf, text); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func TestShowQRDrawsTheSetupLink(t *testing.T) {
	sealOutputVault(t)
	stubTTY(t, true)
	stdout, stderr, err := runRootStreams(t, "show", "ze", "--qr")
	if err != nil {
		t.Fatal(err)
	}
	if want := terminalQR(t, otpauth.Format(handled("z", "Zeta", "ops", "ze"))); stdout != want {
		t.Fatal("stdout is not the terminal QR code of the account's otpauth link")
	}
	if stderr != "This QR code holds the secret for Zeta (ops).\n" {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestShowQRRefusesAPipeWithoutForce(t *testing.T) {
	sealOutputVault(t)
	stubTTY(t, false)
	stdout, _, err := runRootStreams(t, "show", "ze", "--qr")
	if !errors.Is(err, errShowQRPiped) || stdout != "" {
		t.Fatalf("err = %v stdout = %q", err, stdout)
	}
	stdout, _, err = runRootStreams(t, "show", "ze", "--qr", "--force")
	if err != nil || stdout != terminalQR(t, otpauth.Format(handled("z", "Zeta", "ops", "ze"))) {
		t.Fatalf("--force: err = %v", err)
	}
}

func TestShowQRStandsAlone(t *testing.T) {
	sealOutputVault(t)
	stubTTY(t, true)
	for _, extra := range []string{"--secret", "--uri", "--json"} {
		stdout, _, err := runRootStreams(t, "show", "ze", "--qr", extra)
		if !errors.Is(err, errShowQRAlone) || stdout != "" {
			t.Errorf("--qr %s: err = %v stdout = %q", extra, err, stdout)
		}
	}
}
