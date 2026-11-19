//go:build !windows

package ui

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func TestEnableConsoleLeavesColorsAlone(t *testing.T) {
	prev := lipgloss.ColorProfile()
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
	lipgloss.SetColorProfile(termenv.ANSI256)

	restore, err := EnableConsole()
	if err != nil {
		t.Fatalf("EnableConsole() error = %v, want nil", err)
	}
	if restore == nil {
		t.Fatal("EnableConsole() restore = nil")
	}
	restore()
	if got := lipgloss.ColorProfile(); got != termenv.ANSI256 {
		t.Errorf("color profile = %v, want %v", got, termenv.ANSI256)
	}
}
