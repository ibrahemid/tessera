//go:build !windows

package ui

import (
	"testing"

	"github.com/charmbracelet/colorprofile"
)

func TestEnableConsoleLeavesColorsAlone(t *testing.T) {
	pinColorProfile(t, colorprofile.ANSI256)

	restore, err := EnableConsole()
	if err != nil {
		t.Fatalf("EnableConsole() error = %v, want nil", err)
	}
	if restore == nil {
		t.Fatal("EnableConsole() restore = nil")
	}
	restore()
	if got := outputProfile(); got != colorprofile.ANSI256 {
		t.Errorf("color profile = %v, want %v", got, colorprofile.ANSI256)
	}
}
