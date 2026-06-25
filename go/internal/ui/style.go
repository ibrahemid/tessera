// Package ui holds shared terminal styling for the tess CLI and TUI: the same
// antique-gold-on-ink identity as the macOS app, degrading gracefully on
// non-color terminals (lipgloss honors NO_COLOR and non-TTY output).
package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	Accent   = lipgloss.AdaptiveColor{Light: "#A9791F", Dark: "#E3B23C"}
	Warning  = lipgloss.AdaptiveColor{Light: "#C2410C", Dark: "#F97316"}
	Primary  = lipgloss.AdaptiveColor{Light: "#1A1C22", Dark: "#F2F2F5"}
	Faint    = lipgloss.AdaptiveColor{Light: "#8A8E98", Dark: "#6A6E7A"}
	TrackCol = lipgloss.AdaptiveColor{Light: "#D9D9DE", Dark: "#3A3D48"}

	TitleStyle   = lipgloss.NewStyle().Bold(true).Foreground(Accent)
	CodeStyle    = lipgloss.NewStyle().Bold(true).Foreground(Primary)
	CodeCopied   = lipgloss.NewStyle().Bold(true).Foreground(Accent)
	IssuerStyle  = lipgloss.NewStyle().Bold(true).Foreground(Primary)
	SubtleStyle  = lipgloss.NewStyle().Foreground(Faint)
	AccentStyle  = lipgloss.NewStyle().Foreground(Accent)
	WarnStyle    = lipgloss.NewStyle().Foreground(Warning)
	SelectedRow  = lipgloss.NewStyle().Bold(true)
	SectionLabel = lipgloss.NewStyle().Foreground(Faint).Bold(true)
)

// tileHues mirror the macOS app's per-issuer palette.
var tileHues = []string{
	"#3B6FB0", "#2A8C7E", "#8E5BA6", "#B5562E",
	"#3E7D4F", "#4F5BB0", "#B0506E", "#B08A2E",
}

// Monogram renders a colored single-letter tile for an issuer/account key.
func Monogram(key, label string) string {
	var hash uint32 = 2166136261
	for _, b := range []byte(key) {
		hash = (hash ^ uint32(b)) * 16777619
	}
	hue := tileHues[hash%uint32(len(tileHues))]
	letter := "?"
	if label != "" {
		letter = strings.ToUpper(string([]rune(label)[0]))
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(hue)).Bold(true).Render(letter)
}

// GroupCode formats a 6-digit code as "123 456"; others pass through.
func GroupCode(code string) string {
	if len(code) == 6 {
		return code[:3] + " " + code[3:]
	}
	return code
}

// Bar renders a width-cell countdown bar; fills with accent, warns under 5s.
func Bar(remaining, period, width int) string {
	if period <= 0 || width <= 0 {
		return strings.Repeat(" ", max(width, 0))
	}
	frac := float64(remaining) / float64(period)
	filled := int(frac*float64(width) + 0.5)
	filled = min(max(filled, 0), width)
	col := Accent
	if remaining <= 5 {
		col = Warning
	}
	on := lipgloss.NewStyle().Foreground(col).Render(strings.Repeat("█", filled))
	off := lipgloss.NewStyle().Foreground(TrackCol).Render(strings.Repeat("░", width-filled))
	return on + off
}

// Plain renders without styling (for piping/JSON contexts when needed).
func Plain(s string) string { return fmt.Sprintf("%s", s) }
