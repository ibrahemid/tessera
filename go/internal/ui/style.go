// Package ui holds shared terminal styling for the tess CLI and TUI: the same
// antique-gold-on-ink identity as the macOS app, degrading to plain text when
// stdout is not a terminal or NO_COLOR is set.
package ui

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
)

// AdaptiveColor is a color with one value for light backgrounds and one for
// dark backgrounds.
type AdaptiveColor struct {
	Light string
	Dark  string
}

// Pick returns the value for a dark (dark = true) or light background.
func (c AdaptiveColor) Pick(dark bool) color.Color {
	if dark {
		return lipgloss.Color(c.Dark)
	}
	return lipgloss.Color(c.Light)
}

var (
	Accent     = AdaptiveColor{Light: "#A9791F", Dark: "#E3B23C"}
	Warning    = AdaptiveColor{Light: "#C2410C", Dark: "#F97316"}
	Primary    = AdaptiveColor{Light: "#1A1C22", Dark: "#F2F2F5"}
	Faint      = AdaptiveColor{Light: "#8A8E98", Dark: "#6A6E7A"}
	TrackCol   = AdaptiveColor{Light: "#D9D9DE", Dark: "#3A3D48"}
	SelectedBg = AdaptiveColor{Light: "#F0ECE0", Dark: "#242732"}
)

// Theme is the set of styles for one background. The TUI holds its own Theme,
// chosen from the background its terminal reports.
type Theme struct {
	Dark       bool
	Title      lipgloss.Style
	Code       lipgloss.Style
	CodeCopied lipgloss.Style
	Issuer     lipgloss.Style
	Subtle     lipgloss.Style
	Accent     lipgloss.Style
	Warn       lipgloss.Style
	Selected   lipgloss.Style
	Section    lipgloss.Style
	Primary    lipgloss.Style
}

// NewTheme builds the styles for a dark (dark = true) or light background.
func NewTheme(dark bool) Theme {
	fg := func(c AdaptiveColor) lipgloss.Style { return lipgloss.NewStyle().Foreground(c.Pick(dark)) }
	return Theme{
		Dark:       dark,
		Title:      fg(Accent).Bold(true),
		Code:       fg(Primary).Bold(true),
		CodeCopied: fg(Accent).Bold(true),
		Issuer:     fg(Primary).Bold(true),
		Subtle:     fg(Faint),
		Accent:     fg(Accent),
		Warn:       fg(Warning),
		Selected:   lipgloss.NewStyle().Background(SelectedBg.Pick(dark)),
		Section:    fg(Faint).Bold(true),
		Primary:    fg(Primary),
	}
}

var (
	darkTheme  = NewTheme(true)
	lightTheme = NewTheme(false)
)

func themeFor(dark bool) Theme {
	if dark {
		return darkTheme
	}
	return lightTheme
}

// Handle renders an account's handle as its colored row identifier, using the
// same stable per-account hue the app tiles use. handle is expected already
// padded to the display column width by the caller.
func (t Theme) Handle(key, handle string) string {
	return t.HandleStyle(key).Render(handle)
}

// HandleStyle is the style [Theme.Handle] draws with: the account's stable hue.
func (t Theme) HandleStyle(key string) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(hueFor(key))).Bold(true)
}

// Track is the style of the unfilled part of a countdown bar.
func (t Theme) Track() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(TrackCol.Pick(t.Dark))
}

// Bar renders a width-cell countdown bar; fills with accent, warns at 5s or less.
func (t Theme) Bar(remaining, period, width int) string {
	if period <= 0 || width <= 0 {
		return strings.Repeat(" ", max(width, 0))
	}
	filled := BarFill(remaining, period, width)
	return t.BarStyle(remaining).Render(strings.Repeat("█", filled)) + t.Track().Render(strings.Repeat("░", width-filled))
}

// BarStyle is the style of the filled part of a countdown bar: the accent, or
// the warning color at 5s or less.
func (t Theme) BarStyle(remaining int) lipgloss.Style {
	if remaining <= 5 {
		return t.Warn
	}
	return t.Accent
}

// BarFill is how many of width cells a countdown bar fills.
func BarFill(remaining, period, width int) int {
	if period <= 0 || width <= 0 {
		return 0
	}
	filled := int(float64(remaining)/float64(period)*float64(width) + 0.5)
	return min(max(filled, 0), width)
}

// Style renders text for stdout: colored for the terminal's background and
// color depth, plain when stdout is not a terminal or NO_COLOR is set.
type Style struct {
	pick func(Theme) lipgloss.Style
}

// Render applies the style to the strings, joined by spaces.
func (s Style) Render(strs ...string) string {
	return render(func(t Theme) string { return s.pick(t).Render(strs...) })
}

var (
	TitleStyle   = Style{func(t Theme) lipgloss.Style { return t.Title }}
	CodeStyle    = Style{func(t Theme) lipgloss.Style { return t.Code }}
	CodeCopied   = Style{func(t Theme) lipgloss.Style { return t.CodeCopied }}
	IssuerStyle  = Style{func(t Theme) lipgloss.Style { return t.Issuer }}
	SubtleStyle  = Style{func(t Theme) lipgloss.Style { return t.Subtle }}
	AccentStyle  = Style{func(t Theme) lipgloss.Style { return t.Accent }}
	WarnStyle    = Style{func(t Theme) lipgloss.Style { return t.Warn }}
	SelectedRow  = Style{func(Theme) lipgloss.Style { return lipgloss.NewStyle().Bold(true) }}
	SectionLabel = Style{func(t Theme) lipgloss.Style { return t.Section }}
)

// tileHues mirror the macOS app's per-issuer palette.
var tileHues = []string{
	"#3B6FB0", "#2A8C7E", "#8E5BA6", "#B5562E",
	"#3E7D4F", "#4F5BB0", "#B0506E", "#B08A2E",
}

// hueFor picks a stable per-account color from the app's tile palette.
func hueFor(key string) string {
	var hash uint32 = 2166136261
	for _, b := range []byte(key) {
		hash = (hash ^ uint32(b)) * 16777619
	}
	return tileHues[hash%uint32(len(tileHues))]
}

// Handle renders an account's handle for stdout; see [Theme.Handle].
func Handle(key, handle string) string {
	return render(func(t Theme) string { return t.Handle(key, handle) })
}

// GroupCode splits an all-digit code of an even length of 6 or more into two
// halves, "123 456" or "1234 5678"; other codes pass through.
func GroupCode(code string) string {
	n := len(code)
	if n < 6 || n%2 != 0 {
		return code
	}
	for i := range n {
		if code[i] < '0' || code[i] > '9' {
			return code
		}
	}
	return code[:n/2] + " " + code[n/2:]
}

// Bar renders a countdown bar for stdout; see [Theme.Bar].
func Bar(remaining, period, width int) string {
	return render(func(t Theme) string { return t.Bar(remaining, period, width) })
}

// Plain renders without styling (for piping/JSON contexts when needed).
func Plain(s string) string { return s }
