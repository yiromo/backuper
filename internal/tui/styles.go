package tui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
)

var (
	colorPrimary = lipgloss.Color("#7C3AED") // purple
	colorSuccess = lipgloss.Color("#10B981") // green
	colorError   = lipgloss.Color("#EF4444") // red
	colorWarning = lipgloss.Color("#F59E0B") // amber
	colorMuted   = lipgloss.Color("#6B7280") // gray
	colorAccent  = lipgloss.Color("#06B6D4") // cyan
	colorFg      = lipgloss.Color("#F9FAFB") // near-white
	colorBg      = lipgloss.Color("#111827") // near-black

	styleNavBar = lipgloss.NewStyle().
			Background(colorPrimary).
			Foreground(colorFg).
			Padding(0, 1).
			Bold(true)

	styleNavKey = lipgloss.NewStyle().
			Background(colorPrimary).
			Foreground(lipgloss.Color("#DDD6FE")).
			Bold(true)

	styleTitle = lipgloss.NewStyle().
			Foreground(colorPrimary).
			Bold(true).
			MarginBottom(1)

	styleSelected = lipgloss.NewStyle().
			Background(colorPrimary).
			Foreground(colorFg).
			Bold(true)

	styleSuccess = lipgloss.NewStyle().Foreground(colorSuccess)
	styleError   = lipgloss.NewStyle().Foreground(colorError)
	styleWarning = lipgloss.NewStyle().Foreground(colorWarning)
	styleMuted   = lipgloss.NewStyle().Foreground(colorMuted)
	styleAccent  = lipgloss.NewStyle().Foreground(colorAccent)

	styleStatusBar = lipgloss.NewStyle().
			Background(lipgloss.Color("#1F2937")).
			Foreground(colorFg).
			Padding(0, 1)

	styleStatusErr = lipgloss.NewStyle().
			Background(colorError).
			Foreground(colorFg).
			Padding(0, 1)

	styleBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorPrimary).
			Padding(0, 1)

	styleTableHeader = lipgloss.NewStyle().
				Foreground(colorAccent).
				Bold(true)

	styleHelp = lipgloss.NewStyle().
			Foreground(colorMuted).
			MarginTop(1)
)

func navItem(key, label string, active bool) string {
	k := "[" + key + "]"
	if active {
		return lipgloss.NewStyle().
			Background(colorFg).
			Foreground(colorPrimary).
			Bold(true).
			Padding(0, 1).
			Render(k + label)
	}
	return lipgloss.NewStyle().
		Background(colorPrimary).
		Foreground(colorFg).
		Padding(0, 1).
		Render(styleNavKey.Render(k) + label)
}

func statusSuccess(s string) string { return styleSuccess.Render(s) }
func statusError(s string) string   { return styleError.Render(s) }

func humanBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
